package p2p

import (
	"context"
	"fmt"
	"math"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/p2pmsg"
)

type MsgHandler struct {
}

func (mh *MsgHandler) MessagePrototypes() []p2pmsg.Message {
	return []p2pmsg.Message{
		&p2pmsg.DecryptionKeys{},
	}
}

func (mh *MsgHandler) ValidateMessage(_ context.Context, msgUntyped p2pmsg.Message) (pubsub.ValidationResult, error) {
	msg, ok := msgUntyped.(*p2pmsg.DecryptionKeys)
	if !ok {
		return pubsub.ValidationReject, nil
	}
	extra := msg.Extra.(*p2pmsg.DecryptionKeys_Service).Service
	if extra == nil {
		return pubsub.ValidationReject, nil
	}
	if msg.Eon > math.MaxInt64 {
		return pubsub.ValidationReject, fmt.Errorf("eon %d overflows int64", msg.Eon)
	}
	if len(msg.Keys) == 0 {
		return pubsub.ValidationReject, fmt.Errorf("no keys in message")
	}

	return pubsub.ValidationAccept, nil
}

func (mh *MsgHandler) HandleMessage(ctx context.Context, msg p2pmsg.Message) ([]p2pmsg.Message, error) {
	// TODO: Implement decryption keys handling
	return []p2pmsg.Message{}, nil
}
