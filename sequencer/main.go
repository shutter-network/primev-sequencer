package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	bidderapiv1 "github.com/primev/mev-commit/p2p/gen/go/bidderapi/v1"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/medley/encodeable/address"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/medley/encodeable/env"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/medley/encodeable/keys"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/medley/service"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/p2p"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/p2pmsg"
	"github.com/spf13/cobra"

	"primev-poc/api"
	"primev-poc/bidder"
	primevp2p "primev-poc/p2p"
	"primev-poc/rpc"
	"primev-poc/shutter"
	"primev-poc/txhandler"
)

var (
	logLevel           string
	InclusionWindow    = uint64(1)
	MaxInclusionWindow = uint64(30)
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
	apiPort := getEnvOrDefault("API_PORT", "8080")
	upstreamRPCURL := getEnvOrDefault("UPSTREAM_RPC_URL", "http://localhost:8546")
	keyperSetManagerAddress := os.Getenv("KEYPER_SET_MANAGER_ADDRESS")
	keyBroadcastAddress := os.Getenv("KEY_BROADCAST_ADDRESS")
	grpcAddr := os.Getenv("GRPC_ADDR")
	instanceId := os.Getenv("INSTANCE_ID")
	bidderNodeAddress := os.Getenv("BIDDER_NODE_ADDRESS")
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
		Str("api-port", apiPort).
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

	// Start REST API server
	restAPI := api.NewRestAPI(txHandler)
	apiMux := restAPI.SetupRoutes()
	apiServer := &http.Server{
		Addr:    ":" + apiPort,
		Handler: apiMux,
	}

	// Start API server in a goroutine
	go func() {
		zlog.Info().Str("port", apiPort).Msg("Starting REST API server")
		if err := apiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zlog.Error().Err(err).Msg("REST API server error")
		}
	}()

	p2p := primevp2p.NewP2P(&p2pConfig, txHandler, bidderNodeAddress)

	config := &rpc.Config{
		Port:                    rpcPort,
		UpstreamRPCURL:          upstreamRPCURL,
		KeyperSetManagerAddress: keyperSetManagerAddress,
		KeyBroadcastAddress:     keyBroadcastAddress,
		BidderNodeAddress:       bidderNodeAddress,
	}

	rpcServer, err := rpc.NewRPCServer(config, txHandler, MaxInclusionWindow)
	if err != nil {
		return fmt.Errorf("failed to create RPC server: %w", err)
	}

	err = startSequencerModule(txHandler, p2p, grpcAddr, instanceIdUint64)
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

	// Gracefully shutdown API server
	apiCtx, apiCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer apiCancel()
	if err := apiServer.Shutdown(apiCtx); err != nil {
		zlog.Error().Err(err).Msg("API server shutdown error")
	} else {
		zlog.Info().Msg("API server stopped")
	}

	zlog.Info().Msg("All modules stopped")
	return nil
}

func startTransactionHandler() *txhandler.TransactionHandler {
	zlog.Info().Msg("Starting transaction handler module")

	txHandler := txhandler.NewTransactionHandler()

	zlog.Info().Msg("Transaction handler module started successfully")
	return txHandler
}

func startSequencerModule(txHandler *txhandler.TransactionHandler, p2p *primevp2p.P2P, grpcAddr string, instanceId uint64) error {
	zlog.Info().Msg("Starting sequencer core module")

	bidManager := bidder.NewBidManager(txHandler)

	go func() {
		zlog.Info().Msg("Sequencer core module running")

		statusTicker := time.NewTicker(30 * time.Second)
		defer statusTicker.Stop()

		bidTicker := time.NewTicker(30 * time.Second)
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
				currentBlock, err := shutter.GetCurrentBlockNumber()
				if err != nil {
					zlog.Error().Err(err).Msg("Failed to get current block number")
					continue
				}
				inclusionBlock := currentBlock + InclusionWindow
				bid, txHashes, err := bidManager.CreateBidFromInitTransactions(inclusionBlock)
				if err != nil {
					zlog.Error().Err(err).Msg("Failed to create bids from init transactions")
					continue
				}

				if bid != nil {
					zlog.Info().
						Uint64("block_number", uint64(bid.BlockNumber)).
						Int("tx_count", len(txHashes)).
						Str("amount", bid.Amount).
						Uint64("decay_start", uint64(bid.DecayStartTimestamp)).
						Uint64("decay_end", uint64(bid.DecayEndTimestamp)).
						Str("slash_amount", bid.SlashAmount).
						Msg("Bid created")

					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					commitments, err := bidManager.SubmitBidGRPC(ctx, grpcAddr, bid)
					cancel()
					if err != nil || len(commitments) == 0 {
						zlog.Error().Err(err).Msg("Failed to submit bid to gRPC server")
						for _, txHash := range txHashes {
							err = txHandler.UpdateTransactionStatus(txHash, txhandler.StatusInit)
							if err != nil {
								zlog.Error().Err(err).Msg("Failed to update transaction status")
								continue
							}
						}
						continue
					}
					zlog.Info().Int("commitment_count", len(commitments)).Msg("Received commitments from gRPC server")

					for _, c := range commitments {
						identityPrefixes, err := getIdentityPrefixes(c.BidOptions)
						if err != nil {
							zlog.Error().Err(err).Msg("Failed to get identities")
							continue
						}
						ctx, _ := context.WithTimeout(context.Background(), 2*time.Minute)
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
							Identities:           identityPrefixes,
						})
						if err != nil {
							zlog.Error().Err(err).Msg("Failed to send commitment to keypers")
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
	zlog.Logger = zlog.Output(zerolog.ConsoleWriter{Out: os.Stderr})
}

func getIdentityPrefixes(bidOptions *bidderapiv1.BidOptions) ([]string, error) {
	identities := make([]string, 0)
	for _, option := range bidOptions.Options {
		switch option.GetOpt().(type) {
		case *bidderapiv1.BidOption_ShutterisedBidOption:
			identities = append(identities, option.GetShutterisedBidOption().IdentityPrefix)
		default:
			return nil, fmt.Errorf("invalid bid option")
		}
	}
	return identities, nil
}
