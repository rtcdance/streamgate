package gateway

import (
	"container/list"
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/rtcdance/streamgate/pkg/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

type manifestCacheEntry struct {
	manifest   string
	expiresAt  time.Time
	walletAddr string // bound to issuing wallet to prevent cross-user token reuse
}

type segmentIndexEntry struct {
	qualities map[string][]string
	expiresAt time.Time
}

const (
	manifestCacheTTL     = 30 * time.Second
	segmentIndexCacheTTL = 2 * time.Minute
	maxManifestCacheSize = 10000
	maxSegmentIndexSize  = 10000
)

type lruEntry[T any] struct {
	key   string
	value T
}

type lruCache[T any] struct {
	mu      sync.RWMutex
	items   map[string]*list.Element
	order   *list.List
	maxSize int
	ttl     time.Duration
}

func newLRUCache[T any](maxSize int, ttl time.Duration) *lruCache[T] {
	return &lruCache[T]{
		items:   make(map[string]*list.Element),
		order:   list.New(),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

func (c *lruCache[T]) Get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.items[key]
	if !ok {
		var zero T
		return zero, false
	}
	entry := elem.Value.(*lruEntry[T])
	c.order.MoveToFront(elem)
	return entry.value, true
}

func (c *lruCache[T]) Set(key string, value T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		elem.Value.(*lruEntry[T]).value = value
		return
	}
	entry := &lruEntry[T]{key: key, value: value}
	elem := c.order.PushFront(entry)
	c.items[key] = elem
	for len(c.items) > c.maxSize {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*lruEntry[T]).key)
	}
}

func (c *lruCache[T]) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.order.Remove(elem)
		delete(c.items, key)
	}
}

type StreamingCache struct {
	manifests  *lruCache[manifestCacheEntry]
	segmentIdx *lruCache[segmentIndexEntry]
	sfGroup    singleflight.Group
}

func NewStreamingCache() *StreamingCache {
	return &StreamingCache{
		manifests:  newLRUCache[manifestCacheEntry](maxManifestCacheSize, manifestCacheTTL),
		segmentIdx: newLRUCache[segmentIndexEntry](maxSegmentIndexSize, segmentIndexCacheTTL),
	}
}

func (sc *StreamingCache) GetManifest(contentID, wallet string) (string, bool) {
	if entry, ok := sc.manifests.Get(contentID); ok && time.Now().Before(entry.expiresAt) && entry.walletAddr == wallet {
		return entry.manifest, true
	}
	return "", false
}

func (sc *StreamingCache) SetManifest(contentID, manifest, wallet string) {
	sc.manifests.Set(contentID, manifestCacheEntry{manifest: manifest, expiresAt: time.Now().Add(manifestCacheTTL), walletAddr: wallet})
}

func (sc *StreamingCache) GetSegmentIndex(contentID string) (map[string][]string, bool) {
	if entry, ok := sc.segmentIdx.Get(contentID); ok && time.Now().Before(entry.expiresAt) {
		return entry.qualities, true
	}
	return nil, false
}

func (sc *StreamingCache) SetSegmentIndex(contentID string, qualities map[string][]string) {
	sc.segmentIdx.Set(contentID, segmentIndexEntry{qualities: qualities, expiresAt: time.Now().Add(segmentIndexCacheTTL)})
}

