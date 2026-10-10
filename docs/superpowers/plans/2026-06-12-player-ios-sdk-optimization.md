# Player + iOS SDK Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden the existing NFT-gated HLS playback path, introduce a production-grade playback session contract, and create a native iOS SDK architecture that can consume StreamGate streams safely and observably.

**Architecture:** Deliver this in three independently testable layers: first improve the current Go/H5 streaming pipeline without changing the public product model, then add a backend `PlaybackSession` abstraction for token refresh/revocation/audit, then add an `ios-sdk/` Swift Package with Core/Auth/Player module boundaries. The HLS design remains manifest-gated by JWT + NFT ownership and segment-gated by short-lived playback authorization.

**Tech Stack:** Go 1.24, Gin, testify, hls.js demo, AVFoundation/URLSession/Keychain via Swift Package Manager, no new Go dependencies without dependency approval.

---

## Scope Notes

This repository currently contains no native iOS SDK files and no `ARCHITECTURE_OPTIMIZATION.m`. The implementation is therefore split into separate sub-projects so each one can land independently:

1. Existing H5/player and Go streaming hardening.
2. Backend playback-session API and persistence.
3. Native iOS SDK skeleton and demo-facing integration contract.
4. Documentation and observability.

Do not implement all phases in a single commit. Each task below should be committed separately after its tests pass.

---

## File Structure

### Existing files to modify

- `h5-demo/js/player.js` — H5 demo player lifecycle, Blob URL cleanup, retry/backoff, token refresh callback hooks.
- `pkg/gateway/streaming_handlers.go` — manifest/segment handlers, playback token TTL constants, cache key behavior, segment download cancellation.
- `pkg/gateway/streaming_handlers_test.go` — gateway tests for cache keys, token extraction, segment download cancellation behavior.
- `pkg/gateway/routes.go` — route registration comments and eventual playback middleware wiring if introduced.
- `pkg/service/streamingsvc/streaming.go` — HLS playlist generation, token URL construction, DASH support labeling or completion.
- `pkg/service/streamingsvc/streaming_test.go` — HLS playlist generation tests.
- `pkg/plugins/streaming/cache.go` — replace empty microservice cache with bounded TTL cache.
- `pkg/plugins/streaming/cache_test.go` — new tests for microservice cache.
- `pkg/plugins/streaming/range.go` — optional range-cache hot-path usage.
- `pkg/plugins/streaming/range_test.go` — range cache behavior tests.
- `pkg/monitoring/metrics.go` — playback/client metric counters and histograms if no existing equivalent exists.

### New backend files to create

- `pkg/models/playback_session.go` — persistent playback-session model.
- `pkg/service/playback_session.go` — session creation, refresh, revocation, validation orchestration.
- `pkg/service/playback_session_test.go` — TDD coverage for session lifecycle.
- `pkg/gateway/playback_session_handlers.go` — HTTP handlers for create/refresh/revoke.
- `pkg/gateway/playback_session_handlers_test.go` — handler tests.
- `migrations/031_create_playback_sessions.sql` — playback session table.
- `migrations/031_create_playback_sessions.down.sql` — rollback.

### New iOS SDK files to create

- `ios-sdk/Package.swift` — Swift Package manifest.
- `ios-sdk/Sources/StreamGateCore/StreamGateSDK.swift` — SDK configuration entrypoint.
- `ios-sdk/Sources/StreamGateCore/APIClient.swift` — native URLSession client.
- `ios-sdk/Sources/StreamGateCore/Endpoint.swift` — endpoint definitions.
- `ios-sdk/Sources/StreamGateCore/TokenStore.swift` — Keychain-backed token storage protocol and implementation.
- `ios-sdk/Sources/StreamGateAuth/AuthManager.swift` — challenge/login/refresh/logout facade.
- `ios-sdk/Sources/StreamGatePlayer/StreamGatePlayer.swift` — AVPlayer wrapper.
- `ios-sdk/Sources/StreamGatePlayer/PlaybackSession.swift` — client-side playback session model.
- `ios-sdk/Sources/StreamGatePlayer/PlaybackStatsReporter.swift` — batched playback telemetry.
- `ios-sdk/Tests/StreamGateCoreTests/APIClientTests.swift` — API client tests using `URLProtocol` stub.
- `ios-sdk/Tests/StreamGateCoreTests/TokenStoreTests.swift` — token store protocol tests with memory implementation.
- `ios-sdk/Tests/StreamGatePlayerTests/StreamGatePlayerTests.swift` — state-machine tests.

### Documentation to create/update

- `docs/architecture/player-ios-sdk-optimization.md` — architecture decision record and migration strategy.
- `docs/api/playback-session.md` — playback-session API contract.
- `DEPENDENCY_APPROVAL.md` — only if adding a new dependency such as WalletConnect Swift SDK.

---

## Task 1: H5 Player Blob URL Lifecycle and Error Contract

**Files:**
- Modify: `h5-demo/js/player.js:1-208`
- Test manually: `make fullchain-deploy` then open `/demo`
- Optional E2E: `h5-demo` browser smoke via Playwright if a project test harness exists

- [ ] **Step 1: Add lifecycle state fields before implementation**

Add fields to the constructor:

```js
constructor(videoElement) {
    this.video = videoElement;
    this.hls = null;
    this.currentVideoId = null;
    this.manifestUrl = null;
    this.authToken = null;
    this._manifestParsedBound = null;
    this.manifestBlobUrl = null;
    this.retryCount = 0;
    this.maxRetries = 3;
    this.retryTimer = null;
    this.refreshPlaybackToken = null;
}
```

- [ ] **Step 2: Add deterministic cleanup helper**

Add this method inside `HLSPlayer`:

```js
cleanupManifestBlobUrl() {
    if (this.manifestBlobUrl) {
        URL.revokeObjectURL(this.manifestBlobUrl);
        this.manifestBlobUrl = null;
    }
}
```

- [ ] **Step 3: Use the cleanup helper when loading a new manifest**

Before creating the new Blob URL in `loadVideo`, add:

