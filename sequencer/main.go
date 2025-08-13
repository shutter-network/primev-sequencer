package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/medley/encodeable/address"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/medley/encodeable/env"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/medley/encodeable/keys"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/medley/service"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/p2p"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/p2pmsg"
	"github.com/spf13/cobra"

	"primev-poc/bidder"
	primevp2p "primev-poc/p2p"
	"primev-poc/rpc"
	"primev-poc/txhandler"
)

var (
	logLevel string
)

func main() {
	status := 0

	if err := Cmd().Execute(); err != nil {
		zlog.Error().Err(err).Msg("failed running server")
		status = 1
	}
	os.Exit(status)
}

func Cmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "primev",
		Short: "PrimeV PoC - Sequencer",
		Long:  "A proof of concept implementation that starts the sequencer with integrated RPC server and other modules",
		RunE: func(cmd *cobra.Command, args []string) error {
			setupLogging()
			return startSequencer()
		},
	}

	rootCmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	return rootCmd
}

func startSequencer() error {
	// Get config values from environment variables
	rpcPort := getEnvOrDefault("RPC_PORT", "8545")
	upstreamRPCURL := getEnvOrDefault("UPSTREAM_RPC_URL", "http://localhost:8546")
	keyperSetManagerAddress := os.Getenv("KEYPER_SET_MANAGER_ADDRESS")
	keyBroadcastAddress := os.Getenv("KEY_BROADCAST_ADDRESS")
	grpcAddr := os.Getenv("GRPC_ADDR")
	instanceId := os.Getenv("INSTANCE_ID")

	// Validate required environment variables
	if keyperSetManagerAddress == "" {
		return fmt.Errorf("KEYPER_SET_MANAGER_ADDRESS environment variable is required")
	}
	if keyBroadcastAddress == "" {
		return fmt.Errorf("KEY_BROADCAST_ADDRESS environment variable is required")
	}
	if grpcAddr == "" {
		return fmt.Errorf("GRPC_ADDR environment variable is required")
	}
	if instanceId == "" {
		return fmt.Errorf("INSTANCE_ID environment variable is required")
	}
	instanceIdUint64, err := strconv.ParseUint(instanceId, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse INSTANCE_ID: %w", err)
	}

	zlog.Info().
		Str("rpc-port", rpcPort).
		Str("upstream-rpc", upstreamRPCURL).
		Str("grpc-addr", grpcAddr).
		Msg("Starting PrimeV sequencer with integrated modules")

	p2pConfig := p2p.Config{}
	var p2pKey keys.Libp2pPrivate
	p2pKeyString := os.Getenv("P2P_KEY")
	if p2pKeyString == "" {
		panic("P2P key not provided in the env")
	}
	if err := p2pKey.UnmarshalText([]byte(p2pKeyString)); err != nil {
		panic("error unmarshalling P2P key")
	}
	p2pConfig.P2PKey = &p2pKey

	bootstrapAddressesStringified := os.Getenv("P2P_BOOTSTRAP_ADDRESSES")
	if bootstrapAddressesStringified == "" {
		panic("bootstrap addresses not provided in the env")
	}
	bootstrapAddresses := strings.Split(bootstrapAddressesStringified, ",")

	bootstrapP2PAddresses := make([]*address.P2PAddress, len(bootstrapAddresses))

	for i, addr := range bootstrapAddresses {
		bootstrapP2PAddresses[i] = address.MustP2PAddress(addr)
	}
	p2pConfig.CustomBootstrapAddresses = bootstrapP2PAddresses

	p2pPort := os.Getenv("P2P_PORT")
	if p2pPort == "" {
		p2pPort = "23003"
	}

	p2pConfig.ListenAddresses = []*address.P2PAddress{
		address.MustP2PAddress("/ip4/0.0.0.0/tcp/" + p2pPort),
		address.MustP2PAddress("/ip4/0.0.0.0/udp/" + p2pPort + "/quic-v1"),
		address.MustP2PAddress("/ip4/0.0.0.0/udp/" + p2pPort + "/quic-v1/webtransport"),
		address.MustP2PAddress("/ip6/::/tcp/" + p2pPort),
		address.MustP2PAddress("/ip6/::/udp/" + p2pPort + "/quic-v1"),
		address.MustP2PAddress("/ip6/::/udp/" + p2pPort + "/quic-v1/webtransport"),
	}
	p2pEnviroment, err := strconv.ParseInt(os.Getenv("P2P_ENVIRONMENT"), 10, 0)
	if err != nil {
		return fmt.Errorf("failed to parse p2p environment: %w", err)
	}
	p2pConfig.Environment = env.Environment(p2pEnviroment)
	p2pConfig.DiscoveryNamespace = os.Getenv("P2P_DISCOVERY_NAMESPACE")

	txHandler := startTransactionHandler()

	p2p := primevp2p.NewP2P(&p2pConfig)

	config := &rpc.Config{
		Port:                    rpcPort,
		UpstreamRPCURL:          upstreamRPCURL,
		KeyperSetManagerAddress: keyperSetManagerAddress,
		KeyBroadcastAddress:     keyBroadcastAddress,
	}

	rpcServer, err := rpc.NewRPCServer(config, txHandler)
	if err != nil {
		return fmt.Errorf("failed to create RPC server: %w", err)
	}

	err = startSequencerModule(txHandler, grpcAddr, p2p, instanceIdUint64)
	if err != nil {
		return fmt.Errorf("failed to start sequencer module: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	service.Run(ctx, p2p, rpcServer)

	zlog.Info().Msg("All modules started successfully")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	cancel()

	zlog.Info().Msg("Shutting down all modules...")

	zlog.Info().Msg("All modules stopped")
	return nil
}

func startTransactionHandler() *txhandler.TransactionHandler {
	zlog.Info().Msg("Starting transaction handler module")

	txHandler := txhandler.NewTransactionHandler()

	zlog.Info().Msg("Transaction handler module started successfully")
	return txHandler
}

func startSequencerModule(txHandler *txhandler.TransactionHandler, grpcAddr string, p2p *primevp2p.P2P, instanceId uint64) error {
	zlog.Info().Msg("Starting sequencer core module")

	bidManager := bidder.NewBidManager(txHandler)

	go func() {
		zlog.Info().Msg("Sequencer core module running")

		statusTicker := time.NewTicker(30 * time.Second)
		defer statusTicker.Stop()

		bidTicker := time.NewTicker(60 * time.Second)
		defer bidTicker.Stop()

		for {
			select {
			case <-statusTicker.C:
				count := txHandler.GetTransactionCount()
				statusCounts := txHandler.GetStatusCounts()
				zlog.Info().
					Int("total_transactions", count).
					Interface("status_counts", statusCounts).
					Msg("Transaction handler status")

			case <-bidTicker.C:
				bids, err := bidManager.CreateBidsFromInitTransactions()
				if err != nil {
					zlog.Error().Err(err).Msg("Failed to create bids from init transactions")
					continue
				}

				if len(bids) > 0 {
					zlog.Info().
						Int("bid_count", len(bids)).
						Msg("Created bids from init transactions")

					for _, bid := range bids {
						zlog.Info().
							Uint64("block_number", uint64(bid.BlockNumber)).
							Int("tx_count", len(bid.TxHashes)).
							Str("amount", bid.Amount).
							Uint64("decay_start", uint64(bid.DecayStartTimestamp)).
							Uint64("decay_end", uint64(bid.DecayEndTimestamp)).
							Str("slash_amount", bid.SlashAmount).
							Msg("📋 Bid created")

						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
						commitments, err := bidManager.SubmitBidGRPC(ctx, grpcAddr, bid)
						cancel()
						if err != nil {
							zlog.Error().Err(err).Msg("Failed to submit bid to gRPC server")
							continue
						}
						zlog.Info().Int("commitment_count", len(commitments)).Msg("Received commitments from gRPC server")

						for _, c := range commitments {
							err = p2p.SendMessage(ctx, &p2pmsg.Commitment{
								InstanceId:           instanceId,
								TxHashes:             c.GetTxHashes(),
								BidAmount:            c.GetBidAmount(),
								BlockNumber:          c.GetBlockNumber(),
								ReceivedBidDigest:    c.GetReceivedBidDigest(),
								ReceivedBidSignature: c.GetReceivedBidSignature(),
								CommitmentDigest:     c.GetCommitmentDigest(),
								CommitmentSignature:  c.GetCommitmentSignature(),
								ProviderAddress:      c.GetProviderAddress(),
								DecayStartTimestamp:  c.GetDecayStartTimestamp(),
								DecayEndTimestamp:    c.GetDecayEndTimestamp(),
								DispatchTimestamp:    c.GetDispatchTimestamp(),
								RevertingTxHashes:    c.GetRevertingTxHashes(),
								SlashAmount:          c.GetSlashAmount(),
							})
							if err != nil {
								zlog.Error().Err(err).Msg("Failed to send commitment")
								continue
							}
							zlog.Info().Msg("Sent commitment")

							zlog.Info().
								Strs("tx_hashes", c.GetTxHashes()).
								Str("provider_address", c.GetProviderAddress()).
								Str("commitment_digest", c.GetCommitmentDigest()).
								Msg("Commitment received and transactions marked as committed")
						}
					}
				}
			}
		}
	}()

	return nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func setupLogging() {
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	if logLevel == "debug" {
		zlog.Logger = zlog.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}
}
