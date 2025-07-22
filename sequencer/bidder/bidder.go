package bidder

import (
	"fmt"
	"io"
	"time"

	"primev-poc/txhandler"

	"context"

	"github.com/ethereum/go-ethereum/common"
	bidderapi "github.com/primev/mev-commit/p2p/gen/go/bidderapi/v1"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

//TODO: commitment has a txHash

const DefaultSlashAmount = "0"

type BidManager struct {
	txHandler   *txhandler.TransactionHandler
	slashAmount string
}

func NewBidManager(txHandler *txhandler.TransactionHandler) *BidManager {
	return &BidManager{
		txHandler:   txHandler,
		slashAmount: DefaultSlashAmount,
	}
}

// For testing, allow setting a custom slash amount
func NewBidManagerWithSlash(txHandler *txhandler.TransactionHandler, slashAmount string) *BidManager {
	return &BidManager{
		txHandler:   txHandler,
		slashAmount: slashAmount,
	}
}

func (bm *BidManager) CreateBidsFromInitTransactions() ([]*bidderapi.Bid, error) {
	initTransactions := bm.txHandler.GetTransactionsByStatus(txhandler.StatusInit)

	if len(initTransactions) == 0 {
		log.Debug().Msg("No transactions with init status found")
		return nil, nil
	}

	log.Info().
		Int("transaction_count", len(initTransactions)).
		Msg("Found transactions with init status, creating bids")

	blockToTransactions := make(map[uint64][]*txhandler.StoredTransaction)
	blockToHashes := make(map[uint64][]common.Hash)

	allTransactions := bm.txHandler.GetAllTransactions()

	for hash, tx := range allTransactions {
		if tx.Status == txhandler.StatusInit {
			blockNum := tx.EncryptedTx.ScheduledBlock
			blockToTransactions[blockNum] = append(blockToTransactions[blockNum], tx)
			blockToHashes[blockNum] = append(blockToHashes[blockNum], hash)
		}
	}

	var bids []*bidderapi.Bid

	for blockNumber, transactions := range blockToTransactions {
		hashes := blockToHashes[blockNumber]

		bid, err := bm.createBidForBlock(blockNumber, transactions, hashes)
		if err != nil {
			log.Error().
				Err(err).
				Uint64("block_number", blockNumber).
				Msg("Failed to create bid for block")
			continue
		}

		bids = append(bids, bid)

		err = bm.updateTransactionStatuses(hashes, txhandler.StatusBidSubmitted)
		if err != nil {
			log.Error().
				Err(err).
				Uint64("block_number", blockNumber).
				Msg("Failed to update transaction statuses after bid creation")
		}

		log.Info().
			Uint64("block_number", blockNumber).
			Int("tx_count", len(transactions)).
			Str("bid_amount", bid.Amount).
			Msg("Created bid for scheduled block and updated transaction statuses")
	}

	return bids, nil
}

func (bm *BidManager) createBidForBlock(blockNumber uint64, transactions []*txhandler.StoredTransaction, hashes []common.Hash) (*bidderapi.Bid, error) {
	if len(transactions) == 0 {
		return nil, fmt.Errorf("no transactions provided for block %d", blockNumber)
	}

	if len(transactions) != len(hashes) {
		return nil, fmt.Errorf("mismatch between transactions and hashes count for block %d", blockNumber)
	}

	var txHashes []string
	var rawTransactions []string

	for i, hash := range hashes {
		txHashes = append(txHashes, hash.Hex())
		rawTransactions = append(rawTransactions, fmt.Sprintf("0x%x", transactions[i].EncryptedTx.EncryptedTx))
	}

	currentTime := time.Now().UnixMilli()

	bid := &bidderapi.Bid{
		TxHashes:    txHashes,
		Amount:      "1000000000000000000",
		BlockNumber: int64(blockNumber),

		DecayStartTimestamp: int64(currentTime + 30000),  // 30 seconds from now
		DecayEndTimestamp:   int64(currentTime + 300000), // 5 minutes from now

		RevertingTxHashes: []string{},

		RawTransactions: rawTransactions,

		SlashAmount: bm.slashAmount,
	}

	return bid, nil
}

func (bm *BidManager) updateTransactionStatuses(hashes []common.Hash, newStatus txhandler.TransactionStatus) error {
	var errors []error

	for _, hash := range hashes {
		err := bm.txHandler.UpdateTransactionStatus(hash, newStatus)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to update status for tx %s: %w", hash.Hex(), err))
		}
	}

	if len(errors) > 0 {
		for i, err := range errors {
			if i == 0 {
				continue
			}
			log.Error().Err(err).Msg("Additional transaction status update error")
		}
		return errors[0]
	}

	return nil
}

// SubmitBidGRPC submits a bid to the mev-commit node and returns all Commitments from the stream.
// For each commitment, updates the status of all tx_hashes in txHandler to StatusCommitted.
func SubmitBidGRPC(ctx context.Context, grpcAddr string, bid *bidderapi.Bid, txHandler *txhandler.TransactionHandler) ([]*bidderapi.Commitment, error) {
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
	}
	defer conn.Close()

	client := bidderapi.NewBidderClient(conn)
	stream, err := client.SendBid(ctx, bid)
	if err != nil {
		return nil, fmt.Errorf("failed to send bid: %w", err)
	}

	commitmentsCh := make(chan *bidderapi.Commitment)
	errCh := make(chan error, 1)
	var commitments []*bidderapi.Commitment

	// Goroutine to receive commitments
	go func() {
		for {
			commitment, err := stream.Recv()
			if err != nil {
				errCh <- err
				close(commitmentsCh)
				return
			}
			commitmentsCh <- commitment
		}
	}()

	// Main loop: collect commitments and update txHandler
	for {
		select {
		case <-ctx.Done():
			return commitments, ctx.Err()
		case err := <-errCh:
			if err == io.EOF {
				return commitments, nil
			}
			return commitments, err
		case commitment, ok := <-commitmentsCh:
			if !ok {
				return commitments, nil
			}
			commitments = append(commitments, commitment)
			// Update all tx_hashes in txHandler to StatusCommitted
			for _, txHashHex := range commitment.GetTxHashes() {
				hash := common.HexToHash(txHashHex)
				_ = txHandler.UpdateTransactionStatus(hash, txhandler.StatusCommitted)
			}
		}
	}
}