func (sc *StreamingCache) Invalidate(contentID string) {
	sc.manifests.Delete(contentID)
	sc.segmentIdx.Delete(contentID)
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
	router.GET(APIPrefix+"/streaming/:id/manifest.m3u8", func(c *gin.Context) {
		contentID := c.Param("id")
		if !isValidContentID(contentID) {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid content ID")
			return
		}

		playbackToken := extractPlaybackToken(c)
		var quality string
		quality = c.Query("quality")

		if playbackToken != "" && quality != "" {
			claims, validateErr := authService.ValidatePlaybackToken(c.Request.Context(), playbackToken, contentID, c.GetHeader("X-Client-Fingerprint"))
			if validateErr != nil {
				middleware.GetLogger(c, log).Warn("playback token validation failed for sub-manifest",
					zap.String("content_id", contentID),
					zap.Error(validateErr))
				abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "invalid playback token")
				return
			}
			_ = claims

			if limiter != nil && !limiter.tryAcquire() {
				c.Header("Retry-After", "1")
				abortWithError(c, http.StatusServiceUnavailable, ErrStreamLimitReached, "too many concurrent streams")
				return
			}
			if limiter != nil {
				defer limiter.release()
			}

			var qualitySegments map[string][]string
			if cached, ok := cache.GetSegmentIndex(contentID); ok {
				qualitySegments = cached
			} else {
				qualitySegments = make(map[string][]string)
			}
			segs, ok := qualitySegments[quality]
			if !ok {
				abortWithError(c, http.StatusNotFound, ErrContentNotFound, "quality not found")
				return
			}
			manifest := service.BuildMediaPlaylist(contentID, quality, segs, playbackToken)
			c.Header("Content-Type", "application/vnd.apple.mpegurl")
			c.Header("Cache-Control", "private, max-age=30")
			c.String(http.StatusOK, manifest)
			return
		}

		wallet := middleware.GetWalletAddress(c)
		contract := middleware.GetNFTContract(c)
		chainID, _ := c.Get("nft_chain_id")
		tokenID := c.Query("token_id")
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

		generatedToken, err := authService.GeneratePlaybackToken(c.Request.Context(), wallet, contentID, contract, tokenID, chainIDInt, 30*time.Minute, c.GetHeader("X-Client-Fingerprint"))
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}
		playbackToken = generatedToken

		if cached, ok := cache.GetManifest(contentID, wallet); ok {
			monitoring.StreamingCacheHitsTotal.WithLabelValues("manifest").Inc()
			rendered := strings.ReplaceAll(cached, "{{PLAYBACK_TOKEN}}", playbackToken)
			c.Header("Content-Type", "application/vnd.apple.mpegurl")
			c.String(http.StatusOK, rendered)
			return
		}

		var qualitySegments map[string][]string
		if cached, ok := cache.GetSegmentIndex(contentID); ok {
			monitoring.StreamingCacheHitsTotal.WithLabelValues("segment_index").Inc()
			qualitySegments = cached
		} else {
			v, err, _ := cache.sfGroup.Do("segidx:"+contentID, func() (interface{}, error) {
				qs := make(map[string][]string)
				segmentPrefix := fmt.Sprintf("streams/%s/", contentID)
				if objStorage != nil {
					if objs, listErr := objStorage.ListObjects(c.Request.Context(), segBucket, segmentPrefix); listErr == nil {
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
							qs[quality] = append(qs[quality], segName)
						}
					}
				}
				if len(qs) > 0 {
					cache.SetSegmentIndex(contentID, qs)
				}
				return qs, nil
			})
			if err != nil {
				qualitySegments = make(map[string][]string)
			} else {
				qualitySegments = v.(map[string][]string)
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

		quality = c.Query("quality")
		if quality != "" {
			segs, ok := qualitySegments[quality]
			if !ok {
				abortWithError(c, http.StatusNotFound, ErrContentNotFound, "quality not found for this content")
				return
			}
			manifest := service.BuildMediaPlaylist(contentID, quality, segs, "{{PLAYBACK_TOKEN}}")
			rendered := strings.ReplaceAll(manifest, "{{PLAYBACK_TOKEN}}", playbackToken)
			c.Header("Content-Type", "application/vnd.apple.mpegurl")
			c.Header("Cache-Control", "private, max-age=30")
			c.String(http.StatusOK, rendered)
			return
		}

		manifest, err := streamingSvc.GenerateHLSPlaylist(contentID, qualitySegments, "{{PLAYBACK_TOKEN}}")
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}

		cache.SetManifest(contentID, manifest, wallet)

		rendered := strings.ReplaceAll(manifest, "{{PLAYBACK_TOKEN}}", playbackToken)
		c.Header("Content-Type", "application/vnd.apple.mpegurl")
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

	router.GET(APIPrefix+"/streaming/:id/segment/:num", func(c *gin.Context) {
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
		claims, err := authService.ValidatePlaybackToken(c.Request.Context(), playbackToken, contentID, c.GetHeader("X-Client-Fingerprint"), wallet)
		if err != nil {
			middleware.GetLogger(c, log).Warn("playback token validation failed",
				zap.String("content_id", contentID),
				zap.Error(err))
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
		if !validateSegmentName(segName) {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid segment name")
			return
		}
		if objStorage != nil {
			candidates := buildSegmentCandidates(contentID, segName, quality, cache)

			ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
			defer cancel()

			start := time.Now()

			type dlResult struct {
				rc   io.ReadCloser
				err  error
				prio int
			}
			ch := make(chan dlResult, len(candidates))
			var wg sync.WaitGroup
			for _, cand := range candidates {
				cand := cand
				wg.Add(1)
				go func() {
					defer wg.Done()
					rc, dlErr := objStorage.DownloadStream(ctx, segBucket, cand.key)
					select {
					case ch <- dlResult{rc: rc, err: dlErr, prio: cand.prio}:
					case <-ctx.Done():
						if rc != nil {
							rc.Close()
						}
					}
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
			wg.Wait()

			if best.rc == nil {
				cancel()
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
			cancel()
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
	// Query parameter playback_token takes priority over Authorization header.
	// HLS sub-manifest and segment URLs embed playback_token in the query string;
	// the Authorization header carries the JWT and should not be used for these requests.
	if pt := strings.TrimSpace(c.Query("playback_token")); pt != "" {
		return pt
	}
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader != "" {
		bearer := strings.TrimPrefix(authHeader, "Bearer ")
		if bearer != authHeader {
			return bearer
		}
	}
	return ""
}

func extractSegmentNumber(segName string) int {
	base := segName
	if idx := strings.LastIndex(segName, "/"); idx >= 0 {
		base = segName[idx+1:]
	}
	base = strings.TrimSuffix(base, ".ts")
	// Extract only the trailing number to avoid false digits in quality labels (e.g. "720p")
	end := len(base)
	for end > 0 && base[end-1] >= '0' && base[end-1] <= '9' {
		end--
	}
	n := 0
	for _, ch := range base[end:] {
		n = n*10 + int(ch-'0')
	}
	return n
}

func isValidContentID(id string) bool {
	if id == "" || len(id) > 256 {
		return false
	}
	for _, ch := range id {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '-' && ch != '_' {
			return false
		}
	}
	return true
}

type segmentCandidate struct {
	key  string
	prio int
}

func validateSegmentName(segName string) bool {
	if strings.Contains(segName, "\\") || strings.Contains(segName, "..") {
		return false
	}
	cleaned := path.Clean(segName)
	if cleaned != segName || path.IsAbs(cleaned) {
		return false
	}
	if !strings.HasSuffix(segName, ".ts") {
		return false
	}
	return true
}

func buildSegmentCandidates(contentID, segName, quality string, cache *StreamingCache) []segmentCandidate {
	candidates := []segmentCandidate{
		{key: fmt.Sprintf("%s/%s", contentID, segName), prio: 0},
	}
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
			candidates = append(candidates, segmentCandidate{
				key:  fmt.Sprintf("streams/%s/%s/%s", contentID, q, segName),
				prio: prio,
			})
		}
	} else {
		if quality != "" {
			candidates = append(candidates, segmentCandidate{
				key:  fmt.Sprintf("streams/%s/%s/%s", contentID, quality, segName),
				prio: 2,
			})
		}
		candidates = append(candidates, segmentCandidate{
			key:  fmt.Sprintf("streams/%s/720p/%s", contentID, segName),
			prio: 1,
		})
	}
	return candidates
}
