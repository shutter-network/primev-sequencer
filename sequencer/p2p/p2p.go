package p2p

import (
	"context"
	"log"
	"primev-poc/txhandler"

	"github.com/shutter-network/rolling-shutter/rolling-shutter/medley/service"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/p2p"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/p2pmsg"
)

type P2P struct {
	config            *p2p.Config
	service           *p2p.P2PMessaging
	txHandler         *txhandler.TransactionHandler
	bidderNodeAddress string
}

func NewP2P(config *p2p.Config, txHandler *txhandler.TransactionHandler, bidderNodeAddress string) *P2P {
	service, err := p2p.New(config)
	if err != nil {
		log.Fatal(err)
	}
	return &P2P{config: config, service: service, txHandler: txHandler, bidderNodeAddress: bidderNodeAddress}
}

func (p *P2P) Start(ctx context.Context, runner service.Runner) error {
	msgHandler := NewDecryptionKeysMsgHandler(p.txHandler, &HandlerConfig{BidderNodeAddress: p.bidderNodeAddress})
	p.service.AddMessageHandler(msgHandler)
	return runner.StartService(p.service)
}

func (p *P2P) SendMessage(ctx context.Context, msg p2pmsg.Message) error {
	return p.service.SendMessage(ctx, msg)
}