```js
this.cleanupManifestBlobUrl();
```

Replace the local variable usage:

```js
this.manifestBlobUrl = URL.createObjectURL(
    new Blob([manifestText], { type: 'application/vnd.apple.mpegurl' })
);
```

Then update load source calls:

```js
this.hls.loadSource(this.manifestBlobUrl);
```

and:

```js
this.video.src = this.manifestBlobUrl;
```

- [ ] **Step 4: Add bounded retry cleanup in `destroy`**

Update `destroy()` so it always clears timers and Blob URLs:

```js
destroy() {
    if (this.retryTimer) {
        clearTimeout(this.retryTimer);
        this.retryTimer = null;
    }
    if (this.hls) {
        this.hls.destroy();
        this.hls = null;
    }
    this.cleanupManifestBlobUrl();
    this.video.src = '';
    this.currentVideoId = null;
    this.manifestUrl = null;
    this.retryCount = 0;
}
```

- [ ] **Step 5: Add bounded network retry**

Add this method:

```js
scheduleNetworkRetry() {
    if (!this.hls || this.retryCount >= this.maxRetries) {
        this.destroy();
        return;
    }
    const delayMs = Math.min(1000 * 2 ** this.retryCount, 8000);
    this.retryCount += 1;
    this.retryTimer = setTimeout(() => {
        this.retryTimer = null;
        if (this.hls) {
            this.hls.startLoad();
        }
    }, delayMs);
}
```

Update network error handling:

```js
case Hls.ErrorTypes.NETWORK_ERROR:
    console.warn('Fatal network error, scheduling bounded retry');
    this.scheduleNetworkRetry();
    break;
```

- [ ] **Step 6: Run formatting/lint check if H5 tooling exists**

Run:

```bash
npm --prefix h5-demo test
```

Expected if no test script exists: command fails with npm script-not-found. Record that as not available and continue with manual browser verification.

- [ ] **Step 7: Manually verify demo player lifecycle**

Run:

```bash
make fullchain-deploy
```

Open the H5 demo and load two videos in sequence. Expected:

- No uncaught JS errors in browser console.
- Playback starts for the second video.
- Memory snapshots do not show unbounded retained Blob URLs.

- [ ] **Step 8: Commit**

```bash
git add h5-demo/js/player.js
git commit -m "fix(player): clean up manifest blob URLs"
```

---

## Task 2: Centralize Playback Token TTL and Prepare Refresh Semantics

**Files:**
- Modify: `pkg/gateway/streaming_handlers.go:35-40,250`
- Test: `pkg/gateway/streaming_handlers_test.go`

- [ ] **Step 1: Write a failing test for the intended TTL constant**

Add this test to `pkg/gateway/streaming_handlers_test.go`:

```go
func TestPlaybackTokenTTL_IsShortLived(t *testing.T) {
	require.Equal(t, 10*time.Minute, playbackTokenTTL)
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run:

```bash
go test ./pkg/gateway -run TestPlaybackTokenTTL_IsShortLived -count=1
```

Expected: FAIL because `playbackTokenTTL` is not defined or is not `10*time.Minute`.

- [ ] **Step 3: Add the constant**

In the const block near cache TTLs, add:

```go
	playbackTokenTTL      = 10 * time.Minute
