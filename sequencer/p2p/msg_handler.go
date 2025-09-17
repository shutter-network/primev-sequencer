package p2p

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"primev-poc/txstore"

	"github.com/ethereum/go-ethereum/common"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/rs/zerolog/log"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/p2pmsg"
)

type HandlerConfig struct {
	BidderNodeAddress string
}

type DecryptionKeysMsgHandler struct {
	txStore *txstore.TransactionStore
	config  *HandlerConfig
}

func NewDecryptionKeysMsgHandler(txStore *txstore.TransactionStore, config *HandlerConfig) *DecryptionKeysMsgHandler {
	return &DecryptionKeysMsgHandler{txStore: txStore, config: config}
}

func (mh *DecryptionKeysMsgHandler) MessagePrototypes() []p2pmsg.Message {
	return []p2pmsg.Message{
		&p2pmsg.DecryptionKeys{},
		&p2pmsg.Commitment{},
	}
}

func (mh *DecryptionKeysMsgHandler) ValidateMessage(_ context.Context, msgUntyped p2pmsg.Message) (pubsub.ValidationResult, error) {
	if _, ok := msgUntyped.(*p2pmsg.Commitment); ok {
		return pubsub.ValidationAccept, nil
	}
	msg, ok := msgUntyped.(*p2pmsg.DecryptionKeys)
	if !ok {
		return pubsub.ValidationReject, fmt.Errorf("unknown message type: %T", msgUntyped)
	}

	if msg.Eon > math.MaxInt64 {
		return pubsub.ValidationReject, fmt.Errorf("eon %d overflows int64", msg.Eon)
	}
	if len(msg.Keys) == 0 {
		return pubsub.ValidationReject, fmt.Errorf("no keys in message")
	}

	return pubsub.ValidationAccept, nil
}

func (mh *DecryptionKeysMsgHandler) HandleMessage(ctx context.Context, msg p2pmsg.Message) ([]p2pmsg.Message, error) {
	msgDecryptionKeys, ok := msg.(*p2pmsg.DecryptionKeys)
	if !ok {
		return nil, fmt.Errorf("unknown message type: %T", msg)
	}

	for _, key := range msgDecryptionKeys.Keys {
		tx := mh.txStore.GetTransactionsByIdentity(hex.EncodeToString(key.IdentityPreimage))
		if tx == nil {
			continue
		}
		tx.EncryptedTx.DecryptionKey = key.Key
		err := mh.txStore.UpdateTransactionStatus(common.BytesToHash(tx.EncryptedTx.TxHash), txstore.StatusDecrypted)
		if err != nil {
			log.Error().
				Err(err).
				Str("tx_hash", hex.EncodeToString(tx.EncryptedTx.TxHash)).
				Msg("Failed to update transaction status")
			continue
		}
		log.Info().
			Str("tx_hash", hex.EncodeToString(tx.EncryptedTx.TxHash)).
			Str("decryption_key", hex.EncodeToString(tx.EncryptedTx.DecryptionKey)).
			Msg("Decryption key received")
	}

	return []p2pmsg.Message{}, nil
}
