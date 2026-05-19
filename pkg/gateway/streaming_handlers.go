package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"streamgate/pkg/middleware"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type manifestCacheEntry struct {
	manifest  string
	expiresAt time.Time
}

type segmentIndexEntry struct {
	qualities map[string][]string
	expiresAt time.Time
}

const (
	manifestCacheTTL      = 30 * time.Second
	segmentIndexCacheTTL  = 2 * time.Minute
	maxManifestCacheSize  = 10000
	maxSegmentIndexSize   = 10000
)

type StreamingCache struct {
	manifests    map[string]manifestCacheEntry
	manifestsMu  sync.RWMutex
	segmentIdx   map[string]segmentIndexEntry
	segmentIdxMu sync.RWMutex
}

func NewStreamingCache() *StreamingCache {
	return &StreamingCache{
		manifests:  make(map[string]manifestCacheEntry),
		segmentIdx: make(map[string]segmentIndexEntry),
	}
}

func (sc *StreamingCache) GetManifest(contentID string) (string, bool) {
	sc.manifestsMu.RLock()
	defer sc.manifestsMu.RUnlock()
	if entry, ok := sc.manifests[contentID]; ok && time.Now().Before(entry.expiresAt) {
		return entry.manifest, true
	}
	return "", false
}

func (sc *StreamingCache) SetManifest(contentID, manifest string) {
	sc.manifestsMu.Lock()
	defer sc.manifestsMu.Unlock()
	sc.manifests[contentID] = manifestCacheEntry{manifest: manifest, expiresAt: time.Now().Add(manifestCacheTTL)}
	if len(sc.manifests) > maxManifestCacheSize {
		now := time.Now()
		evicted := 0
		for k, v := range sc.manifests {
			if now.After(v.expiresAt) {
				delete(sc.manifests, k)
				evicted++
			}
		}
		if len(sc.manifests) > maxManifestCacheSize {
			for k := range sc.manifests {
				if evicted >= len(sc.manifests)/2 {
					break
				}
				delete(sc.manifests, k)
				evicted++
			}
		}
	}
}

func (sc *StreamingCache) GetSegmentIndex(contentID string) (map[string][]string, bool) {
	sc.segmentIdxMu.RLock()
	defer sc.segmentIdxMu.RUnlock()
	if entry, ok := sc.segmentIdx[contentID]; ok && time.Now().Before(entry.expiresAt) {
		return entry.qualities, true
	}
	return nil, false
}

func (sc *StreamingCache) SetSegmentIndex(contentID string, qualities map[string][]string) {
	sc.segmentIdxMu.Lock()
	defer sc.segmentIdxMu.Unlock()
	sc.segmentIdx[contentID] = segmentIndexEntry{qualities: qualities, expiresAt: time.Now().Add(segmentIndexCacheTTL)}
	if len(sc.segmentIdx) > maxSegmentIndexSize {
		now := time.Now()
		evicted := 0
		for k, v := range sc.segmentIdx {
			if now.After(v.expiresAt) {
				delete(sc.segmentIdx, k)
				evicted++
			}
		}
		if len(sc.segmentIdx) > maxSegmentIndexSize {
			for k := range sc.segmentIdx {
				if evicted >= len(sc.segmentIdx)/2 {
					break
				}
				delete(sc.segmentIdx, k)
				evicted++
			}
		}
	}
}

func (sc *StreamingCache) Invalidate(contentID string) {
	sc.manifestsMu.Lock()
	delete(sc.manifests, contentID)
	sc.manifestsMu.Unlock()
	sc.segmentIdxMu.Lock()
	delete(sc.segmentIdx, contentID)
	sc.segmentIdxMu.Unlock()
}

type streamLimiter struct {
	sem chan struct{}
}

func newStreamLimiter(maxVal int) *streamLimiter {
	if maxVal <= 0 {
		maxVal = 1000
	}
	return &streamLimiter{sem: make(chan struct{}, maxVal)}
}

