package transaction_verifier

import (
	"context"
	"testing"
	"time"

	"primev-poc/txstore"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTransactionVerifier(t *testing.T) {
	txStore := txstore.NewTransactionStore()

	// Test with invalid RPC URL
	_, err := NewTransactionVerifier("invalid-url", txStore, time.Minute)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to Ethereum client")

	// Note: We can't test with a real RPC URL in unit tests without mocking
	// In a real scenario, you would use a mock Ethereum client
}

func TestTransactionVerifier_GetVerificationStats(t *testing.T) {
	txStore := txstore.NewTransactionStore()

	// Add some test transactions
	hash1 := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	hash2 := common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")

	encryptedTx1 := &txstore.EncryptedTransaction{
		Eon:                1,
		MaxInclusionWindow: 10,
		EncryptedTx:        []byte("encrypted1"),
		TxHash:             hash1.Bytes(),
		Identity:           "identity1",
		DecryptionKey:      []byte("key1"),
	}

	encryptedTx2 := &txstore.EncryptedTransaction{
		Eon:                2,
		MaxInclusionWindow: 20,
		EncryptedTx:        []byte("encrypted2"),
		TxHash:             hash2.Bytes(),
		Identity:           "identity2",
		DecryptionKey:      []byte("key2"),
	}

	err := txStore.StoreTransaction(hash1, encryptedTx1)
	require.NoError(t, err)

	err = txStore.StoreTransaction(hash2, encryptedTx2)
	require.NoError(t, err)

	// Update one to decrypted status
	err = txStore.UpdateTransactionStatus(hash1, txstore.StatusDecrypted)
	require.NoError(t, err)

	// Create verifier with mock RPC URL (won't actually connect in this test)
	verifier := &TransactionVerifier{
		txStore:  txStore,
		rpcURL:   "http://localhost:8545",
		interval: time.Minute,
		ctx:      context.Background(),
	}

	stats := verifier.GetVerificationStats()

	assert.Equal(t, 2, stats["total_transactions"])
	assert.Equal(t, "http://localhost:8545", stats["rpc_url"])
	assert.Equal(t, "1m0s", stats["verification_interval"])

	statusCounts, ok := stats["status_counts"].(map[txstore.TransactionStatus]int)
	require.True(t, ok)
	assert.Equal(t, 1, statusCounts[txstore.StatusInit])
	assert.Equal(t, 1, statusCounts[txstore.StatusDecrypted])
}

func TestTransactionVerifier_Stop(t *testing.T) {
	txStore := txstore.NewTransactionStore()

	verifier := &TransactionVerifier{
		txStore:  txStore,
		rpcURL:   "http://localhost:8545",
		interval: time.Minute,
		ctx:      context.Background(),
	}

	// Should not panic
	verifier.Stop()
}
