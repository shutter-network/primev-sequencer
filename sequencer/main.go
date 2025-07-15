package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"primev-poc/bidder"
	"primev-poc/rpc"
	"primev-poc/txhandler"
)

var (
	logLevel string

	rpcPort                 string
	upstreamRPCURL          string
	keyperSetManagerAddress string
	keyBroadcastAddress     string
)

func main() {
	status := 0

	if err := Cmd().Execute(); err != nil {
		log.Printf("failed running server: %v", err)
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
	rootCmd.Flags().StringVar(&rpcPort, "rpc-port", "8545", "Port for encrypted RPC server to listen on")
	rootCmd.Flags().StringVar(&upstreamRPCURL, "upstream-rpc", "http://localhost:8546", "Upstream RPC URL to proxy requests to")
	rootCmd.Flags().StringVar(&keyperSetManagerAddress, "keyper-set-manager-address", "", "Shutter KeyperSetManager contract address")
	rootCmd.Flags().StringVar(&keyBroadcastAddress, "key-broadcast-address", "", "Shutter Key Broadcast contract address")

	rootCmd.MarkFlagRequired("keyper-set-manager-address")
	rootCmd.MarkFlagRequired("key-broadcast-address")

	return rootCmd
}

func startSequencer() error {
	zlog.Info().
		Str("rpc-port", rpcPort).
		Str("upstream-rpc", upstreamRPCURL).
		Msg("Starting PrimeV sequencer with integrated modules")

	txHandler := startTransactionHandler()

	rpcServer, err := startRPCModule(txHandler)
	if err != nil {
		return fmt.Errorf("failed to start RPC module: %w", err)
	}

	err = startSequencerModule(txHandler)
	if err != nil {
		return fmt.Errorf("failed to start sequencer module: %w", err)
	}

	zlog.Info().Msg("All modules started successfully")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	zlog.Info().Msg("Shutting down all modules...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := rpcServer.Shutdown(ctx); err != nil {
		zlog.Error().Err(err).Msg("Failed to shutdown RPC server gracefully")
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

func startRPCModule(txHandler *txhandler.TransactionHandler) (*http.Server, error) {
	zlog.Info().
		Str("port", rpcPort).
		Msg("Starting encrypted RPC module")

	config := &rpc.Config{
		Port:                    rpcPort,
		UpstreamRPCURL:          upstreamRPCURL,
		KeyperSetManagerAddress: keyperSetManagerAddress,
		KeyBroadcastAddress:     keyBroadcastAddress,
	}

	server, err := rpc.NewRPCServer(config, txHandler)
	if err != nil {
		return nil, fmt.Errorf("failed to create RPC server: %w", err)
	}

	httpServer := &http.Server{
		Addr:    ":" + rpcPort,
		Handler: server,
	}

	go func() {
		zlog.Info().Str("address", httpServer.Addr).Msg("RPC module listening")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zlog.Fatal().Err(err).Msg("RPC module failed")
		}
	}()

	return httpServer, nil
}

func startSequencerModule(txHandler *txhandler.TransactionHandler) error {
	zlog.Info().Msg("Starting sequencer core module")

	// Initialize bid manager
	bidManager := bidder.NewBidManager(txHandler)

	go func() {
		zlog.Info().Msg("Sequencer core module running")

		// Status reporting ticker (every 30 seconds)
		statusTicker := time.NewTicker(30 * time.Second)
		defer statusTicker.Stop()

		// Bid creation ticker (every 60 seconds)
		bidTicker := time.NewTicker(60 * time.Second)
		defer bidTicker.Stop()

		for {
			select {
			case <-statusTicker.C:
				// Report transaction handler status
				count := txHandler.GetTransactionCount()
				statusCounts := txHandler.GetStatusCounts()
				zlog.Info().
					Int("total_transactions", count).
					Interface("status_counts", statusCounts).
					Msg("Transaction handler status")

			case <-bidTicker.C:
				// Create bids from init transactions
				bids, err := bidManager.CreateBidsFromInitTransactions()
				if err != nil {
					zlog.Error().Err(err).Msg("Failed to create bids from init transactions")
					continue
				}

				if len(bids) > 0 {
					zlog.Info().
						Int("bid_count", len(bids)).
						Msg("Created bids from init transactions")

					// Log details of each bid
					for _, bid := range bids {
						zlog.Info().
							Uint64("block_number", bid.BlockNumber).
							Int("tx_count", len(bid.TxHashes)).
							Str("amount", bid.Amount).
							Uint64("decay_start", bid.DecayStartTimestamp).
							Uint64("decay_end", bid.DecayEndTimestamp).
							Str("slash_amount", bid.SlashAmount).
							Msg("📋 Bid created")
					}

					// Here you would typically submit the bids to PrimeV
					// For now, we just log them
					zlog.Info().Msg("Bids would be submitted to PrimeV bidder auction")
				}
			}
		}
	}()

	return nil
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
