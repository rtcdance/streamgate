# streaming-demo

Standalone Go program that demonstrates HLS manifest generation. No network
calls, no blockchain calls — pure playlist construction so you can see the
.m3u8 format without standing up a backend.

## What it shows

1. Building an `#EXTM3U` manifest with `EXT-X-VERSION:3`.
2. Listing segments with `#EXTINF:<duration>`.
3. Per-user playback-token generation bound to content ID + timestamp.
4. The conceptual flow of segment delivery under token gating.

## Run

```bash
cd examples/streaming-demo
go run main.go
```

## Output

```
=== HLS Streaming Demo ===

Content ID: demo-video-001

--- Manifest (playlist.m3u8) ---
#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:10,
seg001.ts
#EXTINF:10,
seg002.ts
...
#EXT-X-ENDLIST

--- Segment Delivery ---
GET /api/v1/streaming/demo-video-001/segment/seg001.ts?token=playback_demo-video-001_...
GET /api/v1/streaming/demo-video-001/segment/seg002.ts?token=...
...
```

## Production path

`pkg/service/streaming.go` adds the production pieces this demo omits:

- Real HLS segments produced by the FFmpeg worker pipeline.
- NFT-ownership check before the manifest is served (`nft_gate`
  middleware in `pkg/middleware/nft_gate.go`).
- JWT playback token bound to `wallet + content + contract + expiry`.
- 403 returned for any segment request without a valid token.

## Reading order

1. This demo — manifest format.
2. `pkg/service/streaming.go` — production manifest delivery.
3. `pkg/middleware/nft_gate.go` — gating layer.