func (l *streamLimiter) tryAcquire() bool {
	select {
	case l.sem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l *streamLimiter) release() {
	<-l.sem
}

func RegisterStreamingRoutes(router gin.IRouter, log *zap.Logger, authService *service.AuthService, streamingSvc *service.StreamingService, objStorage service.SegmentStorage, limiter *streamLimiter, cache *StreamingCache, bucket ...string) {
	if cache == nil {
		cache = NewStreamingCache()
	}
	segBucket := "streamgate"
	if len(bucket) > 0 && bucket[0] != "" {
		segBucket = bucket[0]
	}
	router.GET("/api/v1/streaming/:id/manifest.m3u8", func(c *gin.Context) {
		wallet := middleware.GetWalletAddress(c)
		contract := middleware.GetNFTContract(c)
		chainID, _ := c.Get("nft_chain_id")
		tokenID := c.Query("token_id")
		contentID := c.Param("id")
		if !isValidContentID(contentID) {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid content ID")
			return
		}
		var chainIDInt int64 = 1
		if v, ok := chainID.(int64); ok {
			chainIDInt = v
		}
		if limiter != nil && !limiter.tryAcquire() {
			c.Header("Retry-After", "1")
			abortWithError(c, http.StatusServiceUnavailable, ErrStreamLimitReached, "too many concurrent streams; try again shortly")
			return
		}
		if limiter != nil {
			defer func() {
				limiter.release()
				monitoring.StreamingViewersActive.Set(float64(cap(limiter.sem) - len(limiter.sem)))
			}()
			monitoring.StreamingViewersActive.Set(float64(cap(limiter.sem) - len(limiter.sem)))
		}
		monitoring.StreamingManifestsTotal.Inc()

		playbackToken, err := authService.GeneratePlaybackToken(c.Request.Context(), wallet, contentID, contract, tokenID, chainIDInt, 2*time.Minute)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(err), err.Error())
			return
		}

		if cached, ok := cache.GetManifest(contentID); ok {
			monitoring.StreamingCacheHitsTotal.WithLabelValues("manifest").Inc()
			rendered := strings.ReplaceAll(cached, "{{PLAYBACK_TOKEN}}", playbackToken)
			c.Header("Content-Type", "application/vnd.apple.mpegurl")
			c.Header("X-Content-Type-Options", "nosniff")
			c.String(http.StatusOK, rendered)
			return
		}

		var qualitySegments map[string][]string
		if cached, ok := cache.GetSegmentIndex(contentID); ok {
			monitoring.StreamingCacheHitsTotal.WithLabelValues("segment_index").Inc()
			qualitySegments = cached
		} else {
			qualitySegments = make(map[string][]string)
			segmentPrefix := fmt.Sprintf("streams/%s/", contentID)
			if objStorage != nil {
				if objs, err := objStorage.ListObjects(c.Request.Context(), segBucket, segmentPrefix); err == nil {
					for _, key := range objs {
						if !strings.HasSuffix(key, ".ts") {
							continue
						}
						rel := strings.TrimPrefix(key, segmentPrefix)
						parts := strings.SplitN(rel, "/", 2)
						quality := "default"
						segName := rel
						if len(parts) == 2 {
							quality = parts[0]
							segName = parts[1]
						}
						qualitySegments[quality] = append(qualitySegments[quality], segName)
					}
				}
			}
			if len(qualitySegments) > 0 {
				cache.SetSegmentIndex(contentID, qualitySegments)
			}
		}
		if len(qualitySegments) == 0 {
			abortWithError(c, http.StatusNotFound, ErrContentNotFound, "content not ready; transcode may still be processing")
			return
		}

		for q, segs := range qualitySegments {
			sort.Slice(segs, func(i, j int) bool {
				return extractSegmentNumber(segs[i]) < extractSegmentNumber(segs[j])
			})
			qualitySegments[q] = segs
		}

		manifest, err := streamingSvc.GenerateHLSPlaylist(contentID, qualitySegments, "{{PLAYBACK_TOKEN}}")
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(err), err.Error())
			return
		}

		cache.SetManifest(contentID, manifest)

		rendered := strings.ReplaceAll(manifest, "{{PLAYBACK_TOKEN}}", playbackToken)
		c.Header("Content-Type", "application/vnd.apple.mpegurl")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Cache-Control", "private, max-age=30") // per-user token in body; browser-only cache
		c.String(http.StatusOK, rendered)
	})
	log.Info("Streaming routes registered")
}