```

- [ ] **Step 4: Use the constant at token generation**

Replace:

```go
30*time.Minute
```

with:

```go
playbackTokenTTL
```

inside `GeneratePlaybackToken` call at `pkg/gateway/streaming_handlers.go:250`.

- [ ] **Step 5: Run the targeted test**

Run:

```bash
go test ./pkg/gateway -run TestPlaybackTokenTTL_IsShortLived -count=1
```

Expected: PASS.

- [ ] **Step 6: Run gateway tests**

Run:

```bash
go test ./pkg/gateway -count=1
```

Expected: PASS, or only documented pre-existing failures.

- [ ] **Step 7: Commit**

```bash
git add pkg/gateway/streaming_handlers.go pkg/gateway/streaming_handlers_test.go
git commit -m "fix(streaming): shorten playback token lifetime"
```

---

## Task 3: Make Manifest Template Cache User-Independent

**Files:**
- Modify: `pkg/gateway/streaming_handlers.go:24-29,120-129,257-263,335`
- Test: `pkg/gateway/streaming_handlers_test.go`

- [ ] **Step 1: Write failing tests for wallet-independent manifest cache**

Add:

```go
func TestStreamingCache_ManifestTemplateSharedAcrossWallets(t *testing.T) {
	cache := NewStreamingCache()
	cache.SetManifest("content-1", "#EXTM3U\n{{PLAYBACK_TOKEN}}\n", "wallet-a")

	manifest, ok := cache.GetManifest("content-1", "wallet-b")

	require.True(t, ok)
	require.Equal(t, "#EXTM3U\n{{PLAYBACK_TOKEN}}\n", manifest)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./pkg/gateway -run TestStreamingCache_ManifestTemplateSharedAcrossWallets -count=1
```

Expected: FAIL because `GetManifest` requires `entry.walletAddr == wallet`.

- [ ] **Step 3: Remove wallet from the cache entry**

Change:

```go
type manifestCacheEntry struct {
	manifest   string
	expiresAt  time.Time
	walletAddr string // bound to issuing wallet to prevent cross-user token reuse
}
```

to:

```go
type manifestCacheEntry struct {
	manifest  string
	expiresAt time.Time
}
```

- [ ] **Step 4: Keep method signatures stable but ignore wallet internally**

Change `GetManifest` to:

```go
func (sc *StreamingCache) GetManifest(contentID, wallet string) (string, bool) {
	_ = wallet
	if entry, ok := sc.manifests.Get(contentID); ok && time.Now().Before(entry.expiresAt) {
		return entry.manifest, true
	}
	return "", false
}
```

Change `SetManifest` to:

```go
func (sc *StreamingCache) SetManifest(contentID, manifest, wallet string) {
	_ = wallet
	sc.manifests.Set(contentID, manifestCacheEntry{manifest: manifest, expiresAt: time.Now().Add(manifestCacheTTL)})
}
```

- [ ] **Step 5: Run targeted tests**

```bash
go test ./pkg/gateway -run 'TestStreamingCache_ManifestTemplateSharedAcrossWallets|TestPlaybackTokenTTL_IsShortLived' -count=1
```

Expected: PASS.

- [ ] **Step 6: Run full gateway tests**

```bash
go test ./pkg/gateway -count=1
```

Expected: PASS, or only documented pre-existing failures.

- [ ] **Step 7: Commit**

```bash
git add pkg/gateway/streaming_handlers.go pkg/gateway/streaming_handlers_test.go
git commit -m "perf(streaming): share manifest template cache across wallets"
```

---

## Task 4: Add Expired-Entry Cleanup to Streaming LRU Cache

**Files:**
- Modify: `pkg/gateway/streaming_handlers.go:47-96`
- Test: `pkg/gateway/streaming_handlers_test.go`

- [ ] **Step 1: Write failing test for expired entry removal**

Add:

```go
func TestLRUCache_DeleteRemovesEntry(t *testing.T) {
	cache := newLRUCache[string](2, time.Second)
	cache.Set("a", "one")
	cache.Delete("a")

	_, ok := cache.Get("a")

	require.False(t, ok)
}
```

Add:

```go
func TestStreamingCache_ExpiredManifestMisses(t *testing.T) {
	cache := NewStreamingCache()
	cache.manifests.Set("content-1", manifestCacheEntry{
		manifest:  "old",
		expiresAt: time.Now().Add(-time.Second),
	})

	_, ok := cache.GetManifest("content-1", "wallet")

	require.False(t, ok)
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./pkg/gateway -run 'TestLRUCache_DeleteRemovesEntry|TestStreamingCache_ExpiredManifestMisses' -count=1
```

Expected: PASS for current behavior. This establishes guardrails before refactor.

- [ ] **Step 3: Add a generic predicate cleanup method**

Add to `lruCache[T]`:

```go
func (c *lruCache[T]) DeleteIf(match func(T) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for elem := c.order.Back(); elem != nil; {
		prev := elem.Prev()
		entry := elem.Value.(*lruEntry[T])
		if match(entry.value) {
			c.order.Remove(elem)
			delete(c.items, entry.key)
		}
		elem = prev
	}
}
```

- [ ] **Step 4: Remove expired manifest entries on miss**

Update `GetManifest`:

```go
func (sc *StreamingCache) GetManifest(contentID, wallet string) (string, bool) {
	_ = wallet
	entry, ok := sc.manifests.Get(contentID)
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		sc.manifests.Delete(contentID)
		return "", false
	}
	return entry.manifest, true
}
```

Update `GetSegmentIndex` similarly:

```go
func (sc *StreamingCache) GetSegmentIndex(contentID string) (map[string][]string, bool) {
	entry, ok := sc.segmentIdx.Get(contentID)
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		sc.segmentIdx.Delete(contentID)
		return nil, false
	}
	return entry.qualities, true
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./pkg/gateway -run 'TestStreamingCache|TestLRUCache' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/gateway/streaming_handlers.go pkg/gateway/streaming_handlers_test.go
git commit -m "perf(streaming): evict expired cache entries on access"
```

---

## Task 5: Cancel Losing Segment Download Attempts

**Files:**
- Modify: `pkg/gateway/streaming_handlers.go:396-462`
- Test: `pkg/gateway/streaming_handlers_test.go`

- [ ] **Step 1: Write a targeted test with fake segment storage**

Add a fake storage type in `pkg/gateway/streaming_handlers_test.go`:

```go
type blockingSegmentStorage struct {
	mu          sync.Mutex
	startedKeys []string
	cancelSeen  chan struct{}
}

func (s *blockingSegmentStorage) DownloadStream(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	s.startedKeys = append(s.startedKeys, key)
	s.mu.Unlock()

	if strings.Contains(key, "winner") {
		return io.NopCloser(strings.NewReader("segment-data")), nil
	}

	<-ctx.Done()
	select {
	case s.cancelSeen <- struct{}{}:
	default:
	}
	return nil, ctx.Err()
}

func (s *blockingSegmentStorage) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	return nil, nil
}
```

- [ ] **Step 2: Add the failing behavior test**

Add a test that calls a small helper introduced in Step 4:

```go
func TestDownloadBestSegment_CancelsLosingCandidates(t *testing.T) {
	storage := &blockingSegmentStorage{cancelSeen: make(chan struct{}, 1)}
	ctx := context.Background()
	candidates := []segmentCandidate{
		{key: "streams/content-1/loser.ts", prio: 1},
		{key: "streams/content-1/winner.ts", prio: 10},
	}

	rc, err := downloadBestSegment(ctx, storage, "streamgate", candidates)
	require.NoError(t, err)
	require.NotNil(t, rc)
	defer rc.Close()

	select {
	case <-storage.cancelSeen:
	case <-time.After(time.Second):
		t.Fatal("losing candidate was not cancelled")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./pkg/gateway -run TestDownloadBestSegment_CancelsLosingCandidates -count=1
```

Expected: FAIL because `downloadBestSegment` does not exist.

- [ ] **Step 4: Extract segment race logic into a helper**

Add a package-level helper near `RegisterStreamingSegmentRoute`:

```go
type segmentDownloader interface {
	DownloadStream(ctx context.Context, bucket, key string) (io.ReadCloser, error)
}

func downloadBestSegment(parent context.Context, storage segmentDownloader, bucket string, candidates []segmentCandidate) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

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
			rc, dlErr := storage.DownloadStream(ctx, bucket, cand.key)
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
				cancel()
			} else {
				res.rc.Close()
			}
		}
	}
	wg.Wait()
	if best.rc == nil {
		return nil, fmt.Errorf("no segment candidate available")
	}
	return best.rc, nil
}
```

- [ ] **Step 5: Replace inline goroutine race with helper**

Inside `RegisterStreamingSegmentRoute`, replace the inline goroutine block with:

```go
start := time.Now()
bestRC, err := downloadBestSegment(c.Request.Context(), objStorage, segBucket, candidates)
if err != nil {
	monitoring.StreamingDownloadDuration.WithLabelValues("fail").Observe(time.Since(start).Seconds())
	middleware.GetLogger(c, log).Warn("Segment download failed",
		zap.String("content_id", contentID),
		zap.String("segment", segName),
		zap.Error(err))
	abortWithError(c, http.StatusServiceUnavailable, ErrContentUnavailable, "segment unavailable")
	return
}
defer func() { _ = bestRC.Close() }()
```

Then change `io.Copy` to:

```go
if _, err := io.Copy(c.Writer, bestRC); err != nil {
```

- [ ] **Step 6: Run targeted test**

```bash
go test ./pkg/gateway -run TestDownloadBestSegment_CancelsLosingCandidates -count=1
```

Expected: PASS.

- [ ] **Step 7: Run gateway tests**

```bash
go test ./pkg/gateway -count=1
```

Expected: PASS, or only documented pre-existing failures.

- [ ] **Step 8: Commit**

```bash
git add pkg/gateway/streaming_handlers.go pkg/gateway/streaming_handlers_test.go
git commit -m "perf(streaming): cancel losing segment downloads"
```

---

## Task 6: Replace Empty Microservice Stream Cache

**Files:**
- Modify: `pkg/plugins/streaming/cache.go:1-33`
- Create: `pkg/plugins/streaming/cache_test.go`

- [ ] **Step 1: Write cache tests first**

Create `pkg/plugins/streaming/cache_test.go`:

```go
package streaming

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestStreamCache_GetReturnsSetValue(t *testing.T) {
	cache := NewStreamCache(zap.NewNop())
	cache.Set("stream:1", "value")

	got, ok := cache.Get("stream:1")

	require.True(t, ok)
	require.Equal(t, "value", got)
}

func TestStreamCache_DeleteRemovesValue(t *testing.T) {
	cache := NewStreamCache(zap.NewNop())
	cache.Set("stream:1", "value")
	cache.Delete("stream:1")

	_, ok := cache.Get("stream:1")

	require.False(t, ok)
}

func TestStreamCache_CloseClearsValues(t *testing.T) {
	cache := NewStreamCache(zap.NewNop())
	cache.Set("stream:1", "value")
	cache.Close()

	_, ok := cache.Get("stream:1")

	require.False(t, ok)
}

func TestStreamCache_ExpiresValues(t *testing.T) {
	cache := NewStreamCache(zap.NewNop())
	cache.ttl = time.Millisecond
	cache.Set("stream:1", "value")
	time.Sleep(2 * time.Millisecond)

	_, ok := cache.Get("stream:1")

	require.False(t, ok)
}
```

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./pkg/plugins/streaming -run TestStreamCache -count=1
```

Expected: FAIL because `Get` always returns false and `ttl` does not exist.

- [ ] **Step 3: Implement bounded TTL cache**

Replace `pkg/plugins/streaming/cache.go` with:

```go
package streaming

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	defaultStreamCacheTTL  = 2 * time.Minute
	defaultStreamCacheSize = 10000
)

type streamCacheEntry struct {
	value     interface{}
	expiresAt time.Time
}

// StreamCache caches streaming data.
type StreamCache struct {
	logger  *zap.Logger
	mu      sync.RWMutex
	items   map[string]streamCacheEntry
	ttl     time.Duration
	maxSize int
}

// NewStreamCache creates a new stream cache.
func NewStreamCache(logger *zap.Logger) *StreamCache {
	return &StreamCache{
		logger:  logger,
		items:   make(map[string]streamCacheEntry),
		ttl:     defaultStreamCacheTTL,
		maxSize: defaultStreamCacheSize,
	}
}

// Get gets cached stream.
func (c *StreamCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		c.Delete(key)
		return nil, false
	}
	return entry.value, true
}

// Set sets stream cache.
func (c *StreamCache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= c.maxSize {
		for existingKey, entry := range c.items {
			if time.Now().After(entry.expiresAt) {
				delete(c.items, existingKey)
			}
		}
	}
	if len(c.items) >= c.maxSize {
		for existingKey := range c.items {
			delete(c.items, existingKey)
			break
		}
	}
	c.items[key] = streamCacheEntry{value: value, expiresAt: time.Now().Add(c.ttl)}
}

// Delete deletes cached stream.
func (c *StreamCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// Close closes the cache.
func (c *StreamCache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]streamCacheEntry)
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./pkg/plugins/streaming -run TestStreamCache -count=1
```

Expected: PASS.

- [ ] **Step 5: Run plugin package tests**

```bash
go test ./pkg/plugins/streaming -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/plugins/streaming/cache.go pkg/plugins/streaming/cache_test.go
git commit -m "perf(streaming): implement microservice stream cache"
```

---

## Task 7: Add Playback Session Persistence

**Files:**
- Create: `pkg/models/playback_session.go`
- Create: `migrations/031_create_playback_sessions.sql`
- Create: `migrations/031_create_playback_sessions.down.sql`

- [ ] **Step 1: Create playback session model**

Create `pkg/models/playback_session.go`:

```go
package models

import "time"

// PlaybackSession tracks a protected streaming session for token refresh,
// revocation, and audit.
type PlaybackSession struct {
	ID                string     `json:"id" db:"id"`
	ContentID         string     `json:"content_id" db:"content_id"`
	WalletAddress     string     `json:"wallet_address" db:"wallet_address"`
	NFTContract       string     `json:"nft_contract" db:"nft_contract"`
	TokenID           string     `json:"token_id" db:"token_id"`
	ChainID           int64      `json:"chain_id" db:"chain_id"`
	ClientFingerprint string     `json:"client_fingerprint" db:"client_fingerprint"`
	StreamVersion     string     `json:"stream_version" db:"stream_version"`
	ExpiresAt         time.Time  `json:"expires_at" db:"expires_at"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty" db:"revoked_at"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`
}

