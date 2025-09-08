package shutter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog/log"
	"github.com/shutter-network/contracts/v2/bindings/keybroadcastcontract"
	"github.com/shutter-network/contracts/v2/bindings/keypersetmanager"
	"github.com/shutter-network/shutter/shlib/shcrypto"

	"primev-poc/txhandler"
	"primev-poc/utils"
)

type Config struct {
	EthereumRPCURL          string
	KeyperSetManagerAddress string
	KeyBroadcastAddress     string
	MaxInclusionWindow      uint64
}

var (
	config *Config
)

func Initialize(cfg *Config) {
	config = cfg
	log.Info().
		Str("ethereum_rpc", cfg.EthereumRPCURL).
		Str("keyper_set_manager", cfg.KeyperSetManagerAddress).
		Str("key_broadcast", cfg.KeyBroadcastAddress).
		Uint64("max_inclusion_window", cfg.MaxInclusionWindow).
		Msg("Initialized shutter package configuration")
}

func EncryptTransaction(rawTx string, txHash common.Hash, bidderNodeAddress string) (*txhandler.EncryptedTransaction, common.Hash, error) {
	if config == nil {
		return nil, common.Hash{}, fmt.Errorf("shutter package not initialized - call Initialize() first")
	}

	currentBlockNum, err := GetCurrentBlockNumber()
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("failed to get current block number: %w", err)
	}

	scheduledBlock := currentBlockNum + config.MaxInclusionWindow

	eonID, err := getEonForBlock(scheduledBlock)
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("failed to get eon for scheduled block: %w", err)
	}

	eonPublicKey, err := fetchEonKeyForEon(eonID)
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("failed to fetch eon public key for eon %d: %w", eonID, err)
	}

	txData, err := hex.DecodeString(rawTx[2:])
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("failed to decode raw transaction: %w", err)
	}

	identity := utils.GetIdentityPrefix(txHash, bidderNodeAddress)

	sigmaBlock, err := shcrypto.RandomSigma(rand.Reader)
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("failed to generate random sigma: %w", err)
	}

	sigma := sigmaBlock[:]

	encryptedData, err := encryptWithShutter(txData, eonID, sigma, eonPublicKey, identity)
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("failed to encrypt transaction: %w", err)
	}

	encryptedTx := &txhandler.EncryptedTransaction{
		EonID:              eonID,
		MaxInclusionWindow: scheduledBlock,
		EncryptedTx:        encryptedData,
		TxHash:             txHash.Bytes(),
		Identity:           hex.EncodeToString(identity),
	}

	log.Info().
		Str("identity", hex.EncodeToString(identity)).
		Str("tx_hash", txHash.Hex()).
		Uint64("current_block", currentBlockNum).
		Uint64("expected_scheduled_block", scheduledBlock).
		Uint64("eon_id", eonID).
		Msg("Successfully encrypted transaction with Shutter threshold cryptography")

	return encryptedTx, txHash, nil
}

func encryptWithShutter(data []byte, eonID uint64, sigma []byte, eonPublicKey []byte, identity []byte) ([]byte, error) {
	eonPubKey := &shcrypto.EonPublicKey{}
	if err := eonPubKey.Unmarshal(eonPublicKey); err != nil {
		return nil, fmt.Errorf("failed to parse eon public key: %w", err)
	}

	epochID := shcrypto.ComputeEpochID(identity)

	var sigmaBlock shcrypto.Block
	if len(sigma) != 32 {
		return nil, fmt.Errorf("sigma must be exactly 32 bytes, got %d", len(sigma))
	}
	copy(sigmaBlock[:], sigma)

	encryptedMessage := shcrypto.Encrypt(data, eonPubKey, epochID, sigmaBlock)
	if encryptedMessage == nil {
		return nil, fmt.Errorf("shutter encryption failed - encrypted message is nil")
	}

	encryptedData := encryptedMessage.Marshal()

	log.Debug().
		Int("original_data_size", len(data)).
		Int("encrypted_data_size", len(encryptedData)).
		Uint64("eon_id", eonID).
		Str("identity", hex.EncodeToString(identity)).
		Msg("Successfully encrypted transaction data with Shutter threshold cryptography")

	return encryptedData, nil
}

func GetCurrentBlockNumber() (uint64, error) {
	if config == nil {
		return 0, fmt.Errorf("shutter package not initialized - call Initialize() first")
	}
	client, err := ethclient.Dial(config.EthereumRPCURL)
	if err != nil {
		return 0, fmt.Errorf("failed to connect to Ethereum client: %w", err)
	}
	defer client.Close()

	blockNumber, err := client.BlockNumber(context.Background())
	if err != nil {
		return 0, fmt.Errorf("failed to get block number: %w", err)
	}

	return blockNumber, nil
}

func getEonForBlock(blockNumber uint64) (uint64, error) {
	if config == nil {
		return 0, fmt.Errorf("shutter package not initialized - call Initialize() first")
	}
	client, err := ethclient.Dial(config.EthereumRPCURL)
	if err != nil {
		return 0, fmt.Errorf("failed to connect to Ethereum client: %w", err)
	}
	defer client.Close()

	keyperSetManagerAddress := common.HexToAddress(config.KeyperSetManagerAddress)
	keyperSetManager, err := keypersetmanager.NewKeypersetmanager(keyperSetManagerAddress, client)
	if err != nil {
		return 0, fmt.Errorf("failed to create KeyperSetManager instance: %w", err)
	}

	callOpts := &bind.CallOpts{Context: context.Background()}
	eonID, err := keyperSetManager.GetKeyperSetIndexByBlock(callOpts, blockNumber)
	if err != nil {
		return 0, fmt.Errorf("failed to get eon for block %d: %w", blockNumber, err)
	}

	log.Debug().
		Uint64("block_number", blockNumber).
		Uint64("eon_id", eonID).
		Msg("Retrieved eon for block")

	return eonID, nil
}

func fetchEonKeyForEon(eonID uint64) ([]byte, error) {
	if config == nil {
		return nil, fmt.Errorf("shutter package not initialized - call Initialize() first")
	}
	client, err := ethclient.Dial(config.EthereumRPCURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum client: %w", err)
	}
	defer client.Close()

	keyBroadcastAddress := common.HexToAddress(config.KeyBroadcastAddress)
	keyBroadcast, err := keybroadcastcontract.NewKeybroadcastcontract(keyBroadcastAddress, client)
	if err != nil {
		return nil, fmt.Errorf("failed to create KeyBroadcast instance: %w", err)
	}

	callOpts := &bind.CallOpts{Context: context.Background()}
	eonKey, err := keyBroadcast.GetEonKey(callOpts, eonID)
	if err != nil {
		return nil, fmt.Errorf("failed to get eon key for eon %d: %w", eonID, err)
	}

	log.Debug().
		Uint64("eon_id", eonID).
		Int("key_length", len(eonKey)).
		Msg("Retrieved eon key")

	return eonKey, nil
}
