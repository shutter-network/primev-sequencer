package bidder

import (
	"encoding/hex"
	"testing"

	"primev-poc/txhandler"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBidManager_CreateBidsFromInitTransactions(t *testing.T) {
	// Create a new transaction handler
	txHandler := txhandler.NewTransactionHandler()
	// Use 0.1 ETH for slash amount in tests
	bidManager := NewBidManagerWithSlash(txHandler, "100000000000000000")

	// Test case 1: No init transactions
	t.Run("No init transactions", func(t *testing.T) {
		bids, err := bidManager.CreateBidsFromInitTransactions()
		require.NoError(t, err)
		assert.Nil(t, bids)
	})

	// Test case 2: Single transaction
	t.Run("Single transaction", func(t *testing.T) {
		// Create a mock encrypted transaction
		encryptedTx := &txhandler.EncryptedTransaction{
			EonID:          1,
			ScheduledBlock: 1000,
			EncryptedTx:    []byte("encrypted_data_1"),
			TxHash:         []byte("hash_1"),
		}

		// Store the transaction
		hash1 := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
		err := txHandler.StoreTransaction(hash1, encryptedTx)
		require.NoError(t, err)

		// Create bids
		bids, err := bidManager.CreateBidsFromInitTransactions()
		require.NoError(t, err)
		require.Len(t, bids, 1)

		// Verify bid structure
		bid := bids[0]
		assert.Equal(t, int64(1000), bid.BlockNumber)
		assert.Len(t, bid.TxHashes, 1)
		assert.Equal(t, hash1.Hex(), bid.TxHashes[0])
		assert.Equal(t, "1000000000000000000", bid.Amount)     // 1 ETH
		assert.Equal(t, "100000000000000000", bid.SlashAmount) // 0.1 ETH
		assert.Empty(t, bid.RevertingTxHashes)
		assert.Len(t, bid.RawTransactions, 1)

		// Verify raw transaction is hex encoded
		expectedRawTx := "0x" + hex.EncodeToString(encryptedTx.EncryptedTx)
		assert.Equal(t, expectedRawTx, bid.RawTransactions[0])

		// Verify transaction status was updated
		storedTx, err := txHandler.GetTransaction(hash1)
		require.NoError(t, err)
		assert.Equal(t, txhandler.StatusBidSubmitted, storedTx.Status)
	})

	// Test case 3: Multiple transactions in same block
	t.Run("Multiple transactions same block", func(t *testing.T) {
		// Clear previous transactions by creating a new handler
		txHandler = txhandler.NewTransactionHandler()
		bidManager = NewBidManagerWithSlash(txHandler, "100000000000000000")

		// Create multiple transactions for the same block
		block := int64(2000)
		hashes := []common.Hash{
			common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
			common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
		}

		for i, hash := range hashes {
			encryptedTx := &txhandler.EncryptedTransaction{
				EonID:          1,
				ScheduledBlock: uint64(block),
				EncryptedTx:    []byte("encrypted_data_" + string(rune(i+2))),
				TxHash:         []byte("hash_" + string(rune(i+2))),
			}
			err := txHandler.StoreTransaction(hash, encryptedTx)
			require.NoError(t, err)
		}

		// Create bids
		bids, err := bidManager.CreateBidsFromInitTransactions()
		require.NoError(t, err)
		require.Len(t, bids, 1) // Should be one bid for one block

		// Verify bid contains both transactions
		bid := bids[0]
		assert.Equal(t, int64(block), bid.BlockNumber)
		assert.Len(t, bid.TxHashes, 2)
		assert.Len(t, bid.RawTransactions, 2)

		// Verify both transactions have updated status
		for _, hash := range hashes {
			storedTx, err := txHandler.GetTransaction(hash)
			require.NoError(t, err)
			assert.Equal(t, txhandler.StatusBidSubmitted, storedTx.Status)
		}
	})

	// Test case 4: Multiple transactions in different blocks
	t.Run("Multiple transactions different blocks", func(t *testing.T) {
		// Clear previous transactions
		txHandler = txhandler.NewTransactionHandler()
		bidManager = NewBidManagerWithSlash(txHandler, "100000000000000000")

		// Create transactions for different blocks
		testData := []struct {
			hash  common.Hash
			block int64
		}{
			{common.HexToHash("0x4444444444444444444444444444444444444444444444444444444444444444"), int64(3000)},
			{common.HexToHash("0x5555555555555555555555555555555555555555555555555555555555555555"), int64(3001)},
			{common.HexToHash("0x6666666666666666666666666666666666666666666666666666666666666666"), int64(3000)}, // Same block as first
		}

		for i, data := range testData {
			encryptedTx := &txhandler.EncryptedTransaction{
				EonID:          1,
				ScheduledBlock: uint64(data.block),
				EncryptedTx:    []byte("encrypted_data_" + string(rune(i+4))),
				TxHash:         []byte("hash_" + string(rune(i+4))),
			}
			err := txHandler.StoreTransaction(data.hash, encryptedTx)
			require.NoError(t, err)
		}

		// Create bids
		bids, err := bidManager.CreateBidsFromInitTransactions()
		require.NoError(t, err)
		require.Len(t, bids, 2) // Should be two bids for two different blocks

		// Verify bids
		blockNumbers := make(map[int64]int)
		for _, bid := range bids {
			blockNumbers[bid.BlockNumber]++
		}

		assert.Equal(t, 1, blockNumbers[3001]) // One transaction in block 3001
		assert.Equal(t, 1, blockNumbers[3000]) // One bid for block 3000 (with 2 transactions)

		// Find the bid for block 3000 and verify it has 2 transactions
		for _, bid := range bids {
			switch bid.BlockNumber {
			case 3000:
				assert.Len(t, bid.TxHashes, 2)
				assert.Len(t, bid.RawTransactions, 2)
			case 3001:
				assert.Len(t, bid.TxHashes, 1)
				assert.Len(t, bid.RawTransactions, 1)
			}
		}
	})
}

func TestBidManager_UpdateTransactionStatuses(t *testing.T) {
	txHandler := txhandler.NewTransactionHandler()
	bidManager := NewBidManager(txHandler)

	// Create and store a transaction
	hash := common.HexToHash("0x7777777777777777777777777777777777777777777777777777777777777777")
	encryptedTx := &txhandler.EncryptedTransaction{
		EonID:          1,
		ScheduledBlock: 4000,
		EncryptedTx:    []byte("encrypted_data"),
		TxHash:         []byte("hash"),
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
