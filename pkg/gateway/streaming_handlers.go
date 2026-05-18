package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
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

var (
	manifestCache       = make(map[string]manifestCacheEntry)
	manifestCacheMu     sync.RWMutex
	segmentIndexCache   = make(map[string]segmentIndexEntry)
	segmentIndexCacheMu sync.RWMutex
)

const (
	manifestCacheTTL      = 30 * time.Second
	segmentIndexCacheTTL  = 2 * time.Minute
)

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

func RegisterStreamingRoutes(router gin.IRouter, log *zap.Logger, authService *service.AuthService, objStorage service.SegmentStorage, limiter *streamLimiter, bucket ...string) {
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
				monitoring.StreamingViewersActive.Set(float64(len(limiter.sem)))
			}()
			monitoring.StreamingViewersActive.Set(float64(len(limiter.sem)))
		}
		monitoring.StreamingManifestsTotal.Inc()

		playbackToken, err := authService.GeneratePlaybackToken(c.Request.Context(), wallet, contentID, contract, tokenID, chainIDInt, 2*time.Minute)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(err), err.Error())
			return
		}

		manifestCacheMu.RLock()
		if entry, ok := manifestCache[contentID]; ok && time.Now().Before(entry.expiresAt) {
			manifestCacheMu.RUnlock()
			monitoring.StreamingCacheHitsTotal.WithLabelValues("manifest").Inc()
			rendered := strings.ReplaceAll(entry.manifest, "{{PLAYBACK_TOKEN}}", playbackToken)
			c.Header("Content-Type", "application/vnd.apple.mpegurl")
			c.String(http.StatusOK, rendered)
			return
		}
		manifestCacheMu.RUnlock()

		segmentIndexCacheMu.RLock()
		cachedIdx, idxHit := segmentIndexCache[contentID]
		segmentIndexCacheMu.RUnlock()

		var qualitySegments map[string][]string
		if idxHit && time.Now().Before(cachedIdx.expiresAt) {
			monitoring.StreamingCacheHitsTotal.WithLabelValues("segment_index").Inc()
			qualitySegments = cachedIdx.qualities
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
				segmentIndexCacheMu.Lock()
				segmentIndexCache[contentID] = segmentIndexEntry{qualities: qualitySegments, expiresAt: time.Now().Add(segmentIndexCacheTTL)}
				segmentIndexCacheMu.Unlock()
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

		var manifest string
		if len(qualitySegments) == 1 {
			for _, segs := range qualitySegments {
				manifest = buildSimplePlaylist(contentID, segs, "{{PLAYBACK_TOKEN}}")
			}
		} else {
			manifest = buildMasterPlaylist(contentID, qualitySegments, "{{PLAYBACK_TOKEN}}")
		}

		manifestCacheMu.Lock()
		if entry, ok := manifestCache[contentID]; ok && time.Now().Before(entry.expiresAt) {
			manifest = entry.manifest
		} else {
			manifestCache[contentID] = manifestCacheEntry{manifest: manifest, expiresAt: time.Now().Add(manifestCacheTTL)}
			if len(manifestCache) > 10000 {
				now := time.Now()
				for k, v := range manifestCache {
					if now.After(v.expiresAt) {
						delete(manifestCache, k)
					}
				}
			}
		}
		manifestCacheMu.Unlock()

		rendered := strings.ReplaceAll(manifest, "{{PLAYBACK_TOKEN}}", playbackToken)
		c.Header("Content-Type", "application/vnd.apple.mpegurl")
		c.Header("Cache-Control", "private, max-age=30") // per-user token in body; browser-only cache
		c.String(http.StatusOK, rendered)
	})
	log.Info("Streaming routes registered")
}

func RegisterStreamingSegmentRoute(router gin.IRouter, log *zap.Logger, authService *service.AuthService, objStorage service.SegmentStorage, limiter *streamLimiter, bucket ...string) {
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
		claims, err := authService.ValidatePlaybackToken(c.Request.Context(), playbackToken, contentID)
		if err != nil {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "invalid playback token")
			return
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
		if strings.Contains(segName, "..") || strings.Contains(segName, "/") || strings.Contains(segName, "\\") {
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
			segmentIndexCacheMu.RLock()
			if cachedIdx, ok := segmentIndexCache[contentID]; ok && time.Now().Before(cachedIdx.expiresAt) {
				qlist = cachedIdx.qualities
			}
			segmentIndexCacheMu.RUnlock()
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

			ctx, cancel := context.WithCancel(c.Request.Context())
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
			c.Header("Cache-Control", "public, max-age=86400, s-maxage=3600")
			c.Header("X-Content-Type-Options", "nosniff")
			c.Status(http.StatusOK)
			if _, err := io.Copy(c.Writer, best.rc); err != nil {
				log.Warn("segment download interrupted", zap.String("content_id", c.Param("id")), zap.Error(err))
			}
			monitoring.StreamingDownloadDuration.WithLabelValues("success").Observe(time.Since(start).Seconds())
			quality := c.Query("quality")
			if quality == "" { quality = "default" }
			monitoring.StreamingSegmentsTotal.WithLabelValues(quality).Inc()
			return
		}
		_ = claims
		abortWithError(c, http.StatusNotFound, ErrNotFound, "segment not found")
	})
}

var qualityBandwidth = map[string]int{
	"1080p": 5000,
	"720p":  2800,
	"480p":  1400,
	"360p":  800,
}

func buildSimplePlaylist(contentID string, segments []string, playbackToken string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n#EXT-X-MEDIA-SEQUENCE:0\n")
	for _, seg := range segments {
		name := seg
		if idx := strings.LastIndex(seg, "/"); idx >= 0 {
			name = seg[idx+1:]
		}
		b.WriteString(fmt.Sprintf("#EXTINF:6.0,\n/api/v1/streaming/%s/segment/%s?playback_token=%s\n", contentID, name, playbackToken))
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

func buildMasterPlaylist(contentID string, qualitySegments map[string][]string, playbackToken string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	for quality, segs := range qualitySegments {
		bw := qualityBandwidth[quality]
		if bw == 0 {
			bw = 1500
		}
		resolution := qualityToResolution(quality)
		b.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%s\n", bw*1000, resolution))
		b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-TARGETDURATION:10\n")
		for _, seg := range segs {
			name := seg
			if idx := strings.LastIndex(seg, "/"); idx >= 0 {
				name = seg[idx+1:]
			}
			b.WriteString(fmt.Sprintf("#EXTINF:6.0,\n/api/v1/streaming/%s/segment/%s?quality=%s&playback_token=%s\n", contentID, name, quality, playbackToken))
		}
		b.WriteString("#EXT-X-ENDLIST\n")
	}
	return b.String()
}

func qualityToResolution(quality string) string {
	switch quality {
	case "1080p":
		return "1920x1080"
	case "720p":
		return "1280x720"
	case "480p":
		return "854x480"
	case "360p":
		return "640x360"
	default:
		return "1280x720"
	}
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
