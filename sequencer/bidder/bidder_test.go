package bidder

import (
	"encoding/hex"
	"testing"

	"primev-poc/txhandler"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBidManager_CreateBidFromInitTransactions(t *testing.T) {
	// Create a new transaction handler
	txHandler := txhandler.NewTransactionHandler()
	// Use 0.1 ETH for slash amount in tests
	bidManager := NewBidManagerWithSlash(txHandler, "100000000000000000")

	// Test case 1: No init transactions
	t.Run("No init transactions", func(t *testing.T) {
		bid, _, err := bidManager.CreateBidFromInitTransactions(1)
		require.NoError(t, err)
		assert.Nil(t, bid)
	})

	// Test case 2: Single transaction within max inclusion window
	t.Run("Single transaction within max inclusion window", func(t *testing.T) {
		// Create a mock encrypted transaction
		hash1 := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
		encryptedTx := &txhandler.EncryptedTransaction{
			EonID:              1,
			MaxInclusionWindow: 1000,
			EncryptedTx:        []byte("encrypted_data_1"),
			TxHash:             hash1.Bytes(), // Store the same hash as bytes
		}

		// Store the transaction
		err := txHandler.StoreTransaction(hash1, encryptedTx)
		require.NoError(t, err)

		// Create bid for block 500 (within max inclusion window)
		bid, _, err := bidManager.CreateBidFromInitTransactions(500)
		require.NoError(t, err)
		require.NotNil(t, bid)

		// Verify bid structure
		assert.Equal(t, int64(500), bid.BlockNumber)
		assert.Equal(t, "30000000000", bid.Amount)
		assert.Equal(t, "100000000000000000", bid.SlashAmount)
		assert.Len(t, bid.RawTransactions, 1)
		assert.Empty(t, bid.RevertingTxHashes)

		// Verify raw transaction is hex encoded
		expectedRawTx := "0x" + hex.EncodeToString(encryptedTx.EncryptedTx)
		assert.Equal(t, expectedRawTx[2:], bid.RawTransactions[0])

		// Now the transaction status should be properly updated since the hash matches
		storedTx, err := txHandler.GetTransaction(hash1)
		require.NoError(t, err)
		assert.Equal(t, txhandler.StatusBidSubmitted, storedTx.Status)
	})

	// Test case 3: Multiple transactions in same block within max inclusion window
	t.Run("Multiple transactions same block within max inclusion window", func(t *testing.T) {
		// Clear previous transactions by creating a new handler
		txHandler = txhandler.NewTransactionHandler()
		bidManager = NewBidManagerWithSlash(txHandler, "100000000000000000")

		// Create multiple transactions for the same block
		block := uint64(2000)
		hashes := []common.Hash{
			common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
			common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
		}

		for i, hash := range hashes {
			encryptedTx := &txhandler.EncryptedTransaction{
				EonID:              1,
				MaxInclusionWindow: block,
				EncryptedTx:        []byte("encrypted_data_" + string(rune(i+2))),
				TxHash:             hash.Bytes(), // Store the same hash as bytes
			}
			err := txHandler.StoreTransaction(hash, encryptedTx)
			require.NoError(t, err)
		}

		// Create bid for block 1500 (within max inclusion window)
		bid, _, err := bidManager.CreateBidFromInitTransactions(1500)
		require.NoError(t, err)
		require.NotNil(t, bid)

		// Verify bid contains both transactions
		assert.Equal(t, int64(1500), bid.BlockNumber)
		assert.Len(t, bid.RawTransactions, 2)

		// Now transaction statuses should be properly updated since the hashes match
		for _, hash := range hashes {
			storedTx, err := txHandler.GetTransaction(hash)
			require.NoError(t, err)
			assert.Equal(t, txhandler.StatusBidSubmitted, storedTx.Status)
		}
	})

	// Test case 4: Max inclusion window logic - transactions should be blocked if max inclusion window is reached
	t.Run("Max inclusion window logic", func(t *testing.T) {
		// Clear previous transactions
		txHandler = txhandler.NewTransactionHandler()
		bidManager = NewBidManagerWithSlash(txHandler, "100000000000000000")

		// Create transactions with different max inclusion windows
		testData := []struct {
			hash               common.Hash
			maxInclusionWindow uint64
			expectedStatus     txhandler.TransactionStatus
			description        string
		}{
			{
				hash:               common.HexToHash("0x7777777777777777777777777777777777777777777777777777777777777777"),
				maxInclusionWindow: 5000, // Should be included in bid for block 4000
				expectedStatus:     txhandler.StatusBidSubmitted,
				description:        "transaction within max inclusion window",
			},
			{
				hash:               common.HexToHash("0x8888888888888888888888888888888888888888888888888888888888888888"),
				maxInclusionWindow: 3000, // Should be blocked for block 4000
				expectedStatus:     txhandler.StatusBlocked,
				description:        "transaction past max inclusion window",
			},
			{
				hash:               common.HexToHash("0x9999999999999999999999999999999999999999999999999999999999999999"),
				maxInclusionWindow: 4000, // Edge case: exactly at the limit
				expectedStatus:     txhandler.StatusBidSubmitted,
				description:        "transaction at max inclusion window boundary",
			},
		}

		// Store all transactions
		for _, data := range testData {
			encryptedTx := &txhandler.EncryptedTransaction{
				EonID:              1,
				MaxInclusionWindow: data.maxInclusionWindow,
				EncryptedTx:        []byte("encrypted_data_" + data.hash.Hex()[:10]),
				TxHash:             data.hash.Bytes(), // Store the same hash as bytes
			}
			err := txHandler.StoreTransaction(data.hash, encryptedTx)
			require.NoError(t, err)
		}

		// Create bid for block 4000
		bid, _, err := bidManager.CreateBidFromInitTransactions(4000)
		require.NoError(t, err)
		require.NotNil(t, bid)

		// Verify bid only contains transactions within max inclusion window
		expectedTxCount := 2 // Only transactions with max inclusion window >= 4000
		assert.Len(t, bid.RawTransactions, expectedTxCount)

		// Now transaction statuses should be properly updated since the hashes match
		for _, data := range testData {
			storedTx, err := txHandler.GetTransaction(data.hash)
			require.NoError(t, err)
			assert.Equal(t, data.expectedStatus, storedTx.Status,
				"Transaction %s: %s", data.hash.Hex(), data.description)
		}

		// Verify the blocked transaction is not in the bid (this part works correctly)
		blockedTx, err := txHandler.GetTransaction(common.HexToHash("0x8888888888888888888888888888888888888888888888888888888888888888"))
		require.NoError(t, err)
		assert.Equal(t, txhandler.StatusBlocked, blockedTx.Status)
	})

	// Test case 5: All transactions blocked due to max inclusion window
	t.Run("All transactions blocked due to max inclusion window", func(t *testing.T) {
		// Clear previous transactions
		txHandler = txhandler.NewTransactionHandler()
		bidManager = NewBidManagerWithSlash(txHandler, "100000000000000000")

		// Create transactions that are all past their max inclusion window
		hashes := []common.Hash{
			common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		}

		for i, hash := range hashes {
			encryptedTx := &txhandler.EncryptedTransaction{
				EonID:              1,
				MaxInclusionWindow: uint64(1000 + i), // 1000, 1001
				EncryptedTx:        []byte("encrypted_data_" + string(rune(i+1))),
				TxHash:             hash.Bytes(), // Store the same hash as bytes
			}
			err := txHandler.StoreTransaction(hash, encryptedTx)
			require.NoError(t, err)
		}

		// Try to create bid for block 2000 (past all max inclusion windows)
		// The current implementation returns an error when no transactions are eligible
		bid, _, err := bidManager.CreateBidFromInitTransactions(2000)
		require.Error(t, err) // Should return error since no transactions are eligible
		assert.Contains(t, err.Error(), "no transactions provided for block 2000")
		assert.Nil(t, bid)

		// Now transaction statuses should be properly updated since the hashes match
		for _, hash := range hashes {
			storedTx, err := txHandler.GetTransaction(hash)
			require.NoError(t, err)
			assert.Equal(t, txhandler.StatusBlocked, storedTx.Status)
		}
	})
}

func TestBidManager_UpdateTransactionStatuses(t *testing.T) {
	txHandler := txhandler.NewTransactionHandler()
	bidManager := NewBidManager(txHandler)

	// Create and store a transaction
	hash := common.HexToHash("0x7777777777777777777777777777777777777777777777777777777777777777")
	encryptedTx := &txhandler.EncryptedTransaction{
		EonID:              1,
		MaxInclusionWindow: 4000,
		EncryptedTx:        []byte("encrypted_data"),
		TxHash:             hash.Bytes(), // Store the same hash as bytes
	}

	err := txHandler.StoreTransaction(hash, encryptedTx)
	require.NoError(t, err)

	// Verify initial status
	storedTx, err := txHandler.GetTransaction(hash)
	require.NoError(t, err)
	assert.Equal(t, txhandler.StatusInit, storedTx.Status)

	// Update status
	err = bidManager.updateTransactionStatuses([]common.Hash{hash}, txhandler.StatusBidSubmitted)
	require.NoError(t, err)

	// Verify updated status
	storedTx, err = txHandler.GetTransaction(hash)
	require.NoError(t, err)
	assert.Equal(t, txhandler.StatusBidSubmitted, storedTx.Status)
}
