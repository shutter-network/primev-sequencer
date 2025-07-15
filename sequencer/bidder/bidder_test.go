package bidder

import (
	"encoding/hex"
	"testing"
	"time"

	"primev-poc/txhandler"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBidManager_CreateBidsFromInitTransactions(t *testing.T) {
	// Create a new transaction handler
	txHandler := txhandler.NewTransactionHandler()
	bidManager := NewBidManager(txHandler)

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
		assert.Equal(t, uint64(1000), bid.BlockNumber)
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
		bidManager = NewBidManager(txHandler)

		// Create multiple transactions for the same block
		block := uint64(2000)
		hashes := []common.Hash{
			common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
			common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
		}

		for i, hash := range hashes {
			encryptedTx := &txhandler.EncryptedTransaction{
				EonID:          1,
				ScheduledBlock: block,
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
		assert.Equal(t, block, bid.BlockNumber)
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
		bidManager = NewBidManager(txHandler)

		// Create transactions for different blocks
		testData := []struct {
			hash  common.Hash
			block uint64
		}{
			{common.HexToHash("0x4444444444444444444444444444444444444444444444444444444444444444"), 3000},
			{common.HexToHash("0x5555555555555555555555555555555555555555555555555555555555555555"), 3001},
			{common.HexToHash("0x6666666666666666666666666666666666666666666666666666666666666666"), 3000}, // Same block as first
		}

		for i, data := range testData {
			encryptedTx := &txhandler.EncryptedTransaction{
				EonID:          1,
				ScheduledBlock: data.block,
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
		blockNumbers := make(map[uint64]int)
		for _, bid := range bids {
			blockNumbers[bid.BlockNumber]++
		}

		assert.Equal(t, 1, blockNumbers[3001]) // One transaction in block 3001
		assert.Equal(t, 1, blockNumbers[3000]) // One bid for block 3000 (with 2 transactions)

		// Find the bid for block 3000 and verify it has 2 transactions
		for _, bid := range bids {
			if bid.BlockNumber == 3000 {
				assert.Len(t, bid.TxHashes, 2)
				assert.Len(t, bid.RawTransactions, 2)
			} else if bid.BlockNumber == 3001 {
				assert.Len(t, bid.TxHashes, 1)
				assert.Len(t, bid.RawTransactions, 1)
			}
		}
	})
}

func TestBid_GetBidValue(t *testing.T) {
	currentTime := time.Now().UnixMilli()

	// Test case 1: Before decay start
	t.Run("Before decay start", func(t *testing.T) {
		bid := &Bid{
			Amount:              "1000000000000000000",       // 1 ETH
			DecayStartTimestamp: uint64(currentTime + 10000), // 10 seconds in future
			DecayEndTimestamp:   uint64(currentTime + 60000), // 1 minute in future
		}

		value, err := bid.GetBidValue()
		require.NoError(t, err)
		assert.Equal(t, "1000000000000000000", value.String())
	})

	// Test case 2: After decay end
	t.Run("After decay end", func(t *testing.T) {
		bid := &Bid{
			Amount:              "1000000000000000000",       // 1 ETH
			DecayStartTimestamp: uint64(currentTime - 60000), // 1 minute ago
			DecayEndTimestamp:   uint64(currentTime - 10000), // 10 seconds ago
		}

		value, err := bid.GetBidValue()
		require.NoError(t, err)
		assert.Equal(t, "0", value.String())
	})

	// Test case 3: During decay (middle)
	t.Run("During decay - middle", func(t *testing.T) {
		bid := &Bid{
			Amount:              "1000000000000000000",       // 1 ETH
			DecayStartTimestamp: uint64(currentTime - 30000), // 30 seconds ago
			DecayEndTimestamp:   uint64(currentTime + 30000), // 30 seconds in future
		}

		value, err := bid.GetBidValue()
		require.NoError(t, err)

		// Should be approximately 50% of original value (halfway through decay)
		// Allow some variance due to timing precision
		originalValue := int64(1000000000000000000)
		actualValue := value.Int64()

		// Should be between 40% and 60% of original (allowing for timing variance)
		assert.True(t, actualValue >= originalValue*4/10)
		assert.True(t, actualValue <= originalValue*6/10)
	})

	// Test case 4: Invalid amount
	t.Run("Invalid amount", func(t *testing.T) {
		bid := &Bid{
			Amount:              "invalid_amount",
			DecayStartTimestamp: uint64(currentTime - 10000),
			DecayEndTimestamp:   uint64(currentTime + 10000),
		}

		_, err := bid.GetBidValue()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid bid amount")
	})
}

func TestBid_String(t *testing.T) {
	bid := &Bid{
		TxHashes:            []string{"0x1111", "0x2222"},
		Amount:              "1000000000000000000",
		BlockNumber:         1234,
		DecayStartTimestamp: 1609459200000, // 2021-01-01 00:00:00 UTC
		DecayEndTimestamp:   1609459260000, // 2021-01-01 00:01:00 UTC
	}

	result := bid.String()
	expected := "Bid{Block: 1234, TxCount: 2, Amount: 1000000000000000000, Decay: 1609459200000-1609459260000}"
	assert.Equal(t, expected, result)
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
