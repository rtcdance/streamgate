package gateway

import (
	"container/list"
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/rtcdance/streamgate/pkg/service"
	streamingsvc "github.com/rtcdance/streamgate/pkg/service/streamingsvc"

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
	// playlists maps a quality to the object key of the media playlist FFmpeg
	// actually produced, and masterKey is the multivariant playlist's key when
	// there is one. Serving these instead of rebuilding a playlist is what keeps
	// EXTINF, TARGETDURATION, CODECS and BANDWIDTH truthful.
	playlists map[string]string
	masterKey string
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
	entry := segmentIndexEntry{qualities: qualities, expiresAt: time.Now().Add(segmentIndexCacheTTL)}
	if existing, ok := sc.segmentIdx.Get(contentID); ok {
		entry.playlists = existing.playlists
		entry.masterKey = existing.masterKey
	}
	sc.segmentIdx.Set(contentID, entry)
}

// GetStoredPlaylists returns the object keys of the playlists FFmpeg produced:
// a quality→media-playlist map and, when present, the multivariant playlist.
func (sc *StreamingCache) GetStoredPlaylists(contentID string) (map[string]string, string, bool) {
	if entry, ok := sc.segmentIdx.Get(contentID); ok && time.Now().Before(entry.expiresAt) {
		if len(entry.playlists) > 0 || entry.masterKey != "" {
			return entry.playlists, entry.masterKey, true
		}
	}
	return nil, "", false
}

// SetStoredPlaylists records the playlist object keys alongside the segment
// index, so one listing serves both.
func (sc *StreamingCache) SetStoredPlaylists(contentID string, playlists map[string]string, masterKey string) {
	entry, ok := sc.segmentIdx.Get(contentID)
	if !ok {
		entry = segmentIndexEntry{expiresAt: time.Now().Add(segmentIndexCacheTTL)}
	}
	entry.playlists = playlists
	entry.masterKey = masterKey
	entry.expiresAt = time.Now().Add(segmentIndexCacheTTL)
	sc.segmentIdx.Set(contentID, entry)
}

