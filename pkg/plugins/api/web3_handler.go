package api

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
	"streamgate/pkg/service"
)

// Web3Handler handles Web3 API requests
type Web3Handler struct {
	web3Service *service.Web3Service
	logger      *zap.Logger
}

// NewWeb3Handler creates a new Web3 handler
func NewWeb3Handler(web3Service *service.Web3Service, logger *zap.Logger) *Web3Handler {
	return &Web3Handler{
		web3Service: web3Service,
		logger:      logger,
	}
}

// VerifySignatureRequest is the request for signature verification
type VerifySignatureRequest struct {
	Address   string `json:"address"`
	Message   string `json:"message"`
	Signature string `json:"signature"`
}

// VerifySignatureResponse is the response for signature verification
type VerifySignatureResponse struct {
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

// HandleVerifySignature handles signature verification requests
func (wh *Web3Handler) HandleVerifySignature(w http.ResponseWriter, r *http.Request) {
	wh.logger.Debug("Handling verify signature request")

	var req VerifySignatureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		wh.logger.Error("Failed to decode request", zap.Error(err))
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Verify signature
	valid, err := wh.web3Service.VerifySignature(r.Context(), req.Address, req.Message, req.Signature)
	if err != nil {
		wh.logger.Error("Failed to verify signature", zap.Error(err))
		http.Error(w, "Failed to verify signature", http.StatusInternalServerError)
		return
	}

	// Return response
	resp := VerifySignatureResponse{
		Valid: valid,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// VerifyNFTRequest is the request for NFT verification
type VerifyNFTRequest struct {
	ChainID         int64  `json:"chain_id"`
	ContractAddress string `json:"contract_address"`
	TokenID         string `json:"token_id"`
	OwnerAddress    string `json:"owner_address"`
}

// VerifyNFTResponse is the response for NFT verification
type VerifyNFTResponse struct {
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

// HandleVerifyNFT handles NFT verification requests
func (wh *Web3Handler) HandleVerifyNFT(w http.ResponseWriter, r *http.Request) {
	wh.logger.Debug("Handling verify NFT request")

	var req VerifyNFTRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		wh.logger.Error("Failed to decode request", zap.Error(err))
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Verify NFT ownership
	valid, err := wh.web3Service.VerifyNFTOwnership(r.Context(), req.ChainID, req.ContractAddress, req.TokenID, req.OwnerAddress)
	if err != nil {
		wh.logger.Error("Failed to verify NFT", zap.Error(err))
		http.Error(w, "Failed to verify NFT", http.StatusInternalServerError)
		return
	}

	// Return response
	resp := VerifyNFTResponse{
		Valid: valid,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GetGasPriceResponse is the response for gas price query
type GetGasPriceResponse struct {
	ChainID      int64   `json:"chain_id"`
	GasPrice     string  `json:"gas_price"`
	GasPriceGwei float64 `json:"gas_price_gwei"`
	Error        string  `json:"error,omitempty"`
}

// HandleGetGasPrice handles gas price requests
func (wh *Web3Handler) HandleGetGasPrice(w http.ResponseWriter, r *http.Request) {
	wh.logger.Debug("Handling get gas price request")

	chainID := int64(80001) // Default to Polygon Mumbai
	if chainIDStr := r.URL.Query().Get("chain_id"); chainIDStr != "" {
		// Parse chain ID
		// TODO: Parse chain ID from query parameter
	}

	// Get gas price
	gasPrice, err := wh.web3Service.GetGasPrice(r.Context(), chainID)
	if err != nil {
		wh.logger.Error("Failed to get gas price", zap.Error(err))
		http.Error(w, "Failed to get gas price", http.StatusInternalServerError)
		return
	}

	// Return response
	resp := GetGasPriceResponse{
		ChainID:  chainID,
		GasPrice: gasPrice,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GetSupportedChainsResponse is the response for supported chains query
type GetSupportedChainsResponse struct {
	Chains []ChainInfo `json:"chains"`
}

// ChainInfo contains information about a supported chain
type ChainInfo struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	RPC       string `json:"rpc"`
	Explorer  string `json:"explorer"`
	Currency  string `json:"currency"`
	IsTestnet bool   `json:"is_testnet"`
}

// HandleGetSupportedChains handles supported chains requests
func (wh *Web3Handler) HandleGetSupportedChains(w http.ResponseWriter, r *http.Request) {
	wh.logger.Debug("Handling get supported chains request")

	// Get supported chains
	chains := wh.web3Service.GetSupportedChains()

	// Convert to response format
	chainInfos := make([]ChainInfo, len(chains))
	for i, chain := range chains {
		chainInfos[i] = ChainInfo{
			ID:        chain.ID,
			Name:      chain.Name,
			RPC:       chain.RPC,
			Explorer:  chain.Explorer,
			Currency:  chain.Currency,
			IsTestnet: chain.IsTestnet,
		}
	}

	// Return response
	resp := GetSupportedChainsResponse{
		Chains: chainInfos,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// UploadToIPFSRequest is the request for IPFS upload
type UploadToIPFSRequest struct {
	Filename string `json:"filename"`
	Data     string `json:"data"` // Base64 encoded
}

// UploadToIPFSResponse is the response for IPFS upload
type UploadToIPFSResponse struct {
	CID   string `json:"cid"`
	URL   string `json:"url"`
	Error string `json:"error,omitempty"`
}

// HandleUploadToIPFS handles IPFS upload requests
func (wh *Web3Handler) HandleUploadToIPFS(w http.ResponseWriter, r *http.Request) {
	wh.logger.Debug("Handling upload to IPFS request")

	var req UploadToIPFSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		wh.logger.Error("Failed to decode request", zap.Error(err))
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// TODO: Decode base64 data
	// data, err := base64.StdEncoding.DecodeString(req.Data)

	// Upload to IPFS
	cid, err := wh.web3Service.UploadToIPFS(r.Context(), req.Filename, []byte(req.Data))
	if err != nil {
		wh.logger.Error("Failed to upload to IPFS", zap.Error(err))
		http.Error(w, "Failed to upload to IPFS", http.StatusInternalServerError)
		return
	}

	// Return response
	resp := UploadToIPFSResponse{
		CID: cid,
		URL: "https://ipfs.io/ipfs/" + cid,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// DownloadFromIPFSRequest is the request for IPFS download
type DownloadFromIPFSRequest struct {
	CID string `json:"cid"`
}

// DownloadFromIPFSResponse is the response for IPFS download
type DownloadFromIPFSResponse struct {
	Data  string `json:"data"` // Base64 encoded
	Error string `json:"error,omitempty"`
}

// HandleDownloadFromIPFS handles IPFS download requests
func (wh *Web3Handler) HandleDownloadFromIPFS(w http.ResponseWriter, r *http.Request) {
	wh.logger.Debug("Handling download from IPFS request")

	var req DownloadFromIPFSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		wh.logger.Error("Failed to decode request", zap.Error(err))
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Download from IPFS
	data, err := wh.web3Service.DownloadFromIPFS(r.Context(), req.CID)
	if err != nil {
		wh.logger.Error("Failed to download from IPFS", zap.Error(err))
		http.Error(w, "Failed to download from IPFS", http.StatusInternalServerError)
		return
	}

	// Return response
	resp := DownloadFromIPFSResponse{
		Data: string(data), // TODO: Base64 encode
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// RegisterWeb3Routes registers Web3 API routes
func RegisterWeb3Routes(mux *http.ServeMux, web3Handler *Web3Handler) {
	mux.HandleFunc("/api/v1/web3/verify-signature", web3Handler.HandleVerifySignature)
	mux.HandleFunc("/api/v1/web3/verify-nft", web3Handler.HandleVerifyNFT)
	mux.HandleFunc("/api/v1/web3/gas-price", web3Handler.HandleGetGasPrice)
	mux.HandleFunc("/api/v1/web3/supported-chains", web3Handler.HandleGetSupportedChains)
	mux.HandleFunc("/api/v1/web3/ipfs/upload", web3Handler.HandleUploadToIPFS)
	mux.HandleFunc("/api/v1/web3/ipfs/download", web3Handler.HandleDownloadFromIPFS)
}