func RegisterStreamingSegmentRoute(router gin.IRouter, log *zap.Logger, authService *service.AuthService, objStorage service.SegmentStorage, limiter *streamLimiter, cache *StreamingCache, bucket ...string) {
	segBucket := "streamgate"
	if len(bucket) > 0 && bucket[0] != "" {
		segBucket = bucket[0]
	}
	router.GET("/api/v1/streaming/:id/segment/:num", func(c *gin.Context) {
		playbackToken := extractPlaybackToken(c)
		if playbackToken == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "missing playback token")
			return
		}
		contentID := c.Param("id")
		if !isValidContentID(contentID) {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid content ID")
			return
		}
		wallet := middleware.GetWalletAddress(c)
		claims, err := authService.ValidatePlaybackToken(c.Request.Context(), playbackToken, contentID, wallet)
		if err != nil {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "invalid playback token")
			return
		}
		if claims.Contract != "" || claims.TokenID != "" {
			// Playback token carries NFT contract/tokenID; verify it matches
			// the NFT gate context to prevent token reuse across content.
			if c.GetBool("nft_verified") {
				gateContract := middleware.GetNFTContract(c)
				if gateContract != "" && !strings.EqualFold(claims.Contract, gateContract) {
					abortWithError(c, http.StatusForbidden, ErrUnauthorized, "nft contract mismatch")
					return
				}
			}
		}
		if limiter != nil && !limiter.tryAcquire() {
			c.Header("Retry-After", "1")
			abortWithError(c, http.StatusServiceUnavailable, ErrStreamLimitReached, "too many concurrent streams; try again shortly")
			return
		}
		if limiter != nil {
			defer limiter.release()
		}
		quality := c.Query("quality")
		segName := c.Param("num")
		// Normalize path to prevent directory traversal attacks (including Windows-style backslashes)
		if strings.Contains(segName, "\\") || strings.Contains(segName, "..") {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid segment name")
			return
		}
		cleaned := path.Clean(segName)
		if cleaned != segName || path.IsAbs(cleaned) {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid segment name")
			return
		}
		if !strings.HasSuffix(segName, ".ts") {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid segment format")
			return
		}
		if objStorage != nil {
			type segTry struct {
				key  string
				prio int
			}
			candidates := []segTry{
				{key: fmt.Sprintf("%s/%s", contentID, segName), prio: 0},
			}
			// Use segment index cache to find available quality levels,
			// avoiding wasted MinIO requests to non-existent profiles.
			var qlist map[string][]string
			if cached, ok := cache.GetSegmentIndex(contentID); ok {
				qlist = cached
			}
			if len(qlist) > 0 {
				for q := range qlist {
					prio := 1
					if q == quality {
						prio = 2
					}
					candidates = append(candidates, segTry{
						key:  fmt.Sprintf("streams/%s/%s/%s", contentID, q, segName),
						prio: prio,
					})
				}
			} else {
				if quality != "" {
					candidates = append(candidates, segTry{
						key:  fmt.Sprintf("streams/%s/%s/%s", contentID, quality, segName),
						prio: 2,
					})
				}
				candidates = append(candidates, segTry{
					key:  fmt.Sprintf("streams/%s/720p/%s", contentID, segName),
					prio: 1,
				})
			}

			ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
			defer cancel()

			start := time.Now()

			type dlResult struct {
				rc   io.ReadCloser
				err  error
				prio int
			}
			ch := make(chan dlResult, len(candidates))
			for _, cand := range candidates {
				cand := cand
				go func() {
					rc, dlErr := objStorage.DownloadStream(ctx, segBucket, cand.key)
					ch <- dlResult{rc: rc, err: dlErr, prio: cand.prio}
				}()
			}

			var best dlResult
			for i := 0; i < len(candidates); i++ {
				res := <-ch
				if res.err == nil && res.rc != nil {
					if best.rc == nil || res.prio > best.prio {
						if best.rc != nil {
							best.rc.Close()
						}
						best = res
					} else {
						res.rc.Close()
					}
				}
			}
			cancel()

			if best.rc == nil {
				monitoring.StreamingDownloadDuration.WithLabelValues("fail").Observe(time.Since(start).Seconds())
				middleware.GetLogger(c, log).Warn("Segment download failed",
					zap.String("content_id", contentID),
					zap.String("segment", segName))
				abortWithError(c, http.StatusServiceUnavailable, ErrContentUnavailable, "segment unavailable")
				return
			}
			defer func() { _ = best.rc.Close() }()
			c.Header("Content-Type", "video/mp2t")
			c.Header("Cache-Control", "private, max-age=86400")
			c.Header("Vary", "Authorization")
			c.Header("X-Content-Type-Options", "nosniff")
			c.Status(http.StatusOK)
			if _, err := io.Copy(c.Writer, best.rc); err != nil {
				log.Warn("segment download interrupted", zap.String("content_id", c.Param("id")), zap.Error(err))
			}
			monitoring.StreamingDownloadDuration.WithLabelValues("success").Observe(time.Since(start).Seconds())
			quality := c.Query("quality")
			if quality == "" {
				quality = "default"
			}
			monitoring.StreamingSegmentsTotal.WithLabelValues(quality).Inc()
			return
		}
		_ = claims
		abortWithError(c, http.StatusNotFound, ErrNotFound, "segment not found")
	})
}

func extractPlaybackToken(c *gin.Context) string {
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader != "" {
		bearer := strings.TrimPrefix(authHeader, "Bearer ")
		if bearer != authHeader {
			return bearer
		}
	}
	return strings.TrimSpace(c.Query("playback_token"))
}

func extractSegmentNumber(segName string) int {
	base := segName
	if idx := strings.LastIndex(segName, "/"); idx >= 0 {
		base = segName[idx+1:]
	}
	base = strings.TrimSuffix(base, ".ts")
	n := 0
	for _, ch := range base {
		if ch >= '0' && ch <= '9' {
			n = n*10 + int(ch-'0')
		}
	}
	return n
}

func isValidContentID(id string) bool {
	if id == "" || len(id) > 256 {
		return false
	}
	for _, ch := range id {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}
