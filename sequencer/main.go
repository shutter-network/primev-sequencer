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
	"primev-poc/transaction_verifier"
	"primev-poc/txhandler"
)

var (
	logLevel           string
	InclusionWindow    = uint64(1)
	MaxInclusionWindow = uint64(30)
)

type SequencerConfig struct {
	RpcPort                 string
	ApiPort                 string
	UpstreamRPCURL          string
	GrpcAddr                string
	InstanceId              uint64
	p2pConfig               p2p.Config
	bidderNodeAddress       string
	KeyperSetManagerAddress string
	KeyBroadcastAddress     string
	VerificationInterval    time.Duration
}

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
			return runSequencer()
		},
	}

	rootCmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	return rootCmd
}

func runSequencer() error {

	config, err := readFromEnv()
	if err != nil {
		return fmt.Errorf("failed to read from env: %w", err)
	}

	zlog.Info().
		Str("rpc-port", config.RpcPort).
		Str("api-port", config.ApiPort).
		Str("upstream-rpc", config.UpstreamRPCURL).
		Str("grpc-addr", config.GrpcAddr).
		Msg("Starting PrimeV sequencer")

	txHandler := txhandler.NewTransactionHandler()

	// Start Transaction Verifier
	verifier, err := transaction_verifier.NewTransactionVerifier(config.UpstreamRPCURL, txHandler, config.VerificationInterval)
	if err != nil {
		return fmt.Errorf("failed to create transaction verifier: %w", err)
	}

	restAPI := api.NewRestAPI(txHandler, config.ApiPort)

	p2p := primevp2p.NewP2P(&config.p2pConfig, txHandler, config.bidderNodeAddress)

	rpcConfig := &rpc.Config{
		Port:                    config.RpcPort,
		UpstreamRPCURL:          config.UpstreamRPCURL,
		KeyperSetManagerAddress: config.KeyperSetManagerAddress,
		KeyBroadcastAddress:     config.KeyBroadcastAddress,
		BidderNodeAddress:       config.bidderNodeAddress,
	}

	rpcServer, err := rpc.NewRPCServer(rpcConfig, txHandler, MaxInclusionWindow)
	if err != nil {
		return fmt.Errorf("failed to create RPC server: %w", err)
	}

	err = startSequencerModule(txHandler, p2p, config.GrpcAddr, config.InstanceId)
	if err != nil {
		return fmt.Errorf("failed to start sequencer module: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	service.Run(ctx, p2p, rpcServer, restAPI, verifier)

	zlog.Info().Msg("All modules started successfully")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	cancel()

	zlog.Info().Msg("Shutting down all modules...")

	zlog.Info().Msg("All modules stopped")
	return nil
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

func readFromEnv() (*SequencerConfig, error) {
	// Get config values from environment variables
	rpcPort := getEnvOrDefault("RPC_PORT", "8545")
	_, err := strconv.ParseInt(rpcPort, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse RPC_PORT: %w", err)
	}
	apiPort := getEnvOrDefault("API_PORT", "8080")
	_, err = strconv.ParseInt(apiPort, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse API_PORT: %w", err)
	}
	upstreamRPCURL := getEnvOrDefault("UPSTREAM_RPC_URL", "http://localhost:8546")
	keyperSetManagerAddress := os.Getenv("KEYPER_SET_MANAGER_ADDRESS")
	keyBroadcastAddress := os.Getenv("KEY_BROADCAST_ADDRESS")
	grpcAddr := os.Getenv("GRPC_ADDR")
	instanceId := os.Getenv("INSTANCE_ID")
	bidderNodeAddress := os.Getenv("BIDDER_NODE_ADDRESS")
	if keyperSetManagerAddress == "" {
		return nil, fmt.Errorf("KEYPER_SET_MANAGER_ADDRESS environment variable is required")
	}
	if keyBroadcastAddress == "" {
		return nil, fmt.Errorf("KEY_BROADCAST_ADDRESS environment variable is required")
	}
	if grpcAddr == "" {
		return nil, fmt.Errorf("GRPC_ADDR environment variable is required")
	}
	if instanceId == "" {
		return nil, fmt.Errorf("INSTANCE_ID environment variable is required")
	}
	instanceIdUint64, err := strconv.ParseUint(instanceId, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse INSTANCE_ID: %w", err)
	}

	p2pConfig := p2p.Config{}
	var p2pKey keys.Libp2pPrivate
	p2pKeyString := os.Getenv("P2P_KEY")
	if p2pKeyString == "" {
		return nil, fmt.Errorf("P2P key not provided in the env")
	}
	if err := p2pKey.UnmarshalText([]byte(p2pKeyString)); err != nil {
		return nil, fmt.Errorf("error unmarshalling P2P key: %w", err)
	}
	p2pConfig.P2PKey = &p2pKey

	bootstrapAddressesStringified := os.Getenv("P2P_BOOTSTRAP_ADDRESSES")
	if bootstrapAddressesStringified == "" {
		return nil, fmt.Errorf("bootstrap addresses not provided in the env")
	}
	bootstrapAddresses := strings.Split(bootstrapAddressesStringified, ",")

	bootstrapP2PAddresses := make([]*address.P2PAddress, len(bootstrapAddresses))

	for i, addr := range bootstrapAddresses {
		bootstrapP2PAddresses[i] = address.MustP2PAddress(addr)
	}
	p2pConfig.CustomBootstrapAddresses = bootstrapP2PAddresses

	p2pPort := getEnvOrDefault("P2P_PORT", "23003")
	_, err = strconv.ParseInt(p2pPort, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse P2P_PORT: %w", err)
	}

	p2pConfig.ListenAddresses = []*address.P2PAddress{
		address.MustP2PAddress("/ip4/0.0.0.0/tcp/" + p2pPort),
		address.MustP2PAddress("/ip4/0.0.0.0/udp/" + p2pPort + "/quic-v1"),
		address.MustP2PAddress("/ip4/0.0.0.0/udp/" + p2pPort + "/quic-v1/webtransport"),
		address.MustP2PAddress("/ip6/::/tcp/" + p2pPort),
		address.MustP2PAddress("/ip6/::/udp/" + p2pPort + "/quic-v1"),
		address.MustP2PAddress("/ip6/::/udp/" + p2pPort + "/quic-v1/webtransport"),
	}
	p2pEnviroment, err := strconv.ParseInt(getEnvOrDefault("P2P_ENVIRONMENT", "0"), 10, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse p2p environment: %w", err)
	}
	p2pConfig.Environment = env.Environment(p2pEnviroment)
	p2pConfig.DiscoveryNamespace = getEnvOrDefault("P2P_DISCOVERY_NAMESPACE", "primev-poc")

	verificationInterval := getEnvOrDefault("VERIFICATION_INTERVAL", "10s")
	interval, err := time.ParseDuration(verificationInterval)
	if err != nil {
		return nil, fmt.Errorf("failed to parse VERIFICATION_INTERVAL: %w", err)
	}

	return &SequencerConfig{
		RpcPort:                 rpcPort,
		ApiPort:                 apiPort,
		UpstreamRPCURL:          upstreamRPCURL,
		GrpcAddr:                grpcAddr,
		InstanceId:              instanceIdUint64,
		p2pConfig:               p2pConfig,
		bidderNodeAddress:       bidderNodeAddress,
		KeyperSetManagerAddress: keyperSetManagerAddress,
		KeyBroadcastAddress:     keyBroadcastAddress,
		VerificationInterval:    interval,
	}, nil
}
