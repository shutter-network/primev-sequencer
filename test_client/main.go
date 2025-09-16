package main

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	// RPC server endpoint (from your logs, it's running on 8546)
	rpcURL = "http://localhost:8545"

	// Gnosis Chiado testnet chain ID
	chainID = 560048
)

type JSONRPCRequest struct {
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	JSONRPC string      `json:"jsonrpc"`
}

type JSONRPCResponse struct {
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
	JSONRPC string      `json:"jsonrpc"`
}

type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func main() {
	// Setup logging
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	log.Info().Msg("🚀 Starting PrimeV RPC Test Client")

	// Get private key from environment variable set by run script
	privateKey, err := getPrivateKey()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to get private key")
	}

	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	log.Info().Str("address", fromAddress.Hex()).Msg("Using test address")

	// Test basic getter methods first
	testGetterMethods()

	// Test transaction sending
	testSendTransaction(privateKey, fromAddress)

	// Test more getter methods after transaction
	testPostTransactionQueries(fromAddress)

	log.Info().Msg("✅ Test client completed successfully")
}

func getPrivateKey() (*ecdsa.PrivateKey, error) {
	// Get private key from environment variable (set by run script)
	envKey := os.Getenv("PRIVATE_KEY")
	if envKey == "" {
		return nil, fmt.Errorf("PRIVATE_KEY environment variable not set")
	}

	// Remove 0x prefix if present
	keyHex := strings.TrimPrefix(envKey, "0x")

	privateKey, err := crypto.HexToECDSA(keyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	log.Info().Msg("Using private key from environment variable")
	return privateKey, nil
}

func testGetterMethods() {
	log.Info().Msg("📊 Testing getter methods...")

	// Test eth_blockNumber
	blockNumber, err := makeRPCCall("eth_blockNumber", []interface{}{})
	if err != nil {
		log.Error().Err(err).Msg("Failed to get block number")
	} else {
		log.Info().Interface("block_number", blockNumber).Msg("Current block number")
	}

	// Test eth_gasPrice
	gasPrice, err := makeRPCCall("eth_gasPrice", []interface{}{})
	if err != nil {
		log.Error().Err(err).Msg("Failed to get gas price")
	} else {
		log.Info().Interface("gas_price", gasPrice).Msg("Current gas price")
	}

	// Test eth_chainId
	chainIdResult, err := makeRPCCall("eth_chainId", []interface{}{})
	if err != nil {
		log.Error().Err(err).Msg("Failed to get chain ID")
	} else {
		log.Info().Interface("chain_id", chainIdResult).Msg("Chain ID")
	}

	// Test web3_clientVersion
	clientVersion, err := makeRPCCall("web3_clientVersion", []interface{}{})
	if err != nil {
		log.Error().Err(err).Msg("Failed to get client version")
	} else {
		log.Info().Interface("client_version", clientVersion).Msg("Client version")
	}
}

func testSendTransaction(privateKey *ecdsa.PrivateKey, fromAddress common.Address) {
	log.Info().Msg("💰 Testing transaction sending...")

	// Send to the same address
	toAddress := fromAddress

	// Get current nonce
	nonceResult, err := makeRPCCall("eth_getTransactionCount", []interface{}{fromAddress.Hex(), "latest"})
	if err != nil {
		log.Error().Err(err).Msg("Failed to get nonce")
		return
	}

	// Convert nonce from hex string to uint64
	var nonce uint64
	if nonceStr, ok := nonceResult.(string); ok {
		if parsed, parseErr := parseHexToUint64(nonceStr); parseErr == nil {
			nonce = parsed
		}
	}

	log.Info().Uint64("nonce", nonce).Msg("Using nonce for transaction")

	// Get current gas price from the network
	gasPriceResult, err := makeRPCCall("eth_gasPrice", []interface{}{})
	if err != nil {
		log.Error().Err(err).Msg("Failed to get gas price from network")
		return
	}

	var gasPrice *big.Int
	if gasPriceStr, ok := gasPriceResult.(string); ok {
		gasPrice = parseHexToBigInt(gasPriceStr)
	} else {
		log.Error().Msg("Invalid gas price response")
		return
	}

	// Gas settings
	gasLimit := uint64(21000) // Gas limit for simple transfer

	// Calculate total gas cost (gas limit * gas price)
	gasCost := new(big.Int).Mul(big.NewInt(int64(gasLimit)), gasPrice)

	// Transaction value: 0.0001 ETH in wei
	value := big.NewInt(100000000000000) // 0.0001 ETH = 100000000000000 wei

	log.Info().
		Uint64("gas_limit", gasLimit).
		Str("gas_price", gasPrice.String()).
		Str("total_gas_cost", gasCost.String()).
		Str("transaction_value", value.String()+" wei (0.0001 ETH)").
		Msg("Transaction details")

	// Create transaction - sending 0.0001 ETH to the same address
	tx := types.NewTransaction(
		nonce,
		toAddress,
		value,    // Send 0.0001 ETH
		gasLimit, // Gas limit
		gasPrice, // Dynamic gas price from network
		nil,      // No data
	)

	// Sign the transaction
	signer := types.NewEIP155Signer(big.NewInt(chainID))
	signedTx, err := types.SignTx(tx, signer, privateKey)
	if err != nil {
		log.Error().Err(err).Msg("Failed to sign transaction")
		return
	}

	// Encode the transaction
	txData, err := signedTx.MarshalBinary()
	if err != nil {
		log.Error().Err(err).Msg("Failed to encode transaction")
		return
	}

	// Convert to hex string
	rawTx := fmt.Sprintf("0x%x", txData)

	log.Info().
		Str("from", fromAddress.Hex()).
		Str("to", toAddress.Hex()).
		Str("value", "0.0001 ETH").
		Str("raw_tx_preview", rawTx[:42]+"...").
		Msg("Sending transaction via eth_sendRawTransaction")

	// Send the transaction
	txHash, err := makeRPCCall("eth_sendRawTransaction", []interface{}{rawTx})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send transaction")
		return
	}

	log.Info().
		Interface("tx_hash", txHash).
		Msg("✅ Transaction submitted successfully - this will be encrypted and stored locally")

	// Wait a bit for processing
	time.Sleep(2 * time.Second)
}