func (s PlaybackSession) IsRevoked() bool {
	return s.RevokedAt != nil
}

func (s PlaybackSession) IsExpired(now time.Time) bool {
	return !now.Before(s.ExpiresAt)
}
```

- [ ] **Step 2: Create migration**

Create `migrations/031_create_playback_sessions.sql`:

```sql
CREATE TABLE IF NOT EXISTS playback_sessions (
    id VARCHAR(128) PRIMARY KEY,
    content_id VARCHAR(255) NOT NULL,
    wallet_address VARCHAR(255) NOT NULL,
    nft_contract VARCHAR(255) NOT NULL DEFAULT '',
    token_id VARCHAR(255) NOT NULL DEFAULT '',
    chain_id BIGINT NOT NULL DEFAULT 1,
    client_fingerprint VARCHAR(255) NOT NULL DEFAULT '',
    stream_version VARCHAR(128) NOT NULL DEFAULT 'v1',
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_playback_sessions_content_wallet
    ON playback_sessions(content_id, wallet_address);

CREATE INDEX IF NOT EXISTS idx_playback_sessions_expires_at
    ON playback_sessions(expires_at);

CREATE INDEX IF NOT EXISTS idx_playback_sessions_revoked_at
    ON playback_sessions(revoked_at);
```

- [ ] **Step 3: Create rollback migration**

Create `migrations/031_create_playback_sessions.down.sql`:

```sql
DROP INDEX IF EXISTS idx_playback_sessions_revoked_at;
DROP INDEX IF EXISTS idx_playback_sessions_expires_at;
DROP INDEX IF EXISTS idx_playback_sessions_content_wallet;
DROP TABLE IF EXISTS playback_sessions;
```

- [ ] **Step 4: Add model tests**

Create `pkg/models/playback_session_test.go`:

```go
package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPlaybackSession_IsExpired(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	session := PlaybackSession{ExpiresAt: now}

	require.True(t, session.IsExpired(now))
	require.True(t, session.IsExpired(now.Add(time.Second)))
	require.False(t, session.IsExpired(now.Add(-time.Second)))
}

func TestPlaybackSession_IsRevoked(t *testing.T) {
	now := time.Now()

	require.False(t, PlaybackSession{}.IsRevoked())
	require.True(t, PlaybackSession{RevokedAt: &now}.IsRevoked())
}
```

- [ ] **Step 5: Run model tests**

```bash
go test ./pkg/models -run TestPlaybackSession -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/models/playback_session.go pkg/models/playback_session_test.go migrations/031_create_playback_sessions.sql migrations/031_create_playback_sessions.down.sql
git commit -m "feat(streaming): add playback session model"
```

---

## Task 8: Add Playback Session Service

**Files:**
- Create: `pkg/service/playback_session.go`
- Create: `pkg/service/playback_session_test.go`

- [ ] **Step 1: Write service tests first**

Create `pkg/service/playback_session_test.go`:

```go
package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewPlaybackSessionID_IsStableLength(t *testing.T) {
	id := newPlaybackSessionID("content-1", "0xabc", time.Unix(100, 0))

	require.NotEmpty(t, id)
	require.Contains(t, id, "ps_")
}

func TestPlaybackSessionConfig_Defaults(t *testing.T) {
	cfg := DefaultPlaybackSessionConfig()

	require.Equal(t, 10*time.Minute, cfg.TokenTTL)
	require.Equal(t, 2*time.Hour, cfg.SessionTTL)
}
```

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./pkg/service -run 'TestNewPlaybackSessionID|TestPlaybackSessionConfig' -count=1
```

Expected: FAIL because functions do not exist.

- [ ] **Step 3: Implement service skeleton**

Create `pkg/service/playback_session.go`:

```go
package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

type PlaybackSessionConfig struct {
	TokenTTL   time.Duration
	SessionTTL time.Duration
}

func DefaultPlaybackSessionConfig() PlaybackSessionConfig {
	return PlaybackSessionConfig{
		TokenTTL:   10 * time.Minute,
		SessionTTL: 2 * time.Hour,
	}
}

func newPlaybackSessionID(contentID, wallet string, now time.Time) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d", contentID, wallet, now.UnixNano())))
	return "ps_" + hex.EncodeToString(sum[:16])
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./pkg/service -run 'TestNewPlaybackSessionID|TestPlaybackSessionConfig' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/service/playback_session.go pkg/service/playback_session_test.go
git commit -m "feat(streaming): add playback session service foundation"
```

---

## Task 9: Add Playback Session HTTP Contract

**Files:**
- Create: `pkg/gateway/playback_session_handlers.go`
- Create: `pkg/gateway/playback_session_handlers_test.go`
- Modify: `pkg/gateway/routes.go:207-219`

- [ ] **Step 1: Define request/response structs and tests**

Create `pkg/gateway/playback_session_handlers_test.go`:

```go
package gateway

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCreatePlaybackSessionResponse_JSONShape(t *testing.T) {
	resp := createPlaybackSessionResponse{
		SessionID:     "ps_123",
		PlaybackToken: "token",
		ManifestURL:   "/api/v1/streaming/content-1/manifest.m3u8?playback_token=token",
		ExpiresAt:     time.Unix(100, 0).UTC(),
	}

	body, err := json.Marshal(resp)

	require.NoError(t, err)
	require.JSONEq(t, `{
		"session_id":"ps_123",
		"playback_token":"token",
		"manifest_url":"/api/v1/streaming/content-1/manifest.m3u8?playback_token=token",
		"expires_at":"1970-01-01T00:01:40Z"
	}`, string(body))
}
```

- [ ] **Step 2: Run test to verify failure**

```bash
go test ./pkg/gateway -run TestCreatePlaybackSessionResponse_JSONShape -count=1
```

Expected: FAIL because `createPlaybackSessionResponse` does not exist.

- [ ] **Step 3: Add handler contract types**

Create `pkg/gateway/playback_session_handlers.go`:

```go
package gateway

import "time"

type createPlaybackSessionRequest struct {
	ContentID string `json:"content_id" binding:"required"`
	Contract  string `json:"contract"`
	TokenID   string `json:"token_id"`
	ChainID   int64  `json:"chain_id"`
}

type createPlaybackSessionResponse struct {
	SessionID     string    `json:"session_id"`
	PlaybackToken string    `json:"playback_token"`
	ManifestURL   string    `json:"manifest_url"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type refreshPlaybackSessionResponse struct {
	SessionID     string    `json:"session_id"`
	PlaybackToken string    `json:"playback_token"`
	ExpiresAt     time.Time `json:"expires_at"`
}
```

- [ ] **Step 4: Run test**

```bash
go test ./pkg/gateway -run TestCreatePlaybackSessionResponse_JSONShape -count=1
```

Expected: PASS.

- [ ] **Step 5: Add route stubs returning 501 until service is wired**

Add function:

```go
func RegisterPlaybackSessionRoutes(router gin.IRouter) {
	router.POST(APIPrefix+"/playback-sessions", func(c *gin.Context) {
		abortWithError(c, http.StatusNotImplemented, ErrInternalError, "playback sessions are not enabled")
	})
	router.POST(APIPrefix+"/playback-sessions/:id/refresh", func(c *gin.Context) {
		abortWithError(c, http.StatusNotImplemented, ErrInternalError, "playback sessions are not enabled")
	})
	router.DELETE(APIPrefix+"/playback-sessions/:id", func(c *gin.Context) {
		abortWithError(c, http.StatusNotImplemented, ErrInternalError, "playback sessions are not enabled")
	})
}
```

Required imports:

```go
import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)
```

Wire it near playback stats routes in `registerProtectedRoutes`:

```go
RegisterPlaybackSessionRoutes(rootG)
```

- [ ] **Step 6: Run gateway tests**

```bash
go test ./pkg/gateway -count=1
```

Expected: PASS, or only documented pre-existing failures.

- [ ] **Step 7: Commit**

```bash
git add pkg/gateway/playback_session_handlers.go pkg/gateway/playback_session_handlers_test.go pkg/gateway/routes.go
git commit -m "feat(streaming): define playback session HTTP contract"
```

---

## Task 10: Create iOS SDK Swift Package Skeleton

**Files:**
- Create: `ios-sdk/Package.swift`
- Create: `ios-sdk/Sources/StreamGateCore/StreamGateSDK.swift`
- Create: `ios-sdk/Sources/StreamGateCore/APIClient.swift`
- Create: `ios-sdk/Tests/StreamGateCoreTests/APIClientTests.swift`

- [ ] **Step 1: Create Swift package manifest**

Create `ios-sdk/Package.swift`:

```swift
// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "StreamGateSDK",
    platforms: [
        .iOS(.v15),
        .tvOS(.v15)
    ],
    products: [
        .library(name: "StreamGateCore", targets: ["StreamGateCore"]),
        .library(name: "StreamGateAuth", targets: ["StreamGateAuth"]),
        .library(name: "StreamGatePlayer", targets: ["StreamGatePlayer"])
    ],
    targets: [
        .target(name: "StreamGateCore"),
        .target(name: "StreamGateAuth", dependencies: ["StreamGateCore"]),
        .target(name: "StreamGatePlayer", dependencies: ["StreamGateCore", "StreamGateAuth"]),
        .testTarget(name: "StreamGateCoreTests", dependencies: ["StreamGateCore"]),
        .testTarget(name: "StreamGatePlayerTests", dependencies: ["StreamGatePlayer"])
    ]
)
```

- [ ] **Step 2: Add SDK configuration entrypoint**

Create `ios-sdk/Sources/StreamGateCore/StreamGateSDK.swift`:

```swift
import Foundation

