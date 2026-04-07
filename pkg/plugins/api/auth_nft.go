package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type challengeRequest struct {
	Address string `json:"address"`
	Wallet  string `json:"wallet"`
	ChainID int64  `json:"chain_id"`
}

type challengeResponse struct {
	ChallengeID string `json:"challenge_id"`
	Message     string `json:"message"`
	ExpiresAt   string `json:"expires_at"`
	Wallet      string `json:"wallet"`
	ChainID     int64  `json:"chain_id"`
}

type walletLoginRequest struct {
	Address     string `json:"address"`
	Wallet      string `json:"wallet"`
	ChallengeID string `json:"challenge_id"`
	Signature   string `json:"signature"`
}

type walletLoginResponse struct {
	Token         string `json:"token"`
	WalletAddress string `json:"wallet_address"`
}

type nftVerifyRequest struct {
	ChainID         int64  `json:"chain_id"`
	Address         string `json:"address"`
	Wallet          string `json:"wallet"`
	OwnerAddress    string `json:"owner_address"`
	Contract        string `json:"contract"`
	ContractAddress string `json:"contract_address"`
	TokenID         string `json:"token_id"`
}

type nftVerifyResponse struct {
	HasNFT   bool   `json:"has_nft"`
	Balance  int64  `json:"balance"`
	ChainID  int64  `json:"chain_id"`
	Contract string `json:"contract"`
	CacheHit bool   `json:"cache_hit"`
}

func nftAccessCacheKey(chainID int64, wallet, contract, tokenID string) string {
	return strings.Join([]string{
		strconv.FormatInt(chainID, 10),
		strings.ToLower(wallet),
		strings.ToLower(contract),
		tokenID,
	}, ":")
}

func normalizeWallet(addresses ...string) string {
	for _, address := range addresses {
		if strings.TrimSpace(address) != "" {
			return strings.TrimSpace(address)
		}
	}
	return ""
}

// AuthChallengeHandler creates a one-time wallet challenge.
func (h *Handler) AuthChallengeHandler(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("auth_challenge_invalid_method_total", nil)
		h.recordRequest("auth", startedAt, false)
		h.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !h.ensureServices(w) {
		h.metricsCollector.IncrementCounter("auth_challenge_failed_total", nil)
		h.recordRequest("auth", startedAt, false)
		return
	}

	var req challengeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.metricsCollector.IncrementCounter("auth_challenge_decode_error_total", nil)
		h.recordRequest("auth", startedAt, false)
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	wallet := normalizeWallet(req.Wallet, req.Address)
	if wallet == "" {
		h.metricsCollector.IncrementCounter("auth_challenge_invalid_total", nil)
		h.recordRequest("auth", startedAt, false)
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "wallet address is required"})
		return
	}

	chainID := req.ChainID
	if chainID == 0 {
		chainID = h.defaultChainID()
	}

	challenge, err := h.authService.GenerateWalletChallenge(wallet, chainID)
	if err != nil {
		h.metricsCollector.IncrementCounter("auth_challenge_failed_total", nil)
		h.recordRequest("auth", startedAt, false)
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	h.metricsCollector.IncrementCounter("auth_challenge_success_total", map[string]string{"chain": strconv.FormatInt(chainID, 10)})
	h.recordRequest("auth", startedAt, true)

	h.writeJSON(w, http.StatusOK, challengeResponse{
		ChallengeID: challenge.ID,
		Message:     challenge.Message,
		ExpiresAt:   challenge.ExpiresAt.Format(time.RFC3339),
		Wallet:      challenge.WalletAddress,
		ChainID:     challenge.ChainID,
	})
}

