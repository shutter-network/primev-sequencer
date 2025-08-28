package rpc

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"primev-poc/shutter"
	"primev-poc/txhandler"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/medley/service"

	"github.com/rs/zerolog/log"
)

type Config struct {
	Port                    string
	UpstreamRPCURL          string
	KeyperSetManagerAddress string
	KeyBroadcastAddress     string
}

type RPCServer struct {
	config     *Config
	txHandler  *txhandler.TransactionHandler
	httpClient *http.Client
}

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

func NewRPCServer(config *Config, txHandler *txhandler.TransactionHandler) (*RPCServer, error) {
	shutterConfig := &shutter.Config{
		EthereumRPCURL:          config.UpstreamRPCURL,
		KeyperSetManagerAddress: config.KeyperSetManagerAddress,
		KeyBroadcastAddress:     config.KeyBroadcastAddress,
		InclusionWindow:         10,
	}

	// Initialize the shutter package with configuration
	shutter.Initialize(shutterConfig)

	return &RPCServer{
		config:     config,
		txHandler:  txHandler,
		httpClient: &http.Client{},
	}, nil
}

func (s *RPCServer) Start(ctx context.Context, runner service.Runner) error {
	httpServer := &http.Server{
		Addr:    ":" + s.config.Port,
		Handler: s,
	}
	runner.Go(httpServer.ListenAndServe)
	runner.Go(func() error {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	})
	return nil
}

func (s *RPCServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read request body")
		s.writeError(w, nil, -32700, "Parse error", nil)
		return
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Error().Err(err).Msg("Failed to parse JSON-RPC request")
		s.writeError(w, nil, -32700, "Parse error", nil)
		return
	}

	log.Debug().
		Str("method", req.Method).
		Interface("id", req.ID).
		Msg("Received RPC request")

	s.handleRequest(w, &req)
}

func (s *RPCServer) handleRequest(w http.ResponseWriter, req *JSONRPCRequest) {
	switch req.Method {
	case "eth_sendRawTransaction":
		log.Info().
			Str("method", req.Method).
			Interface("id", req.ID).
			Msg("Handling request locally - encrypted transaction processing")
		s.handleSendRawTransaction(w, req)
	case "eth_sendTransaction":
		log.Info().
			Str("method", req.Method).
			Interface("id", req.ID).
			Msg("Handling request locally - transaction method not implemented")
		s.handleSendTransaction(w, req)
	default:
		s.proxyRequest(w, req)
	}
}

func (s *RPCServer) handleSendRawTransaction(w http.ResponseWriter, req *JSONRPCRequest) {
	params, ok := req.Params.([]interface{})
	if !ok || len(params) != 1 {
		log.Warn().
			Str("method", req.Method).
			Interface("id", req.ID).
			Msg("Invalid params for encrypted transaction")
		s.writeError(w, req.ID, -32602, "Invalid params", nil)
		return
	}

	rawTx, ok := params[0].(string)
	if !ok {
		log.Warn().
			Str("method", req.Method).
			Interface("id", req.ID).
			Msg("Invalid transaction data format")
		s.writeError(w, req.ID, -32602, "Invalid transaction data", nil)
		return
	}

	txData, err := hex.DecodeString(rawTx[2:])
	if err != nil {
		log.Warn().
			Str("method", req.Method).
			Interface("id", req.ID).
			Msg("Invalid transaction data format")
		s.writeError(w, req.ID, -32602, "Invalid transaction data", nil)
		return
	}

	txHash := crypto.Keccak256Hash(txData)

	log.Info().
		Str("method", req.Method).
		Interface("id", req.ID).
		Str("tx_hash", txHash.Hex()).
		Msg("Processing encrypted transaction locally")

	tx, err := s.txHandler.GetTransaction(txHash)
	if err == nil && tx != nil {
		log.Debug().
			Str("method", req.Method).
			Interface("id", req.ID).
			Str("tx_hash", txHash.Hex()).
			Msg("Transaction already exists locally")
		s.writeSuccess(w, req.ID, txHash.Hex())
		return
	}

	encryptedTx, _, err := shutter.EncryptTransaction(rawTx, txHash)
	if err != nil {
		log.Error().
			Err(err).
			Str("method", req.Method).
			Interface("id", req.ID).
			Msg("Failed to encrypt transaction locally")
		s.writeError(w, req.ID, -32000, "Encryption failed", err.Error())
		return
	}

	encryptedTx.EncryptedTx = txData

	err = s.txHandler.StoreTransaction(txHash, encryptedTx)
	if err != nil {
		log.Error().
			Err(err).
			Str("method", req.Method).
			Interface("id", req.ID).
			Msg("Failed to store transaction locally")
		s.writeError(w, req.ID, -32000, "Submission failed", err.Error())
		return
	}

	log.Info().
		Str("method", req.Method).
		Interface("id", req.ID).
		Str("tx_hash", txHash.Hex()).
		Uint64("scheduled_block", encryptedTx.ScheduledBlock).
		Msg("Successfully processed encrypted transaction with identity locally")

	s.writeSuccess(w, req.ID, txHash.Hex())
}

func (s *RPCServer) handleSendTransaction(w http.ResponseWriter, req *JSONRPCRequest) {
	log.Info().
		Str("method", req.Method).
		Interface("id", req.ID).
		Msg("Method not implemented locally - use eth_sendRawTransaction instead")
	s.writeError(w, req.ID, -32601, "Method not implemented", "Use eth_sendRawTransaction instead")
}

func (s *RPCServer) proxyRequest(w http.ResponseWriter, req *JSONRPCRequest) {
	requestBody, err := json.Marshal(req)
	if err != nil {
		s.writeError(w, req.ID, -32603, "Internal error", nil)
		return
	}

	upstreamReq, err := http.NewRequest("POST", s.config.UpstreamRPCURL, bytes.NewBuffer(requestBody))
	if err != nil {
		s.writeError(w, req.ID, -32603, "Internal error", nil)
		return
	}

	upstreamReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		log.Error().Err(err).Msg("Failed to proxy request to upstream")
		s.writeError(w, req.ID, -32603, "Internal error", nil)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		s.writeError(w, req.ID, -32603, "Internal error", nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

func (s *RPCServer) writeSuccess(w http.ResponseWriter, id interface{}, result interface{}) {
	response := JSONRPCResponse{
		ID:      id,
		Result:  result,
		JSONRPC: "2.0",
	}

	responseBody, _ := json.Marshal(response)
	w.WriteHeader(http.StatusOK)
	w.Write(responseBody)
}

func (s *RPCServer) writeError(w http.ResponseWriter, id interface{}, code int, message string, data interface{}) {
	response := JSONRPCResponse{
		ID:      id,
		Error:   &RPCError{Code: code, Message: message, Data: data},
		JSONRPC: "2.0",
	}

	responseBody, _ := json.Marshal(response)
	w.WriteHeader(http.StatusOK)
	w.Write(responseBody)
}