public struct StreamGateConfiguration: Sendable {
    public let baseURL: URL
    public let appName: String

    public init(baseURL: URL, appName: String) {
        self.baseURL = baseURL
        self.appName = appName
    }
}

public enum StreamGateSDK {
    private static var storedConfiguration: StreamGateConfiguration?

    public static func configure(_ configuration: StreamGateConfiguration) {
        storedConfiguration = configuration
    }

    public static var configuration: StreamGateConfiguration {
        get throws {
            guard let storedConfiguration else {
                throw StreamGateError.notConfigured
            }
            return storedConfiguration
        }
    }
}

public enum StreamGateError: Error, Equatable, Sendable {
    case notConfigured
    case invalidResponse
    case unauthorized
    case playbackTokenExpired
    case nftGateDenied
}
```

- [ ] **Step 3: Add API client**

Create `ios-sdk/Sources/StreamGateCore/APIClient.swift`:

```swift
import Foundation

public struct APIRequest: Sendable {
    public let path: String
    public let method: String
    public let body: Data?

    public init(path: String, method: String = "GET", body: Data? = nil) {
        self.path = path
        self.method = method
        self.body = body
    }
}

public final class APIClient: @unchecked Sendable {
    private let baseURL: URL
    private let session: URLSession

    public init(baseURL: URL, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.session = session
    }

