package bidder

import (
	"fmt"
	"math/big"
	"time"

	"primev-poc/txhandler"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog/log"
)

//TODO: commitment has a txHash

type Bid struct {
	TxHashes            []string `json:"txHashes"`
	Amount              string   `json:"amount"`
	BlockNumber         uint64   `json:"blockNumber"`
	DecayStartTimestamp uint64   `json:"decayStartTimestamp"`
	DecayEndTimestamp   uint64   `json:"decayEndTimestamp"`
	RevertingTxHashes   []string `json:"revertingTxHashes"`
	RawTransactions     []string `json:"rawTransactions"`
	SlashAmount         string   `json:"slashAmount"`
}

type BidManager struct {
	txHandler *txhandler.TransactionHandler
}

func NewBidManager(txHandler *txhandler.TransactionHandler) *BidManager {
	return &BidManager{
		txHandler: txHandler,
	}
}

func (bm *BidManager) CreateBidsFromInitTransactions() ([]*Bid, error) {
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

	var bids []*Bid

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

func (bm *BidManager) createBidForBlock(blockNumber uint64, transactions []*txhandler.StoredTransaction, hashes []common.Hash) (*Bid, error) {
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

	bid := &Bid{
		TxHashes:    txHashes,
		Amount:      "1000000000000000000",
		BlockNumber: blockNumber,

		DecayStartTimestamp: uint64(currentTime + 30000),  // 30 seconds from now
		DecayEndTimestamp:   uint64(currentTime + 300000), // 5 minutes from now

		RevertingTxHashes: []string{},

		RawTransactions: rawTransactions,

		SlashAmount: "0",
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

func (bid *Bid) GetBidValue() (*big.Int, error) {
	amount, ok := new(big.Int).SetString(bid.Amount, 10)
	if !ok {
		return nil, fmt.Errorf("invalid bid amount: %s", bid.Amount)
	}

	currentTime := uint64(time.Now().UnixMilli())

	if currentTime < bid.DecayStartTimestamp {
		return amount, nil
	}

	if currentTime >= bid.DecayEndTimestamp {
		return big.NewInt(0), nil
	}

	totalDecayTime := bid.DecayEndTimestamp - bid.DecayStartTimestamp
	elapsedDecayTime := currentTime - bid.DecayStartTimestamp

	remainingRatio := new(big.Int).Sub(
		new(big.Int).SetUint64(totalDecayTime),
		new(big.Int).SetUint64(elapsedDecayTime),
	)

	currentValue := new(big.Int).Mul(amount, remainingRatio)
	currentValue.Div(currentValue, new(big.Int).SetUint64(totalDecayTime))

	return currentValue, nil
}

func (bid *Bid) String() string {
	return fmt.Sprintf("Bid{Block: %d, TxCount: %d, Amount: %s, Decay: %d-%d}",
		bid.BlockNumber,
		len(bid.TxHashes),
		bid.Amount,
		bid.DecayStartTimestamp,
		bid.DecayEndTimestamp,
	)
}
