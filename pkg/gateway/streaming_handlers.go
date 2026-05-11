package gateway

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"streamgate/pkg/middleware"
	"streamgate/pkg/service"
)

// RegisterStreamingRoutes registers the HLS manifest delivery route (NFT-gated).
func RegisterStreamingRoutes(router gin.IRouter, log *zap.Logger, authService *service.AuthService, objStorage service.SegmentStorage) {
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
		manifest := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n#EXT-X-MEDIA-SEQUENCE:0\n"
		if objStorage != nil {
			segmentPrefix := fmt.Sprintf("%s/", contentID)
			if objects, err := objStorage.ListObjects(c.Request.Context(), "streamgate", segmentPrefix); err == nil && len(objects) > 0 {
				for _, key := range objects {
					if !strings.HasSuffix(key, ".ts") || !strings.Contains(key, "720p") {
						continue
					}
					segName := key[strings.LastIndex(key, "/")+1:]
					manifest += fmt.Sprintf("#EXTINF:4.0,\n/api/v1/streaming/%s/segment/%s?playback_token=%s\n", contentID, segName, playbackToken)
				}
			} else {
				manifest += fmt.Sprintf("#EXTINF:10.0,\n/api/v1/streaming/%s/segment/720p_000.ts?playback_token=%s\n", contentID, playbackToken)
			}
		} else {
			manifest += fmt.Sprintf("#EXTINF:10.0,\n/api/v1/streaming/%s/segment/720p_000.ts?playback_token=%s\n", contentID, playbackToken)
		}
		manifest += "#EXT-X-ENDLIST\n"
		c.Header("Content-Type", "application/vnd.apple.mpegurl")
		c.String(http.StatusOK, manifest)
	})
	log.Info("Streaming routes registered")
}

// RegisterStreamingSegmentRoute registers the segment download route (playback-token validated).
func RegisterStreamingSegmentRoute(router gin.IRouter, log *zap.Logger, authService *service.AuthService, objStorage service.SegmentStorage) {
	router.GET("/api/v1/streaming/:id/segment/:num", func(c *gin.Context) {
		playbackToken := strings.TrimSpace(c.Query("playback_token"))
		if playbackToken == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "missing playback token")
			return
		}
		if _, err := authService.ValidatePlaybackToken(playbackToken, c.Param("id")); err != nil {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "invalid playback token")
			return
		}
		contentID := c.Param("id")
		segName := c.Param("num")
		// Validate segName: must be a .ts filename with no path traversal
		if strings.Contains(segName, "..") || strings.Contains(segName, "/") || strings.Contains(segName, "\\") {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid segment name")
			return
		}
		if !strings.HasSuffix(segName, ".ts") {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid segment format")
			return
		}
		if objStorage != nil {
			objectKey := fmt.Sprintf("%s/%s", contentID, segName)
			data, err := objStorage.Download(c.Request.Context(), "streamgate", objectKey)
			if err != nil {
				middleware.GetLogger(c, log).Warn("Segment download failed",
					zap.String("key", objectKey),
					zap.Error(err))
				abortWithError(c, http.StatusServiceUnavailable, ErrContentUnavailable, "segment unavailable")
				return
			}
			c.Header("Content-Type", "video/mp2t")
			c.Data(http.StatusOK, "video/mp2t", data)
			return
		}
		abortWithError(c, http.StatusNotFound, ErrNotFound, "segment not found")
	})
}