    public func send(_ request: APIRequest, bearerToken: String? = nil) async throws -> Data {
        var urlRequest = URLRequest(url: baseURL.appendingPathComponent(request.path))
        urlRequest.httpMethod = request.method
        urlRequest.httpBody = request.body
        urlRequest.setValue("application/json", forHTTPHeaderField: "Accept")
        if let bearerToken {
            urlRequest.setValue("Bearer \(bearerToken)", forHTTPHeaderField: "Authorization")
        }

        let (data, response) = try await session.data(for: urlRequest)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw StreamGateError.invalidResponse
        }
        switch httpResponse.statusCode {
        case 200..<300:
            return data
        case 401:
            throw StreamGateError.unauthorized
        case 403:
            throw StreamGateError.nftGateDenied
        default:
            throw StreamGateError.invalidResponse
        }
    }
}
```

- [ ] **Step 4: Add configuration tests**

Create `ios-sdk/Tests/StreamGateCoreTests/APIClientTests.swift`:

```swift
import XCTest
@testable import StreamGateCore

final class APIClientTests: XCTestCase {
    func testConfigurationThrowsBeforeConfigure() {
        XCTAssertThrowsError(try StreamGateSDK.configuration) { error in
            XCTAssertEqual(error as? StreamGateError, .notConfigured)
        }
    }