// lookupSegmentIndex lists the object store once for a content and populates
// both the segment index and the stored playlist keys. Concurrent callers share
// one listing through the singleflight group; a failed listing yields an empty
// index so the caller can treat the content as not ready.
func (sc *StreamingCache) lookupSegmentIndex(ctx context.Context, objStorage service.SegmentStorage, bucket, contentID string) map[string][]string {
	if objStorage == nil {
		return map[string][]string{}
	}
	v, err, _ := sc.sfGroup.Do("segidx:"+contentID, func() (interface{}, error) {
		qs := make(map[string][]string)
		pls := make(map[string]string)
		master := ""
		segmentPrefix := fmt.Sprintf("streams/%s/", contentID)
		if objs, listErr := objStorage.ListObjects(ctx, bucket, segmentPrefix); listErr == nil {
			for _, key := range objs {
				rel := strings.TrimPrefix(key, segmentPrefix)
				parts := strings.SplitN(rel, "/", 2)
				subdir := ""
				name := rel
				if len(parts) == 2 {
					subdir = parts[0]
					name = parts[1]
				}
				switch {
				case strings.HasSuffix(name, ".m3u8"):
					// The playlist FFmpeg wrote, keyed by the quality the
					// rest of the pipeline uses (the subdirectory).
					if name == "master.m3u8" {
						master = key
						continue
					}
					quality := subdir
					if quality == "" {
						quality = strings.TrimSuffix(name, ".m3u8")
					}
					pls[quality] = key
				case strings.HasSuffix(name, ".ts"):
					quality := "default"
					if subdir != "" {
						quality = subdir
					}
					qs[quality] = append(qs[quality], name)
				}
			}
		}
		if len(qs) > 0 {
			sc.SetSegmentIndex(contentID, qs)
		}
		if len(pls) > 0 || master != "" {
			sc.SetStoredPlaylists(contentID, pls, master)
		}
		return qs, nil
	})
	if err != nil {
		return map[string][]string{}
	}
	return v.(map[string][]string)
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

		// A playback token presented in the query MUST be validated regardless of
		// any other parameter — previously, omitting `quality` skipped the only
		// validation branch entirely. The decision deliberately uses the query
		// param only: extractPlaybackToken's Authorization-header fallback would
		// see the JWT itself on the master-manifest path.
		queryToken := c.Query("playback_token")
		quality := c.Query("quality")

		if queryToken != "" {
			wallet := middleware.GetWalletAddress(c)
			if wallet == "" {
				abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "authentication required")
				return
			}
			claims, validateErr := authService.ValidatePlaybackToken(c.Request.Context(), queryToken, contentID, c.GetHeader("X-Client-Fingerprint"), wallet)
			if validateErr != nil {
				middleware.GetLogger(c, log).Warn("playback token validation failed for manifest",
					zap.String("content_id", contentID),
					zap.Error(validateErr))
				abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "invalid playback token")
				return
			}
			_ = claims
		}

		if queryToken != "" && quality != "" {
			playbackToken := queryToken

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
				qualitySegments = cache.lookupSegmentIndex(c.Request.Context(), objStorage, segBucket, contentID)
			}

			// Prefer the playlist FFmpeg produced for this variant. The master
			// rewrite points players here, so a synthesised answer on this path
			// would still misstate the durations of every segment it lists.
			storedPlaylists, masterKey, _ := cache.GetStoredPlaylists(contentID)
			if serveStoredPlaylist(c, objStorage, segBucket, contentID, quality, playbackToken, storedPlaylists, masterKey) {
				return
			}

			segs, ok := qualitySegments[quality]
			if !ok {
				abortWithError(c, http.StatusNotFound, ErrContentNotFound, "quality not found")
				return
			}
			manifest := service.BuildMediaPlaylist(contentID, quality, segs, playbackToken)
			c.Header("Content-Type", "application/vnd.apple.mpegurl")
			setCapabilityResponseHeaders(c)
			c.String(http.StatusOK, manifest)
			return
		}

		wallet := middleware.GetWalletAddress(c)
		// JWTAuthMiddleware and PlaybackTokenAuth both guarantee a wallet by the
		// time we get here; refuse to mint a token if one somehow slipped through.
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "authentication required")
			return
		}
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

		// The capability lifetime comes from the policy (60s by default). It used
		// to be hardcoded to 30 minutes, which meant a revoked or transferred
		// holding kept playing for half an hour.
		//
		// It also inherits THIS request's session, so a logout revokes the
		// capability along with the rest of the login rather than leaving it
		// valid until it expires.
		sessionID, authEpoch := middleware.GetSessionBinding(c)
		generatedToken, err := authService.GeneratePlaybackToken(c.Request.Context(), wallet, contentID, contract, tokenID, chainIDInt, authService.PlaybackTokenTTL(), c.GetHeader("X-Client-Fingerprint"), service.WithSessionBinding(sessionID, authEpoch))
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}
		playbackToken := generatedToken

		if cached, ok := cache.GetManifest(contentID, wallet); ok {
			monitoring.StreamingCacheHitsTotal.WithLabelValues("manifest").Inc()
			rendered := strings.ReplaceAll(cached, "{{PLAYBACK_TOKEN}}", playbackToken)
			c.Header("Content-Type", "application/vnd.apple.mpegurl")
			setCapabilityResponseHeaders(c)
			c.String(http.StatusOK, rendered)
			return
		}

		var qualitySegments map[string][]string
		var storedPlaylists map[string]string
		var masterKey string
		if cached, ok := cache.GetSegmentIndex(contentID); ok {
			monitoring.StreamingCacheHitsTotal.WithLabelValues("segment_index").Inc()
			qualitySegments = cached
			storedPlaylists, masterKey, _ = cache.GetStoredPlaylists(contentID)
		} else {
			qualitySegments = cache.lookupSegmentIndex(c.Request.Context(), objStorage, segBucket, contentID)
			storedPlaylists, masterKey, _ = cache.GetStoredPlaylists(contentID)
		}
		if len(qualitySegments) == 0 && len(storedPlaylists) == 0 && masterKey == "" {
			abortWithError(c, http.StatusNotFound, ErrContentNotFound, "content not ready; transcode may still be processing")
			return
		}

		// Prefer the playlist FFmpeg actually produced: its EXTINF, TARGETDURATION,
		// MEDIA-SEQUENCE, CODECS and BANDWIDTH are the truth about this media. Only
		// the URIs are rewritten, and only to the protected route.
		if serveStoredPlaylist(c, objStorage, segBucket, contentID, quality, playbackToken, storedPlaylists, masterKey) {
			return
		}

		for q, segs := range qualitySegments {
			sort.Slice(segs, func(i, j int) bool {
				return extractSegmentNumber(segs[i]) < extractSegmentNumber(segs[j])
			})
			qualitySegments[q] = segs
		}

		if quality != "" {
			segs, ok := qualitySegments[quality]
			if !ok {
				abortWithError(c, http.StatusNotFound, ErrContentNotFound, "quality not found for this content")
				return
			}
			manifest := service.BuildMediaPlaylist(contentID, quality, segs, "{{PLAYBACK_TOKEN}}")
			rendered := strings.ReplaceAll(manifest, "{{PLAYBACK_TOKEN}}", playbackToken)
			c.Header("Content-Type", "application/vnd.apple.mpegurl")
			setCapabilityResponseHeaders(c)
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
		setCapabilityResponseHeaders(c)
		c.String(http.StatusOK, rendered)
	})
	log.Info("Streaming routes registered")
}

