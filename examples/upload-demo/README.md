# upload-demo

Standalone Go program that demonstrates chunked, resumable upload. No network
calls — simulates the protocol so you can see the request shape.

## What it shows

1. File splitting: 10 MB file → 10 × 1 MB chunks.
2. Per-chunk upload with content hash.
3. Final `CompleteUpload` call that hands off to transcoding.
4. Why resumable upload matters: only failed chunks need retransmission.

## Run

```bash
cd examples/upload-demo
go run main.go
```

## Output

```
=== Chunked Upload Demo ===

Upload ID: upload_abc123
File size: 10.0 MB
Chunk size: 1.0 MB
Total chunks: 10

  Uploading chunk  1/10 ... hash=hash_upload_abc123_chunk_1 ✓
  Uploading chunk  2/10 ... hash=hash_upload_abc123_chunk_2 ✓
  ...
  Uploading chunk 10/10 ... hash=hash_upload_abc123_chunk_10 ✓

--- Complete Upload upload_abc123 ---
  Chunks received: 10
  Integrity check: hash_upload_abc123_chunk_1:hash_...:hash_..._chunk_10
  Status: ✅ assembled and ready for transcoding
```

## Production path

`pkg/service/upload.go` is the real implementation and adds:

| Concern | Implementation |
|---------|----------------|
| Auth | `auth` middleware requires valid JWT |
| Ownership | `nft_gate` middleware checks upload NFT |
| Storage | Each chunk written to MinIO/S3 with its hash as key |
| Resume | Client can `GET /upload/{id}/status` to see which chunks landed |
| Hand-off | On `CompleteUpload`, a `TranscodeTask` is published to NATS |

The transcoding worker picks up the task, runs FFmpeg to produce HLS, and
updates per-profile progress in Redis. The h5-demo dashboard polls
`/api/v1/transcode/{contentId}/progress` to render the per-profile bars.

## Reading order

1. This demo — chunk protocol.
2. `pkg/service/upload.go` — production handler.
3. `pkg/plugins/transcoder/` — worker that picks up `TranscodeTask`.
4. `pkg/service/transcoding.go` — FFmpeg pipeline + per-profile state.