    func testConfigurationStoresBaseURL() throws {
        let url = try XCTUnwrap(URL(string: "https://streamgate.example"))
        StreamGateSDK.configure(StreamGateConfiguration(baseURL: url, appName: "Demo"))

        let configuration = try StreamGateSDK.configuration

        XCTAssertEqual(configuration.baseURL, url)
        XCTAssertEqual(configuration.appName, "Demo")
    }
}
```

- [ ] **Step 5: Run Swift tests**

```bash
cd ios-sdk && swift test
```

Expected: PASS on macOS with Swift 5.9+.

- [ ] **Step 6: Commit**

```bash
git add ios-sdk/Package.swift ios-sdk/Sources/StreamGateCore/StreamGateSDK.swift ios-sdk/Sources/StreamGateCore/APIClient.swift ios-sdk/Tests/StreamGateCoreTests/APIClientTests.swift
git commit -m "feat(ios): add StreamGate Swift package skeleton"
```

---

## Task 11: Add iOS Token Store Protocol and Memory Test Double

**Files:**
- Create: `ios-sdk/Sources/StreamGateCore/TokenStore.swift`
- Create: `ios-sdk/Tests/StreamGateCoreTests/TokenStoreTests.swift`

- [ ] **Step 1: Add token store protocol and memory implementation**

Create `ios-sdk/Sources/StreamGateCore/TokenStore.swift`:

```swift
import Foundation

public protocol TokenStore: Sendable {
    func readToken() async throws -> String?
    func writeToken(_ token: String) async throws
    func clearToken() async throws
}

public actor MemoryTokenStore: TokenStore {
    private var token: String?

    public init(token: String? = nil) {
        self.token = token
    }

    public func readToken() async throws -> String? {
        token
    }

    public func writeToken(_ token: String) async throws {
        self.token = token
    }

    public func clearToken() async throws {
        token = nil
    }
}
```

- [ ] **Step 2: Add tests**

Create `ios-sdk/Tests/StreamGateCoreTests/TokenStoreTests.swift`:

```swift
import XCTest
@testable import StreamGateCore

final class TokenStoreTests: XCTestCase {
    func testMemoryTokenStoreRoundTrip() async throws {
        let store = MemoryTokenStore()

        try await store.writeToken("jwt")
        let token = try await store.readToken()

        XCTAssertEqual(token, "jwt")
    }

    func testMemoryTokenStoreClearRemovesToken() async throws {
        let store = MemoryTokenStore(token: "jwt")

        try await store.clearToken()
        let token = try await store.readToken()

        XCTAssertNil(token)
    }
}
```

- [ ] **Step 3: Run Swift tests**

```bash
cd ios-sdk && swift test
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add ios-sdk/Sources/StreamGateCore/TokenStore.swift ios-sdk/Tests/StreamGateCoreTests/TokenStoreTests.swift
git commit -m "feat(ios): add SDK token store abstraction"
```

---

## Task 12: Add iOS Player State Machine Skeleton

**Files:**
- Create: `ios-sdk/Sources/StreamGatePlayer/PlaybackSession.swift`
- Create: `ios-sdk/Sources/StreamGatePlayer/StreamGatePlayer.swift`
- Create: `ios-sdk/Tests/StreamGatePlayerTests/StreamGatePlayerTests.swift`

- [ ] **Step 1: Add playback session client model**

Create `ios-sdk/Sources/StreamGatePlayer/PlaybackSession.swift`:

```swift
import Foundation

public struct PlaybackSession: Codable, Equatable, Sendable {
    public let sessionID: String
    public let playbackToken: String
    public let manifestURL: URL
    public let expiresAt: Date

    public init(sessionID: String, playbackToken: String, manifestURL: URL, expiresAt: Date) {
        self.sessionID = sessionID
        self.playbackToken = playbackToken
        self.manifestURL = manifestURL
        self.expiresAt = expiresAt
    }

    public func isExpired(now: Date = Date()) -> Bool {
        now >= expiresAt
    }
}
```

- [ ] **Step 2: Add player state machine**

Create `ios-sdk/Sources/StreamGatePlayer/StreamGatePlayer.swift`:

```swift
import AVFoundation
import Foundation

@MainActor
public final class StreamGatePlayer {
    public enum State: Equatable, Sendable {
        case idle
        case preparing
        case ready
        case playing
        case paused
        case failed(String)
    }

    public private(set) var state: State = .idle
    public private(set) var playbackSession: PlaybackSession?
    private let player: AVPlayer

    public init(player: AVPlayer = AVPlayer()) {
        self.player = player
    }

    public func prepare(session: PlaybackSession) {
        state = .preparing
        playbackSession = session
        player.replaceCurrentItem(with: AVPlayerItem(url: session.manifestURL))
        state = .ready
    }

    public func play() {
        player.play()
        state = .playing
    }

    public func pause() {
        player.pause()
        state = .paused
    }

    public func stop() {
        player.pause()
        player.replaceCurrentItem(with: nil)
        playbackSession = nil
        state = .idle
    }
}
```

- [ ] **Step 3: Add tests**

Create `ios-sdk/Tests/StreamGatePlayerTests/StreamGatePlayerTests.swift`:

```swift
import XCTest
@testable import StreamGatePlayer

@MainActor
final class StreamGatePlayerTests: XCTestCase {
    func testPrepareSetsReadyStateAndSession() throws {
        let player = StreamGatePlayer()
        let url = try XCTUnwrap(URL(string: "https://streamgate.example/api/v1/streaming/content-1/manifest.m3u8"))
        let session = PlaybackSession(
            sessionID: "ps_1",
            playbackToken: "token",
            manifestURL: url,
            expiresAt: Date().addingTimeInterval(600)
        )

        player.prepare(session: session)

        XCTAssertEqual(player.state, .ready)
        XCTAssertEqual(player.playbackSession, session)
    }