// serveStoredPlaylist serves the playlist FFmpeg produced, with only its URIs
// rewritten to the protected routes. It reports whether it handled the request;
// false means no stored playlist was available and the caller should fall back.
//
// The raw playlist never reaches the client: every URI in it pointed at an object
// key, so it is rewritten wholesale. A playlist that cannot be rewritten safely
// (an encrypted one, or an unknown tag carrying a URI) is refused rather than
// served in a form that leaks object paths or points at routes that do not exist.
func serveStoredPlaylist(
	c *gin.Context,
	objStorage service.SegmentStorage,
	bucket, contentID, quality, playbackToken string,
	playlists map[string]string,
	masterKey string,
) bool {
	if objStorage == nil {
		return false
	}
	ctx := c.Request.Context()

	write := func(body string) {
		c.Header("Content-Type", "application/vnd.apple.mpegurl")
		setCapabilityResponseHeaders(c)
		c.String(http.StatusOK, body)
	}

	// A specific quality: serve that media playlist.
	if quality != "" {
		key, ok := playlists[quality]
		if !ok {
			return false
		}
		raw, err := objStorage.Download(ctx, bucket, key)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError,
				"could not read the stored playlist", err.Error())
			return true
		}
		rewritten, err := streamingsvc.RewriteMediaPlaylistURIs(string(raw), contentID, quality, playbackToken)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError,
				"stored playlist could not be served safely", err.Error())
			return true
		}
		write(rewritten)
		return true
	}

	// No quality requested: serve the multivariant playlist when FFmpeg wrote one.
	if masterKey != "" {
		raw, err := objStorage.Download(ctx, bucket, masterKey)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError,
				"could not read the stored playlist", err.Error())
			return true
		}
		rewritten, err := streamingsvc.RewriteMasterPlaylistURIs(string(raw), contentID, playbackToken)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError,
				"stored playlist could not be served safely", err.Error())
			return true
		}
		write(rewritten)
		return true
	}

	// A single rendition with no multivariant playlist: serve it directly, which
	// is what a one-quality transcode produces.
	if len(playlists) == 1 {
		for q, key := range playlists {
			raw, err := objStorage.Download(ctx, bucket, key)
			if err != nil {
				abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError,
					"could not read the stored playlist", err.Error())
				return true
			}
			rewritten, err := streamingsvc.RewriteMediaPlaylistURIs(string(raw), contentID, q, playbackToken)
			if err != nil {
				abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError,
					"stored playlist could not be served safely", err.Error())
				return true
			}
			write(rewritten)
			return true
		}
	}
	return false
}

