package p2p

import (
	"context"
	"log"
	"primev-poc/txstore"

	"github.com/shutter-network/rolling-shutter/rolling-shutter/medley/service"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/p2p"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/p2pmsg"
)

type P2P struct {
	config            *p2p.Config
	service           *p2p.P2PMessaging
	txStore           *txstore.TransactionStore
	bidderNodeAddress string
}

func NewP2P(config *p2p.Config, txStore *txstore.TransactionStore, bidderNodeAddress string) *P2P {
	service, err := p2p.New(config)
	if err != nil {
		log.Fatal(err)
	}
	return &P2P{config: config, service: service, txStore: txStore, bidderNodeAddress: bidderNodeAddress}
}

func (p *P2P) Start(ctx context.Context, runner service.Runner) error {
	msgHandler := NewDecryptionKeysMsgHandler(p.txStore, &HandlerConfig{BidderNodeAddress: p.bidderNodeAddress})
	p.service.AddMessageHandler(msgHandler)
	return runner.StartService(p.service)
}

func (p *P2P) SendMessage(ctx context.Context, msg p2pmsg.Message) error {
	return p.service.SendMessage(ctx, msg)
}