func testPostTransactionQueries(address common.Address) {
	log.Info().Msg("🔍 Testing post-transaction queries...")

	// Test eth_getBalance
	balance, err := makeRPCCall("eth_getBalance", []interface{}{address.Hex(), "latest"})
	if err != nil {
		log.Error().Err(err).Msg("Failed to get balance")
	} else {
		log.Info().
			Str("address", address.Hex()).
			Interface("balance", balance).
			Msg("Account balance")
	}

	// Test eth_getTransactionCount again
	txCount, err := makeRPCCall("eth_getTransactionCount", []interface{}{address.Hex(), "latest"})
	if err != nil {
		log.Error().Err(err).Msg("Failed to get transaction count")
	} else {
		log.Info().
			Str("address", address.Hex()).
			Interface("tx_count", txCount).
			Msg("Transaction count (nonce)")
	}

	// Test eth_getCode (should be empty for EOA)
	code, err := makeRPCCall("eth_getCode", []interface{}{address.Hex(), "latest"})
	if err != nil {
		log.Error().Err(err).Msg("Failed to get code")
	} else {
		log.Info().
			Str("address", address.Hex()).
			Interface("code", code).
			Msg("Account code")
	}

	// Test getting latest block
	latestBlock, err := makeRPCCall("eth_getBlockByNumber", []interface{}{"latest", false})
	if err != nil {
		log.Error().Err(err).Msg("Failed to get latest block")
	} else {
		log.Info().Interface("block", latestBlock).Msg("Latest block info")
	}
}

func makeRPCCall(method string, params []interface{}) (interface{}, error) {
	requestID := time.Now().UnixNano()

	request := JSONRPCRequest{
		ID:      requestID,
		Method:  method,
		Params:  params,
		JSONRPC: "2.0",
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	log.Debug().
		Str("method", method).
		Interface("params", params).
		Int64("id", requestID).
		Msg("Making RPC call")

	resp, err := http.Post(rpcURL, "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var rpcResponse JSONRPCResponse
	if err := json.Unmarshal(responseBody, &rpcResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if rpcResponse.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResponse.Error.Code, rpcResponse.Error.Message)
	}

	log.Debug().
		Str("method", method).
		Int64("id", requestID).
		Interface("result", rpcResponse.Result).
		Msg("RPC call successful")

	return rpcResponse.Result, nil
}

func parseHexToUint64(hexStr string) (uint64, error) {
	if len(hexStr) >= 2 && hexStr[:2] == "0x" {
		hexStr = hexStr[2:]
	}

	if hexStr == "" {
		return 0, nil
	}

	var result uint64
	for _, char := range hexStr {
		result *= 16
		if char >= '0' && char <= '9' {
			result += uint64(char - '0')
		} else if char >= 'a' && char <= 'f' {
			result += uint64(char - 'a' + 10)
		} else if char >= 'A' && char <= 'F' {
			result += uint64(char - 'A' + 10)
		} else {
			return 0, fmt.Errorf("invalid hex character: %c", char)
		}
	}

	return result, nil
}

func parseHexToBigInt(hexStr string) *big.Int {
	if len(hexStr) >= 2 && hexStr[:2] == "0x" {
		hexStr = hexStr[2:]
	}

	result := new(big.Int)
	result.SetString(hexStr, 16)
	return result
}