// AuthLoginHandler authenticates a wallet using a challenge and signature.
func (h *Handler) AuthLoginHandler(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("auth_login_invalid_method_total", nil)
		h.recordRequest("auth", startedAt, false)
		h.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !h.ensureServices(w) {
		h.metricsCollector.IncrementCounter("auth_login_failed_total", nil)
		h.recordRequest("auth", startedAt, false)
		return
	}

	var req walletLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.metricsCollector.IncrementCounter("auth_login_decode_error_total", nil)
		h.recordRequest("auth", startedAt, false)
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	wallet := normalizeWallet(req.Wallet, req.Address)
	token, err := h.authService.AuthenticateWithWallet(wallet, req.ChallengeID, req.Signature)
	if err != nil {
		status := http.StatusUnauthorized
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "invalid wallet address") {
			status = http.StatusBadRequest
		}
		h.metricsCollector.IncrementCounter("auth_login_failed_total", nil)
		h.recordRequest("auth", startedAt, false)
		h.writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	h.metricsCollector.IncrementCounter("auth_login_success_total", nil)
	h.recordRequest("auth", startedAt, true)

	h.writeJSON(w, http.StatusOK, walletLoginResponse{
		Token:         token,
		WalletAddress: wallet,
	})
}

// VerifyNFTHandler verifies NFT access for a wallet.
func (h *Handler) VerifyNFTHandler(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	if r.Method != http.MethodPost {
		h.metricsCollector.IncrementCounter("nft_verify_invalid_method_total", nil)
		h.recordRequest("nft", startedAt, false)
		h.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !h.ensureServices(w) {
		h.metricsCollector.IncrementCounter("nft_verify_failed_total", nil)
		h.recordRequest("nft", startedAt, false)
		return
	}

	var req nftVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.metricsCollector.IncrementCounter("nft_verify_decode_error_total", nil)
		h.recordRequest("nft", startedAt, false)
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	wallet := normalizeWallet(req.Wallet, req.Address, req.OwnerAddress)
	contract := normalizeWallet(req.Contract, req.ContractAddress)
	if wallet == "" || contract == "" {
		h.metricsCollector.IncrementCounter("nft_verify_invalid_total", nil)
		h.recordRequest("nft", startedAt, false)
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "wallet and contract are required"})
		return
	}

	chainID := req.ChainID
	if chainID == 0 {
		chainID = h.defaultChainID()
	}

	var (
		hasNFT   bool
		balance  int64
		err      error
		cacheHit bool
	)

	cacheKey := nftAccessCacheKey(chainID, wallet, contract, req.TokenID)
	if cached, ok := h.getCachedNFTAccess(cacheKey); ok {
		hasNFT = cached.HasNFT
		balance = cached.Balance
		cacheHit = true
	} else if strings.TrimSpace(req.TokenID) != "" {
		hasNFT, err = h.web3Service.VerifyNFTOwnership(r.Context(), chainID, contract, req.TokenID, wallet)
		if hasNFT {
			balance = 1
		}
	} else {
		balance, err = h.web3Service.GetNFTBalance(r.Context(), chainID, contract, wallet)
		hasNFT = balance > 0
	}

	if err != nil {
		h.metricsCollector.IncrementCounter("nft_verify_failed_total", map[string]string{"chain": strconv.FormatInt(chainID, 10)})
		h.recordRequest("nft", startedAt, false)
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	statusTag := "miss"
	if hasNFT {
		statusTag = "hit"
	}
	if !cacheHit {
		h.setCachedNFTAccess(cacheKey, cachedNFTAccess{
			HasNFT:    hasNFT,
			Balance:   balance,
			ExpiresAt: time.Now().Add(60 * time.Second),
		})
	}
	h.metricsCollector.IncrementCounter("nft_verification_total", map[string]string{
		"chain":  strconv.FormatInt(chainID, 10),
		"status": statusTag,
	})
	h.recordRequest("nft", startedAt, true)

	h.writeJSON(w, http.StatusOK, nftVerifyResponse{
		HasNFT:   hasNFT,
		Balance:  balance,
		ChainID:  chainID,
		Contract: contract,
		CacheHit: cacheHit,
	})
}
