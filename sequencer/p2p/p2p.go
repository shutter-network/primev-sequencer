package p2p

import (
	"context"

	"github.com/shutter-network/rolling-shutter/rolling-shutter/medley/service"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/p2p"
)

type P2P struct {
	config *p2p.Config
}

func NewP2P(config *p2p.Config) *P2P {
	return &P2P{config: config}
}

func (p *P2P) Start(ctx context.Context, runner service.Runner) error {
	service, err := p2p.New(p.config)
	if err != nil {
		return err
	}
	service.AddMessageHandler(&MsgHandler{})
	return nil
}
