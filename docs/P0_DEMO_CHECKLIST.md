# P0 Demo Checklist

This checklist is the fastest way to demo the current StreamGate P0 path for learning and interviews.

## What is already implemented

- Wallet challenge generation
- Wallet signature login
- NFT verification by `balanceOf` or `ownerOf`
- Runtime RPC status visibility for active / failed endpoints
- Protected HLS manifest access
- Short-lived playback token for segment access
- NFT access cache with `cache_hit`
- Transcoding task submission and status query
- Transcoder profiles listing
- Metrics exposure for auth, NFT, and streaming flows

## What is already test-verified

- `go test ./pkg/service -run 'Test(AuthService_AuthenticateWithWallet|MemoryChallengeStore_ChallengeLifecycle|AuthService_PlaybackTokenLifecycle)' -v`
- `go test ./pkg/plugins/api -v`
- `go test ./cmd/microservices/api-gateway -v`
- `go test ./pkg/service -run 'TestTranscodingService_' -v`
- `go test ./pkg/api/v1 -run 'TestTranscodingHandler_' -v`
- `go test ./pkg/plugins/transcoder -v`

## Local run options

### Option A: Microservice API Gateway

```bash
go run ./cmd/microservices/api-gateway
```

HTTP endpoint:

- `http://localhost:9090`

### Option B: Monolith

```bash
go run ./cmd/monolith/streamgate
```

Use this if you want to demo the plugin-based monolith path instead of the API gateway service.

## Demo flow

### 1. Request wallet challenge

```bash
curl -s \
  -X POST http://localhost:9090/api/v1/auth/challenge \
  -H 'Content-Type: application/json' \
  -d '{
    "wallet": "0xYourWalletAddress",
    "chain_id": 11155111
  }'
```

Expected response shape:

```json
{
  "challenge_id": "....",
  "message": "Sign this message to authenticate with StreamGate....",
  "expires_at": "2026-04-07T00:00:00Z",
  "wallet": "0xYourWalletAddress",
  "chain_id": 11155111
}
```

### 2. Sign the challenge message with your wallet

Use MetaMask, Foundry, or your preferred signer to sign the returned `message`.

You need:

- `wallet`
- `challenge_id`
- `signature`

### 3. Login with wallet signature

```bash
curl -s \
  -X POST http://localhost:9090/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{
    "wallet": "0xYourWalletAddress",
    "challenge_id": "your-challenge-id",
    "signature": "0xyourSignature"
  }'
```

Expected response shape:

```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "wallet_address": "0xYourWalletAddress"
}
```

### 4. Verify NFT access

#### Collection-level access

```bash
curl -s \
  -X POST http://localhost:9090/api/v1/nft/verify \
  -H 'Content-Type: application/json' \
  -d '{
    "chain_id": 11155111,
    "wallet": "0xYourWalletAddress",
    "contract": "0xYourNFTContract"
  }'
```

#### Token-level access

```bash
curl -s \
  -X POST http://localhost:9090/api/v1/nft/verify \
  -H 'Content-Type: application/json' \
  -d '{
    "chain_id": 11155111,
    "wallet": "0xYourWalletAddress",
    "contract": "0xYourNFTContract",
    "token_id": "1"
  }'
```

Expected response shape:

```json
{
  "has_nft": true,
  "balance": 1,
  "chain_id": 11155111,
  "contract": "0xYourNFTContract",
  "cache_hit": false
}
```

Repeat the same request once more and expect:

- `cache_hit: true`

### 4.1 Check runtime RPC status

```bash
curl -s \
  http://localhost:9090/api/v1/web3/rpc-status
```

Expected response shape:

```json
{
  "chains": [
    {
      "chain_id": 11155111,
      "name": "Ethereum Sepolia",
      "rpcs": [
        {
          "url": "https://...",
          "is_active": true,
          "failures": 0
        }
      ]
    }
  ]
}
```

### 5. Request protected manifest

```bash
curl -s \
  'http://localhost:9090/api/v1/streaming/demo/manifest.m3u8?contract=0xYourNFTContract&chain_id=11155111' \
  -H 'Authorization: Bearer YOUR_LOGIN_TOKEN'
```

Expected result:

- HLS manifest content
- segment URL contains `playback_token=...`

Example:

```text
#EXTM3U
#EXT-X-VERSION:3
#EXTINF:10.0,
/api/v1/streaming/demo/segment/0?playback_token=...
```

### 6. Request protected segment

```bash
curl -s \
  'http://localhost:9090/api/v1/streaming/demo/segment/0?playback_token=YOUR_PLAYBACK_TOKEN'
```

Expected result:

- HTTP `200`
- placeholder segment body: `segment`

### 7. Submit a transcoding task

```bash
curl -s \
  -X POST http://localhost:9090/api/v1/transcode/submit \
  -H 'Content-Type: application/json' \
  -d '{
    "content_id": "content-1",
    "profile": "720p",
    "input_url": "https://example.com/input.mp4",
    "priority": 5
  }'
```

Expected response shape:

```json
{
  "task_id": "....",
  "status": "pending"
}
```

### 8. Query transcoding status

```bash
curl -s \
  http://localhost:9090/api/v1/transcode/status/YOUR_TASK_ID
```

Expected response shape:

```json
{
  "id": "....",
  "content_id": "content-1",
  "profile": "720p",
  "status": "pending"
}
```

## Metrics check

```bash
curl -s http://localhost:9090/metrics
```

Look for labels or counters related to:

- `auth_login_success_total`
- `nft_verification_total`
- `streaming_manifest_success_total`
- `streaming_segment_success_total`
- `transcoding_jobs_total`

## Interview talk track

### 1. Project positioning

- This is not a generic Web3 demo.
- It is an `NFT-gated streaming` backend built in Go.
- The core business problem is protected media access, not token speculation.

### 2. Why this is a good transition project

- My existing strength is audio/video backend work: distribution, access control, task orchestration, performance thinking.
- The new skill I am adding is wallet-based identity and chain-backed authorization.
- The combination is realistic for enterprise media products with premium or membership-gated content.

### 3. Key architecture decisions

- Use wallet challenge + signature instead of trusting wallet address input.
- Verify NFT once at manifest access, not on every segment request.
- Issue short-lived playback tokens for segment retrieval.
- Cache NFT access briefly to avoid repeated RPC calls.
- Keep the first implementation monolith/API-gateway friendly before pushing deeper service splits.

### 4. What is intentionally not overbuilt yet

- No DRM system yet
- No segment-level chain lookups
- No full transcoding scheduler demo yet
- No multi-chain production hardening yet

That keeps the scope tight and makes the main learning path clear.

## Suggested live demo order

1. Run tests first to show the path is automated.
2. Show `auth/challenge`.
3. Show `auth/login`.
4. Show `nft/verify`, then repeat it to show `cache_hit`.
5. Show `manifest.m3u8` access.
6. Show `segment` access with playback token.
7. Submit a transcoding task and query its status.
8. End with `/metrics`.

## Risks to call out honestly

- Real wallet signing still depends on your local signer flow.
- Real NFT verification depends on reachable RPC and a real contract on the selected chain.
- Redis-backed challenge storage exists, but the strongest current automated coverage is still on the in-memory path.

That is fine for interviews as long as you present it clearly as the current maturity level.