// setCapabilityResponseHeaders marks a response whose body carries a playback
// capability. Such a response is per-user and single-use-ish, so a shared cache
// must never store it, and the URL (which contains the capability) must not be
// forwarded to another origin in a Referer header.
func setCapabilityResponseHeaders(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
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
		if wallet == "" {
			// The wallet is set by PlaybackTokenAuth (from the validated token's
			// claims) or by JWTAuthMiddleware; without it there is nothing to
			// bind the token to, so refuse rather than skip the wallet check.
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "authentication required")
			return
		}
		claims, err := authService.ValidatePlaybackToken(c.Request.Context(), playbackToken, contentID, c.GetHeader("X-Client-Fingerprint"), wallet)
		if err != nil {
			middleware.GetLogger(c, log).Warn("playback token validation failed",
				zap.String("content_id", contentID),
				zap.Error(err))
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "invalid playback token")
			return
		}
		if rejectMismatchedJWTCapability(c, authService, wallet) {
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
			// The capability rides in this URL; never hand it to another origin
			// through the Referer header.
			c.Header("Referrer-Policy", "no-referrer")
			// Advertise range support on every segment response, including 200s,
			// so players know they may seek.
			c.Header("Accept-Ranges", "bytes")

			// A range request must be answered with a range. Serving 200 and the
			// whole segment ignores what the client asked for, breaks seeking and
			// forces a re-download of bytes the client already has.
			rangeHeader := strings.TrimSpace(c.GetHeader("Range"))
			if rangeHeader != "" {
				// The total size is needed for a correct Content-Range, so a
				// range request is buffered. Bounded: a segment larger than the
				// cap is a malformed object, not a reason to exhaust memory.
				data, readErr := io.ReadAll(io.LimitReader(best.rc, maxRangeableSegmentBytes+1))
				cancel()
				if readErr != nil || int64(len(data)) > maxRangeableSegmentBytes {
					middleware.GetLogger(c, log).Warn("segment too large to serve a range from",
						zap.String("content_id", contentID),
						zap.String("segment", segName),
						zap.Int("bytes", len(data)))
					abortWithError(c, http.StatusServiceUnavailable, ErrContentUnavailable, "segment unavailable")
					return
				}
				rangeStart, rangeEnd, ok := parseByteRange(rangeHeader, int64(len(data)))
				if !ok {
					c.Header("Content-Range", fmt.Sprintf("bytes */%d", len(data)))
					abortWithError(c, http.StatusRequestedRangeNotSatisfiable, ErrInvalidRequest, "unsatisfiable range")
					return
				}
				c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rangeStart, rangeEnd, len(data)))
				c.Header("Content-Length", strconv.FormatInt(rangeEnd-rangeStart+1, 10))
				c.Status(http.StatusPartialContent)
				if _, err := c.Writer.Write(data[rangeStart : rangeEnd+1]); err != nil {
					log.Warn("segment range write interrupted", zap.String("content_id", c.Param("id")), zap.Error(err))
				}
				monitoring.StreamingDownloadDuration.WithLabelValues("success").Observe(time.Since(start).Seconds())
				q := c.Query("quality")
				if q == "" {
					q = "default"
				}
				monitoring.StreamingSegmentsTotal.WithLabelValues(q).Inc()
				return
			}

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

// maxRangeableSegmentBytes caps how large a segment may be before a range
// request is refused. Answering a range needs the total size, so the object is
// buffered; this keeps a malformed or hostile object from exhausting memory.
// Real HLS segments are orders of magnitude smaller.
const maxRangeableSegmentBytes = 64 << 20

// parseByteRange parses a single-range HTTP Range header ("bytes=0-99",
// "bytes=100-", "bytes=-100") against a known size, returning an inclusive
// [start, end].
//
// Multi-range requests are rejected rather than answered with one of the
// ranges: replying 200/206 with the wrong bytes is worse than saying the request
// cannot be satisfied.
func parseByteRange(header string, size int64) (int64, int64, bool) {
	const prefix = "bytes="
	if !strings.HasPrefix(header, prefix) || size <= 0 {
		return 0, 0, false
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if spec == "" || strings.Contains(spec, ",") {
		return 0, 0, false
	}
	dash := strings.Index(spec, "-")
	if dash < 0 {
		return 0, 0, false
	}
	startStr := strings.TrimSpace(spec[:dash])
	endStr := strings.TrimSpace(spec[dash+1:])

	if startStr == "" {
		// Suffix form: the final N bytes.
		n, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true
	}

	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	end := size - 1
	if endStr != "" {
		end, err = strconv.ParseInt(endStr, 10, 64)
		if err != nil || end < start {
			return 0, 0, false
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, true
}

// rejectMismatchedJWTCapability refuses a request that presents BOTH an access
// JWT and a playback capability for DIFFERENT wallets.
//
// A segment request carries its capability in the query string, and the JWT
// middleware deliberately steps aside when a capability has already been
// validated. Without this check, a caller could pair their own JWT with someone
// else's capability and the mismatch would go unnoticed - the capability would
// be honoured while the request claims a different identity.
//
// Returns true when the request has been refused.
func rejectMismatchedJWTCapability(c *gin.Context, authService *service.AuthService, capabilityWallet string) bool {
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}
	presented := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	// On segment requests the header may legitimately carry the capability
	// itself rather than a JWT; that is the same credential, not a conflict.
	if presented == "" || presented == extractPlaybackToken(c) {
		return false
	}

	claims, err := authService.ParseToken(presented)
	if err != nil {
		// Not an access token. It was already ignored by the JWT middleware
		// because a capability was present, so there is nothing to reconcile.
		return false
	}
	if claims.WalletAddress == "" {
		return false
	}
	if !strings.EqualFold(claims.WalletAddress, capabilityWallet) {
		abortWithError(c, http.StatusUnauthorized, ErrUnauthorized,
			"the access token and the playback capability belong to different wallets")
		return true
	}
	return false
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
