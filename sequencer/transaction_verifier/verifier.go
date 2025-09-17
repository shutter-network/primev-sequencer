package transaction_verifier

import (
	"context"
	"fmt"
	"time"

	"primev-poc/txhandler"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog/log"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/medley/service"
)

// TransactionVerifier handles verification of decrypted transactions on the blockchain
type TransactionVerifier struct {
	ethClient *ethclient.Client
	txHandler *txhandler.TransactionHandler
	rpcURL    string
	interval  time.Duration
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewTransactionVerifier creates a new transaction verifier instance
func NewTransactionVerifier(rpcURL string, txHandler *txhandler.TransactionHandler, interval time.Duration) (*TransactionVerifier, error) {
	// Create Ethereum client connection
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum client: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &TransactionVerifier{
		ethClient: client,
		txHandler: txHandler,
		rpcURL:    rpcURL,
		interval:  interval,
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

// Start begins the verification process in a goroutine
func (tv *TransactionVerifier) Start(ctx context.Context, runner service.Runner) error {
	runner.Go(func() error {
		tv.verificationLoop()
		return nil
	})
	runner.Go(func() error {
		<-ctx.Done()
		tv.Stop()
		return nil
	})
	return nil
}

// Stop stops the verification process
func (tv *TransactionVerifier) Stop() {
	tv.cancel()
	tv.ethClient.Close()
}

// verificationLoop runs the main verification loop
func (tv *TransactionVerifier) verificationLoop() {
	ticker := time.NewTicker(tv.interval)
	defer ticker.Stop()

	log.Info().Msg("Transaction verifier started")

	for {
		select {
		case <-tv.ctx.Done():
			log.Info().Msg("Transaction verifier stopped")
			return
		case <-ticker.C:
			tv.verifyDecryptedTransactions()
		}
	}
}

// verifyDecryptedTransactions checks all decrypted transactions for blockchain inclusion
func (tv *TransactionVerifier) verifyDecryptedTransactions() {

	blockNumber, err := tv.ethClient.BlockNumber(tv.ctx)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to get current block number")
		return
	}
	decryptedTxs := tv.txHandler.GetTransactionsByCommitedBlock(blockNumber)

	if len(decryptedTxs) == 0 {
		return
	}

	log.Info().Int("count", len(decryptedTxs)).Msg("Verifying decrypted transactions")

	for _, tx := range decryptedTxs {
		txHash := common.BytesToHash(tx.EncryptedTx.TxHash)

		isOnBlockchain, err := tv.isTransactionOnBlockchain(txHash)
		if err != nil {
			log.Error().
				Err(err).
				Str("tx_hash", txHash.Hex()).
				Msg("Failed to check transaction on blockchain")
			continue
		}

		if isOnBlockchain {
			err = tv.txHandler.UpdateTransactionStatus(txHash, txhandler.StatusFinalised)
			if err != nil {
				log.Error().
					Err(err).
					Str("tx_hash", txHash.Hex()).
					Msg("Failed to update transaction status to finalised")
			} else {
				log.Info().
					Str("tx_hash", txHash.Hex()).
					Msg("Transaction verified on blockchain, status updated to finalised")
			}
		} else {
			err := tv.txHandler.IncrementTransactionRetries(txHash)
			if err != nil {
				log.Error().
					Err(err).
					Str("tx_hash", txHash.Hex()).
					Msg("Failed to increment transaction retries")
			}
			if tx.Retries >= 3 {
				log.Error().
					Str("tx_hash", txHash.Hex()).
					Msg("Transaction not found on blockchain after max retries, status set to blocked")
				err = tv.txHandler.UpdateTransactionStatus(txHash, txhandler.StatusBlocked)
				if err != nil {
					log.Error().
						Err(err).
						Str("tx_hash", txHash.Hex()).
						Msg("Failed to update transaction status to blocked")
				}
				continue
			}
			err = tv.txHandler.UpdateTransactionStatus(txHash, txhandler.StatusInit)
			if err != nil {
				log.Error().
					Err(err).
					Str("tx_hash", txHash.Hex()).
					Msg("Failed to update transaction status to init")
			} else {
				log.Info().
					Str("tx_hash", txHash.Hex()).
					Msg("Transaction not found on blockchain, status reset to init")
			}
		}
	}
}

// isTransactionOnBlockchain checks if a transaction exists on the blockchain
func (tv *TransactionVerifier) isTransactionOnBlockchain(txHash common.Hash) (bool, error) {
	// Get transaction receipt to check if it exists
	receipt, err := tv.ethClient.TransactionReceipt(tv.ctx, txHash)
	if err != nil {
		// If we get an error, it might mean the transaction doesn't exist
		// We need to distinguish between "not found" and other errors
		if err.Error() == "not found" {
			return false, nil
		}
		return false, fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	// If we get a receipt, the transaction exists on the blockchain
	return receipt != nil, nil
}

// GetVerificationStats returns statistics about the verification process
func (tv *TransactionVerifier) GetVerificationStats() map[string]interface{} {
	statusCounts := tv.txHandler.GetStatusCounts()

	stats := map[string]interface{}{
		"total_transactions":    tv.txHandler.GetTransactionCount(),
		"status_counts":         statusCounts,
		"rpc_url":               tv.rpcURL,
		"verification_interval": tv.interval.String(),
	}

	return stats
}
