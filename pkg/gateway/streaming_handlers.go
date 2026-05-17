package gateway

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"streamgate/pkg/middleware"
	"streamgate/pkg/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type manifestCacheEntry struct {
	manifest  string
	expiresAt time.Time
}

var (
	manifestCache   = make(map[string]manifestCacheEntry)
	manifestCacheMu sync.RWMutex
)

const manifestCacheTTL = 30 * time.Second

// RegisterStreamingRoutes registers the HLS manifest delivery route (NFT-gated).
func RegisterStreamingRoutes(router gin.IRouter, log *zap.Logger, authService *service.AuthService, objStorage service.SegmentStorage, bucket ...string) {
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
		playbackToken, err := authService.GeneratePlaybackToken(wallet, contentID, contract, tokenID, chainIDInt, 2*time.Minute)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(err), err.Error())
			return
		}

		manifestCacheMu.RLock()
		if entry, ok := manifestCache[contentID]; ok && time.Now().Before(entry.expiresAt) {
			manifestCacheMu.RUnlock()
			rendered := strings.ReplaceAll(entry.manifest, "{{PLAYBACK_TOKEN}}", playbackToken)
			c.Header("Content-Type", "application/vnd.apple.mpegurl")
			c.String(http.StatusOK, rendered)
			return
		}
		manifestCacheMu.RUnlock()

		segmentPrefix := fmt.Sprintf("streams/%s/", contentID)
		qualitySegments := make(map[string][]string)
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
		manifestCache[contentID] = manifestCacheEntry{manifest: manifest, expiresAt: time.Now().Add(manifestCacheTTL)}
		if len(manifestCache) > 10000 {
			now := time.Now()
			for k, v := range manifestCache {
				if now.After(v.expiresAt) {
					delete(manifestCache, k)
				}
			}
		}
		manifestCacheMu.Unlock()

		rendered := strings.ReplaceAll(manifest, "{{PLAYBACK_TOKEN}}", playbackToken)
		c.Header("Content-Type", "application/vnd.apple.mpegurl")
		c.String(http.StatusOK, rendered)
	})
	log.Info("Streaming routes registered")
}

// RegisterStreamingSegmentRoute registers the segment download route (playback-token validated).
func RegisterStreamingSegmentRoute(router gin.IRouter, log *zap.Logger, authService *service.AuthService, objStorage service.SegmentStorage, bucket ...string) {
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
		claims, err := authService.ValidatePlaybackToken(playbackToken, c.Param("id"))
		if err != nil {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "invalid playback token")
			return
		}
		contentID := c.Param("id")
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
			var rc io.ReadCloser
			quality := c.Query("quality")
			if quality != "" {
				objectKey := fmt.Sprintf("streams/%s/%s/%s", contentID, quality, segName)
				rc, err = objStorage.DownloadStream(c.Request.Context(), segBucket, objectKey)
			}
			if rc == nil {
				objectKey := fmt.Sprintf("streams/%s/720p/%s", contentID, segName)
				rc, err = objStorage.DownloadStream(c.Request.Context(), segBucket, objectKey)
			}
			if rc == nil {
				objectKey := fmt.Sprintf("%s/%s", contentID, segName)
				rc, err = objStorage.DownloadStream(c.Request.Context(), segBucket, objectKey)
			}
			if err != nil || rc == nil {
				middleware.GetLogger(c, log).Warn("Segment download failed",
					zap.String("content_id", contentID),
					zap.String("segment", segName),
					zap.Error(err))
				abortWithError(c, http.StatusServiceUnavailable, ErrContentUnavailable, "segment unavailable")
				return
			}
			defer func() { _ = rc.Close() }()
			c.Header("Content-Type", "video/mp2t")
			c.Header("Cache-Control", "private, max-age=3600")
			c.Header("X-Content-Type-Options", "nosniff")
			c.Status(http.StatusOK)
			if _, err := io.Copy(c.Writer, rc); err != nil {
				log.Warn("segment download interrupted", zap.String("content_id", c.Param("id")), zap.Error(err))
			}
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
