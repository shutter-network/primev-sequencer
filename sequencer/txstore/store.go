package txstore

import (
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog/log"
)

var ErrNotFound = fmt.Errorf("transaction not found")

type TransactionStatus string

const (
	StatusInit         TransactionStatus = "init"
	StatusBidSubmitted TransactionStatus = "bidsubmitted"
	StatusCommitted    TransactionStatus = "committed"
	StatusDecrypted    TransactionStatus = "decrypted"
	StatusFinalised    TransactionStatus = "finalised"
	StatusBlocked      TransactionStatus = "blocked"
)

type EncryptedTransaction struct {
	Eon                uint64
	MaxInclusionWindow uint64
	EncryptedTx        []byte
	TxHash             []byte
	Identity           string
	DecryptionKey      []byte
}

type StoredTransaction struct {
	EncryptedTx    *EncryptedTransaction
	Status         TransactionStatus
	SubmissionTime int64
	CommitedBlock  uint64
	Retries        int
}

type TransactionStore struct {
	transactions map[common.Hash]*StoredTransaction
	mutex        sync.RWMutex
}

func NewTransactionStore() *TransactionStore {
	return &TransactionStore{
		transactions: make(map[common.Hash]*StoredTransaction),
		mutex:        sync.RWMutex{},
	}
}

func (th *TransactionStore) StoreTransaction(hash common.Hash, encryptedTx *EncryptedTransaction) error {
	th.mutex.Lock()
	defer th.mutex.Unlock()

	if _, exists := th.transactions[hash]; exists {
		return fmt.Errorf("transaction %s already exists", hash.Hex())
	}

	transaction := &StoredTransaction{
		EncryptedTx:    encryptedTx,
		Status:         StatusInit,
		SubmissionTime: getCurrentTimestamp(),
		CommitedBlock:  0,
		Retries:        0,
	}

	th.transactions[hash] = transaction

	log.Info().
		Str("tx_hash", hash.Hex()).
		Uint64("max_inclusion_window", encryptedTx.MaxInclusionWindow).
		Uint64("eon", encryptedTx.Eon).
		Str("status", string(StatusInit)).
		Msg("Transaction stored")

	return nil
}

func (th *TransactionStore) UpdateTransactionStatus(hash common.Hash, status TransactionStatus) error {
	th.mutex.Lock()
	defer th.mutex.Unlock()

	transaction, exists := th.transactions[hash]
	if !exists {
		return fmt.Errorf("transaction %s not found", hash.Hex())
	}

	oldStatus := transaction.Status
	transaction.Status = status

	log.Info().
		Str("tx_hash", hash.Hex()).
		Str("old_status", string(oldStatus)).
		Str("new_status", string(status)).
		Msg("Transaction status updated")

	return nil
}

func (th *TransactionStore) UpdateCommittedTransaction(hash common.Hash, commitedBlock uint64) error {
	th.mutex.Lock()
	defer th.mutex.Unlock()

	transaction, exists := th.transactions[hash]
	if !exists {
		return fmt.Errorf("transaction %s not found", hash.Hex())
	}

	if transaction.EncryptedTx.DecryptionKey != nil {
		transaction.Status = StatusDecrypted
	} else {
		transaction.Status = StatusCommitted
	}
	transaction.CommitedBlock = commitedBlock
	return nil
}

func (th *TransactionStore) IncrementTransactionRetries(hash common.Hash) error {
	th.mutex.Lock()
	defer th.mutex.Unlock()

	transaction, exists := th.transactions[hash]
	if !exists {
		return fmt.Errorf("transaction %s not found", hash.Hex())
	}
	transaction.Retries++
	return nil
}

func (th *TransactionStore) GetTransaction(hash common.Hash) (*StoredTransaction, error) {
	th.mutex.RLock()
	defer th.mutex.RUnlock()

	transaction, exists := th.transactions[hash]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, hash.Hex())
	}

	return transaction, nil
}

func (th *TransactionStore) GetTransactionsByStatus(status TransactionStatus) []*StoredTransaction {
	th.mutex.RLock()
	defer th.mutex.RUnlock()

	var result []*StoredTransaction
	for _, transaction := range th.transactions {
		if transaction.Status == status {
			result = append(result, transaction)
		}
	}

	return result
}

func (th *TransactionStore) GetAllTransactions() map[common.Hash]*StoredTransaction {
	th.mutex.RLock()
	defer th.mutex.RUnlock()

	result := make(map[common.Hash]*StoredTransaction)
	for hash, transaction := range th.transactions {
		result[hash] = &StoredTransaction{
			EncryptedTx:    transaction.EncryptedTx,
			Status:         transaction.Status,
			SubmissionTime: transaction.SubmissionTime,
		}
	}

	return result
}

func (th *TransactionStore) GetTransactionCount() int {
	th.mutex.RLock()
	defer th.mutex.RUnlock()

	return len(th.transactions)
}

func (th *TransactionStore) GetStatusCounts() map[TransactionStatus]int {
	th.mutex.RLock()
	defer th.mutex.RUnlock()

	counts := make(map[TransactionStatus]int)
	for _, transaction := range th.transactions {
		counts[transaction.Status]++
	}

	return counts
}

// GetTransactionsByIdentity returns all transactions that match the given identity
func (th *TransactionStore) GetTransactionsByIdentity(identity string) *StoredTransaction {
	th.mutex.RLock()
	defer th.mutex.RUnlock()

	for _, transaction := range th.transactions {
		if transaction.EncryptedTx.Identity == identity {
			return transaction
		}
	}

	return nil
}

// GetTransactionsByIdentityAndStatus returns all transactions that match both identity and status
func (th *TransactionStore) GetTransactionsByIdentityAndStatus(identity string, status TransactionStatus) *StoredTransaction {
	th.mutex.RLock()
	defer th.mutex.RUnlock()

	for _, transaction := range th.transactions {
		if transaction.EncryptedTx.Identity == identity && transaction.Status == status {
			return transaction
		}
	}

	return nil
}

func (th *TransactionStore) GetTransactionsByCommitedBlock(commitedBlock uint64) []*StoredTransaction {
	th.mutex.RLock()
	defer th.mutex.RUnlock()

	var result []*StoredTransaction
	for _, transaction := range th.transactions {
		if transaction.CommitedBlock != 0 && transaction.CommitedBlock <= commitedBlock && transaction.Status == StatusDecrypted {
			result = append(result, transaction)
		}
	}

	return result
}

func getCurrentTimestamp() int64 {
	return time.Now().Unix()
}