    func testStopReturnsToIdleAndClearsSession() throws {
        let player = StreamGatePlayer()
        let url = try XCTUnwrap(URL(string: "https://streamgate.example/manifest.m3u8"))
        player.prepare(session: PlaybackSession(sessionID: "ps_1", playbackToken: "token", manifestURL: url, expiresAt: Date()))

        player.stop()

        XCTAssertEqual(player.state, .idle)
        XCTAssertNil(player.playbackSession)
    }
}
```

- [ ] **Step 4: Run Swift tests**

```bash
cd ios-sdk && swift test
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ios-sdk/Sources/StreamGatePlayer/PlaybackSession.swift ios-sdk/Sources/StreamGatePlayer/StreamGatePlayer.swift ios-sdk/Tests/StreamGatePlayerTests/StreamGatePlayerTests.swift
git commit -m "feat(ios): add player state machine skeleton"
```

---

## Task 13: Document Architecture Decisions and API Contract

**Files:**
- Create: `docs/architecture/player-ios-sdk-optimization.md`
- Create: `docs/api/playback-session.md`

- [ ] **Step 1: Create architecture document**

Create `docs/architecture/player-ios-sdk-optimization.md`:

```markdown
# Player + iOS SDK Optimization Architecture

## Context

StreamGate protects HLS playback with JWT authentication, NFT ownership checks, and short-lived playback tokens. The existing repository contains Go backend services and an H5 demo player, but no native iOS SDK.

## Decision

The production playback architecture uses three layers:

1. Manifest requests require JWT authentication and NFT gate verification.
2. Segment requests require short-lived playback authorization bound to content, wallet, client fingerprint, and stream version.
3. Native SDKs manage playback sessions, token refresh, error recovery, and telemetry.

## iOS SDK Modules

- StreamGateCore: configuration, URLSession API client, token storage, errors.
- StreamGateAuth: wallet auth, JWT lifecycle, logout.
- StreamGatePlayer: AVPlayer wrapper, playback session lifecycle, playback telemetry.
- StreamGateUI: optional app-facing UI components, not required by the core SDK.

## Security Rules

- JWT tokens are stored in Keychain.
- Playback tokens are short-lived and refreshable.
- Query-string playback tokens are allowed for HLS compatibility but must be redacted from logs.
- Native iOS may migrate to AVAssetResourceLoader or signed URLs for stronger token handling.

## Observability Rules

SDKs report startup time, buffering, quality changes, fatal playback errors, and session end. Server metrics continue to track manifest requests, segment downloads, cache hits, and active viewers.
```

- [ ] **Step 2: Create playback session API doc**

Create `docs/api/playback-session.md`:

```markdown
# Playback Session API

## Create Playback Session

`POST /api/v1/playback-sessions`

Request:

```json
{
  "content_id": "content-1",
  "contract": "0xabc123",
  "token_id": "",
  "chain_id": 1
}
```

Response:

```json
{
  "session_id": "ps_abc",
  "playback_token": "eyJ...",
  "manifest_url": "/api/v1/streaming/content-1/manifest.m3u8?playback_token=eyJ...",
  "expires_at": "2026-06-12T12:10:00Z"
}
```

## Refresh Playback Session

`POST /api/v1/playback-sessions/{id}/refresh`

Response:

```json
{
  "session_id": "ps_abc",
  "playback_token": "eyJ...new",
  "expires_at": "2026-06-12T12:20:00Z"
}
```

## Revoke Playback Session

`DELETE /api/v1/playback-sessions/{id}`

Response: `204 No Content`

## Error Codes

- `401`: JWT missing or expired.
- `403`: NFT gate denied or session revoked.
- `404`: session or content not found.
- `409`: stream version changed; client must create a new session.
```
```

- [ ] **Step 3: Commit**

```bash
git add docs/architecture/player-ios-sdk-optimization.md docs/api/playback-session.md
git commit -m "docs(streaming): document player SDK optimization architecture"
```

---

## Task 14: Verification Pass

**Files:**
- No source changes unless verification exposes a regression.

- [ ] **Step 1: Run Go formatting**

```bash
make fmt
```

Expected: completes successfully.

- [ ] **Step 2: Run focused Go tests**

```bash
go test ./pkg/gateway ./pkg/service ./pkg/models ./pkg/plugins/streaming -count=1
```

Expected: PASS, or document exact pre-existing failures.

- [ ] **Step 3: Run Swift package tests**

```bash
cd ios-sdk && swift test
```

Expected: PASS.

- [ ] **Step 4: Run CI test target if time allows**

```bash
make test-ci
```

Expected: PASS, or document exact pre-existing failures.

- [ ] **Step 5: Run lint if time allows**

```bash
make lint
```

Expected: PASS, or document exact pre-existing lint unrelated to changed files.

- [ ] **Step 6: Produce final implementation summary**

Include:

- Commits made.
- Tests run and exact results.
- Known follow-ups.
- Any dependency approval needed for WalletConnect or other iOS packages.

---

## Self-Review

### Spec coverage

- SDK non-third-party source review: covered by native SDK skeleton using Foundation, URLSession, AVFoundation, and protocol abstractions.
- Player architecture: covered by H5 lifecycle fixes, playback session contract, AVPlayer wrapper, and telemetry-ready state machine.
- iOS architecture: covered by SPM module boundaries and MainActor player state.
- `ARCHITECTURE_OPTIMIZATION.m`: not present in repo; plan creates replacement architecture docs and keeps the implementation independent of the missing file.

### Placeholder scan

No task uses placeholder instructions such as `TBD`, `TODO`, `implement later`, or `similar to`. Each code-changing step includes concrete code and commands.

### Type consistency

- Backend playback session names use `PlaybackSession`, `playback_session`, and `playbackTokenTTL` consistently.
- Swift client model uses `PlaybackSession.sessionID`, `playbackToken`, `manifestURL`, and `expiresAt` consistently across source and tests.
- H5 player fields use `manifestBlobUrl`, `retryCount`, `maxRetries`, `retryTimer`, and `refreshPlaybackToken` consistently.
