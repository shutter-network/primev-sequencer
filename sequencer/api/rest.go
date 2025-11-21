package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"primev-poc/txstore"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog/log"
	"github.com/shutter-network/rolling-shutter/rolling-shutter/medley/service"
	"github.com/shutter-network/shutter/shlib/shcrypto"
)

// RestAPI represents the REST API server
type RestAPI struct {
	txStore *txstore.TransactionStore
	apiPort string
}

// NewRestAPI creates a new REST API instance
func NewRestAPI(txStore *txstore.TransactionStore, apiPort string) *RestAPI {
	return &RestAPI{
		txStore: txStore,
		apiPort: apiPort,
	}
}

// GetDecryptedTxResponse represents the response for get_decrypted_tx endpoint
type GetDecryptedTxResponse struct {
	Success bool                      `json:"success"`
	Data    *DecryptedTransactionData `json:"data,omitempty"`
	Error   string                    `json:"error,omitempty"`
}

// DecryptedTransactionData represents the decrypted transaction data
type DecryptedTransactionData struct {
	TxHash        string `json:"tx_hash"`
	Identity      string `json:"identity"`
	DecryptionKey string `json:"decryption_key"`
	DecryptedTx   string `json:"decrypted_tx"` // hex encoded
}

// GetDecryptedTx handles the get_decrypted_tx endpoint
func (api *RestAPI) GetDecryptedTx(w http.ResponseWriter, r *http.Request) {
	// Set content type
	w.Header().Set("Content-Type", "application/json")

	// Only allow GET requests
	if r.Method != http.MethodGet {
		api.sendErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get txHash from URL path
	// Expected path: /get_decrypted_tx/{txHash}
	path := strings.TrimPrefix(r.URL.Path, "/decrypted_tx/")
	if path == "" || path == r.URL.Path {
		api.sendErrorResponse(w, "txHash is required in path: "+path, http.StatusBadRequest)
		return
	}

	txHashStr := path

	// Validate and convert txHash to common.Hash
	txHash := common.HexToHash(txHashStr)
	if txHash == (common.Hash{}) {
		api.sendErrorResponse(w, "Invalid txHash format", http.StatusBadRequest)
		return
	}

	// Get transaction from handler
	transaction, err := api.txStore.GetTransaction(txHash)
	if err != nil {
		log.Error().Err(err).Str("tx_hash", txHashStr).Msg("Failed to get transaction")
		api.sendErrorResponse(w, "Transaction not found: "+err.Error(), http.StatusNotFound)
		return
	}

	// Check if transaction is decrypted
	if len(transaction.EncryptedTx.DecryptionKey) == 0 {
		log.Error().Str("tx_hash", txHashStr).Msg("Transaction is not decrypted")
		responseData := &DecryptedTransactionData{
			TxHash:        txHash.Hex(),
			Identity:      transaction.EncryptedTx.Identity,
			DecryptionKey: "",
			DecryptedTx:   "",
		}

		response := GetDecryptedTxResponse{
			Success: false,
			Data:    responseData,
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}

	decryptionKey := new(shcrypto.EpochSecretKey)
	err = decryptionKey.Unmarshal(transaction.EncryptedTx.DecryptionKey)
	if err != nil {
		log.Error().Err(err).Str("tx_hash", txHashStr).Msg("Failed to unmarshal decryption key")
		responseData := &DecryptedTransactionData{
			TxHash:        txHash.Hex(),
			Identity:      transaction.EncryptedTx.Identity,
			DecryptionKey: "",
			DecryptedTx:   "",
		}

		response := GetDecryptedTxResponse{
			Success: false,
			Data:    responseData,
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}

	encryptedMsg := new(shcrypto.EncryptedMessage)
	err = encryptedMsg.Unmarshal(transaction.EncryptedTx.EncryptedTx)
	if err != nil {
		log.Error().Err(err).Str("tx_hash", txHashStr).Msg("Failed to unmarshal encrypted message")
		responseData := &DecryptedTransactionData{
			TxHash:        txHash.Hex(),
			Identity:      transaction.EncryptedTx.Identity,
			DecryptionKey: "",
			DecryptedTx:   "",
		}
		response := GetDecryptedTxResponse{
			Success: false,
			Data:    responseData,
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}

	decryptedMsg, err := encryptedMsg.Decrypt(decryptionKey)
	if err != nil {
		log.Error().Err(err).Str("tx_hash", txHashStr).Msg("Failed to decrypt transaction")
		responseData := &DecryptedTransactionData{
			TxHash:        txHash.Hex(),
			Identity:      transaction.EncryptedTx.Identity,
			DecryptionKey: "",
			DecryptedTx:   "",
		}
		response := GetDecryptedTxResponse{
			Success: false,
			Data:    responseData,
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Prepare response data
	responseData := &DecryptedTransactionData{
		TxHash:        txHash.Hex(),
		Identity:      transaction.EncryptedTx.Identity,
		DecryptionKey: hex.EncodeToString(transaction.EncryptedTx.DecryptionKey),
		DecryptedTx:   hex.EncodeToString(decryptedMsg), // This would contain the decrypted transaction data
	}

	// Send success response
	response := GetDecryptedTxResponse{
		Success: true,
		Data:    responseData,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// sendErrorResponse sends an error response
func (api *RestAPI) sendErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	response := GetDecryptedTxResponse{
		Success: false,
		Error:   message,
	}

	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

// Start starts the REST API server
func (api *RestAPI) Start(ctx context.Context, runner service.Runner) error {
	mux := http.NewServeMux()

	// Register the decrypted_tx endpoint with path parameter
	mux.HandleFunc("/decrypted_tx/", api.GetDecryptedTx)

	httpServer := &http.Server{
		Addr:    ":" + api.apiPort,
		Handler: mux,
	}

	runner.Go(httpServer.ListenAndServe)

	runner.Go(func() error {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	})
	return nil
}
