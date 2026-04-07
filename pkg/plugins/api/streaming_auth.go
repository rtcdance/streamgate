package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

// ProtectedManifestHandler gates manifest access behind JWT + NFT verification.
func (h *Handler) StreamingAccessHandler(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/manifest.m3u8"):
		h.ProtectedManifestHandler(w, r)
	case strings.Contains(r.URL.Path, "/segment/"):
		h.ProtectedSegmentHandler(w, r)
	default:
		h.NotFoundHandler(w, r)
	}
}

func (h *Handler) ProtectedManifestHandler(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("streaming_manifest_invalid_method_total", nil)
		h.recordRequest("streaming", startedAt, false)
		h.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !h.ensureServices(w) {
		h.metricsCollector.IncrementCounter("streaming_manifest_failed_total", nil)
		h.recordRequest("streaming", startedAt, false)
		return
	}

	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		h.metricsCollector.IncrementCounter("streaming_manifest_unauthorized_total", nil)
		h.recordRequest("streaming", startedAt, false)
		h.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
		return
	}

	claims, err := h.authService.ParseToken(token)
	if err != nil {
		h.metricsCollector.IncrementCounter("streaming_manifest_unauthorized_total", nil)
		h.recordRequest("streaming", startedAt, false)
		h.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
		return
	}

	wallet := claims.WalletAddress
	if wallet == "" {
		h.metricsCollector.IncrementCounter("streaming_manifest_unauthorized_total", nil)
		h.recordRequest("streaming", startedAt, false)
		h.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "token missing wallet address"})
		return
	}

	contract := strings.TrimSpace(r.URL.Query().Get("contract"))
	if contract == "" {
		contract = strings.TrimSpace(r.URL.Query().Get("contract_address"))
	}
	if contract == "" {
		h.metricsCollector.IncrementCounter("streaming_manifest_invalid_total", nil)
		h.recordRequest("streaming", startedAt, false)
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "contract is required"})
		return
	}

	tokenID := strings.TrimSpace(r.URL.Query().Get("token_id"))
	chainID := h.defaultChainID()
	if raw := strings.TrimSpace(r.URL.Query().Get("chain_id")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			chainID = parsed
		}
	}

	var hasNFT bool
	cacheKey := nftAccessCacheKey(chainID, wallet, contract, tokenID)
	if cached, ok := h.getCachedNFTAccess(cacheKey); ok {
		hasNFT = cached.HasNFT
	} else if tokenID != "" {
		hasNFT, err = h.web3Service.VerifyNFTOwnership(r.Context(), chainID, contract, tokenID, wallet)
	} else {
		balance, balanceErr := h.web3Service.GetNFTBalance(r.Context(), chainID, contract, wallet)
		err = balanceErr
		hasNFT = balance > 0
	}
	if err != nil {
		h.metricsCollector.IncrementCounter("streaming_manifest_failed_total", map[string]string{"chain": strconv.FormatInt(chainID, 10)})
		h.recordRequest("streaming", startedAt, false)
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !hasNFT {
		h.metricsCollector.IncrementCounter("streaming_manifest_forbidden_total", map[string]string{"chain": strconv.FormatInt(chainID, 10)})
		h.recordRequest("streaming", startedAt, false)
		h.writeJSON(w, http.StatusForbidden, map[string]string{"error": "nft access denied"})
		return
	}
	h.setCachedNFTAccess(cacheKey, cachedNFTAccess{
		HasNFT:    hasNFT,
		Balance:   1,
		ExpiresAt: time.Now().Add(60 * time.Second),
	})

	contentID := strings.TrimPrefix(r.URL.Path, "/api/v1/streaming/")
	contentID = strings.TrimSuffix(contentID, "/manifest.m3u8")
	playbackToken, err := h.authService.GeneratePlaybackToken(wallet, contentID, contract, tokenID, chainID, 2*time.Minute)
	if err != nil {
		h.metricsCollector.IncrementCounter("streaming_manifest_failed_total", map[string]string{"chain": strconv.FormatInt(chainID, 10)})
		h.recordRequest("streaming", startedAt, false)
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.metricsCollector.IncrementCounter("streaming_manifest_success_total", map[string]string{"chain": strconv.FormatInt(chainID, 10)})
	h.recordRequest("streaming", startedAt, true)

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(
		"#EXTM3U\n" +
			"#EXT-X-VERSION:3\n" +
			"#EXT-X-TARGETDURATION:10\n" +
			"#EXT-X-MEDIA-SEQUENCE:0\n" +
			"#EXTINF:10.0,\n" +
			"/api/v1/streaming/" + contentID + "/segment/0?playback_token=" + playbackToken + "\n" +
			"#EXT-X-ENDLIST\n",
	))
}

// ProtectedSegmentHandler validates the playback token for media segment access.
func (h *Handler) ProtectedSegmentHandler(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	if r.Method != http.MethodGet {
		h.metricsCollector.IncrementCounter("streaming_segment_invalid_method_total", nil)
		h.recordRequest("streaming", startedAt, false)
		h.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !h.ensureServices(w) {
		h.metricsCollector.IncrementCounter("streaming_segment_failed_total", nil)
		h.recordRequest("streaming", startedAt, false)
		return
	}

	token := strings.TrimSpace(r.URL.Query().Get("playback_token"))
	if token == "" {
		h.metricsCollector.IncrementCounter("streaming_segment_unauthorized_total", nil)
		h.recordRequest("streaming", startedAt, false)
		h.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing playback token"})
		return
	}

	contentPath := strings.TrimPrefix(r.URL.Path, "/api/v1/streaming/")
	contentID := strings.SplitN(contentPath, "/segment/", 2)[0]
	if _, err := h.authService.ValidatePlaybackToken(token, contentID); err != nil {
		h.metricsCollector.IncrementCounter("streaming_segment_unauthorized_total", nil)
		h.recordRequest("streaming", startedAt, false)
		h.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid playback token"})
		return
	}

	w.Header().Set("Content-Type", "video/mp2t")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("segment"))
	h.metricsCollector.IncrementCounter("streaming_segment_success_total", nil)
	h.recordRequest("streaming", startedAt, true)
}
