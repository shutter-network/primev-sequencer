package api

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"primev-poc/txhandler"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog/log"
	"github.com/shutter-network/shutter/shlib/shcrypto"
)

// RestAPI represents the REST API server
type RestAPI struct {
	txHandler *txhandler.TransactionHandler
}

// NewRestAPI creates a new REST API instance
func NewRestAPI(txHandler *txhandler.TransactionHandler) *RestAPI {
	return &RestAPI{
		txHandler: txHandler,
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
	path := strings.TrimPrefix(r.URL.Path, "/get_decrypted_tx/")
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
	transaction, err := api.txHandler.GetTransaction(txHash)
	if err != nil {
		log.Error().Err(err).Str("tx_hash", txHashStr).Msg("Failed to get transaction")
		api.sendErrorResponse(w, "Transaction not found: "+err.Error(), http.StatusNotFound)
		return
	}

	// Check if transaction is decrypted
	if len(transaction.EncryptedTx.DecryptionKey) == 0 {
		log.Error().Str("tx_hash", txHashStr).Msg("Transaction is not decrypted")
		api.sendErrorResponse(w, "Transaction is not decrypted", http.StatusBadRequest)
		return
	}

	decryptionKey := new(shcrypto.EpochSecretKey)
	err = decryptionKey.Unmarshal(transaction.EncryptedTx.DecryptionKey)
	if err != nil {
		log.Error().Err(err).Str("tx_hash", txHashStr).Msg("Failed to unmarshal decryption key")
		api.sendErrorResponse(w, "Failed to unmarshal decryption key", http.StatusInternalServerError)
		return
	}
	encryptedMsg := new(shcrypto.EncryptedMessage)
	err = encryptedMsg.Unmarshal(transaction.EncryptedTx.EncryptedTx)
	if err != nil {
		log.Error().Err(err).Str("tx_hash", txHashStr).Msg("Failed to unmarshal encrypted message")
		api.sendErrorResponse(w, "Failed to unmarshal encrypted message", http.StatusInternalServerError)
		return
	}

	decryptedMsg, err := encryptedMsg.Decrypt(decryptionKey)
	if err != nil {
		log.Error().Err(err).Str("tx_hash", txHashStr).Msg("Failed to decrypt transaction")
		api.sendErrorResponse(w, "Failed to decrypt transaction", http.StatusInternalServerError)
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

// SetupRoutes sets up the REST API routes
func (api *RestAPI) SetupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Register the get_decrypted_tx endpoint with path parameter
	mux.HandleFunc("/get_decrypted_tx/", api.GetDecryptedTx)

	return mux
}
