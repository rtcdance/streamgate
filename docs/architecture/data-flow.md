# Data Flow

> **Date**: 2026-06-05
> **Source**: Code analysis of `pkg/gateway/`, `pkg/middleware/`, `pkg/service/`, `pkg/web3/`, `pkg/storage/`
> **Status**: Single source of truth for request and media pipelines
> **Last verified against**: `master` branch (commit `96beacf`)

This document describes the 4 core data flows and the media pipeline. For protocol details, see [communication.md](communication.md). For service internals, see [microservices.md](microservices.md). Master context: [ARCHITECTURE.md](../ARCHITECTURE.md#5-core-data-flow-auth--nft--streaming).

---

## 1. Wallet Sign-In (Challenge -> Sign -> Verify -> JWT)

Entry: `pkg/gateway/auth_handlers.go:19-43`. Core logic: `pkg/service/auth_wallet.go`.

```mermaid
sequenceDiagram
    autonumber
    actor U as User (MetaMask)
    participant H as h5-demo
    participant G as API Gateway
    participant A as auth service
    participant R as Redis
    participant W as web3 (signature)

    U->>H: Click "Sign & Login"
    H->>G: POST /api/v1/auth/challenge {wallet, chain_id}
    G->>A: GenerateWalletChallenge
    A->>A: Normalize address (EVM hex or Solana base58)
    A->>A: Generate nonce (16 bytes, hex-encoded)
    A->>A: Build SIWE message (EIP-4361)
    A->>R: SaveChallenge(challengeID, wallet, nonce, 5min)
    A-->>G: {challenge_id, message, nonce, expires_at}
    G-->>H: 200
    H->>U: MetaMask personal_sign(message)
    U-->>H: signature

    H->>G: POST /api/v1/auth/login {challenge_id, signature}
    G->>A: AuthenticateWithWallet
    A->>R: GetChallenge + MarkChallengeUsed (atomic)
    alt Challenge invalid or already used
        A-->>G: 401
        G-->>H: 401
    else
        A->>W: VerifySignature (EIP-191 secp256k1)
        Note over A,W: Routes by wallet type:<br/>EIP-191, EIP-712, SIWE, Solana ed25519
        W-->>A: valid
        A->>A: Generate JWT (HS256, 2h, wallet_address claim)
        A-->>G: {jwt_token}
        G-->>H: 200
    end
```

The challenge message follows EIP-4361 (Sign-In with Ethereum) by default, with fallback to plain EIP-191 and EIP-712 typed data. Solana wallets use ed25519 off-chain message verification. Replay protection is atomic via Redis `SETNX` or in-memory `sync.Map.LoadOrStore`.

---

## 2. NFT Ownership Verification

Entry: `pkg/gateway/nft_handlers.go:22-162`. Cache: in-memory LRU + optional Redis backend.

```mermaid
sequenceDiagram
    autonumber
    actor U as User
    participant H as h5-demo
    participant G as API Gateway
    participant N as NFT handler
    participant C as NFT cache
    participant W as web3 (RPC)
    participant BC as Anvil (chain 31337)

    U->>H: Navigate to protected content
    H->>G: POST /api/v1/nft/verify {wallet, contract, chain_id}
    Note over H,G: Authorization: Bearer JWT
    G->>G: JWT middleware validates token
    G->>N: RegisterNFTRoutes handler
    N->>C: CheckCache(wallet, contract, chain_id)
    alt Cache hit and valid
        C-->>N: {has_nft: true, cached: true}
    else Cache miss
        C-->>N: miss
        N->>W: balanceOf(wallet, contract) via eth_call
        W->>BC: JSON-RPC eth_call
        BC-->>W: balance (big.Int)
        W-->>N: big.Int (0 or 1+)
        N->>C: Set(wallet -> has_nft=true, TTL 5min)
    end
    N-->>G: {has_nft: true/false, balance: N}
    G-->>H: 200
```

The NFT gate middleware (`pkg/middleware/nft_gate.go`) reuses the same cache. This means POST /api/v1/nft/verify and the streaming gate both check the same key with the same TTL -- preventing TOCTOU race conditions.

---

## 3. HLS Manifest Delivery (Gated)

Entry: `pkg/gateway/streaming_handlers.go`. Middleware: `pkg/middleware/nft_gate.go`. Route: `pkg/gateway/routes.go`.

```mermaid
sequenceDiagram
    autonumber
    actor U as User
    participant H as h5-demo
    participant G as API Gateway
    participant J as JWT middleware
    participant NF as NFT gate middleware
    participant S as streaming service
    participant C as LRU cache

    U->>H: Click play on video
    H->>G: GET /api/v1/streaming/:id/manifest.m3u8
    Note over H,G: Authorization: Bearer JWT
    G->>J: Validate JWT
    J-->>G: wallet_address from claims
    G->>NF: Check NFT ownership
    NF->>NF: Verify cached NFT status (wallet, content gating rule)
    alt No NFT
        NF-->>G: 403 Forbidden
        G-->>H: 403
    else Has NFT
        NF-->>G: pass
        G->>S: GenerateManifest(contentID, wallet)
        S->>C: LRU cache lookup
        alt Cache hit
            C-->>S: cached manifest
        else Cache miss
            S->>S: Read HLS from MinIO
            S->>C: store in LRU cache
        end
        S->>S: Inject playback token
        S-->>G: manifest.m3u8 text
        G-->>H: 200 manifest.m3u8
    end
```

The manifest contains segment URLs with short-lived playback tokens. The token replaces JWT for segment requests.

---

## 4. HLS Segment Delivery (Playback Token, No JWT)

Entry: `pkg/gateway/routes.go:52` -- the segment route is registered **before** JWT middleware. This is intentional.

```mermaid
sequenceDiagram
    autonumber
    actor H as h5.js (browser)
    participant G as API Gateway
    participant S as streaming service

    H->>G: GET /api/v1/streaming/:id/segment/001.ts
    Note over H,G: ?playback_token=abc123<br/>No Authorization header
    Note over G: Route registered BEFORE JWT middleware<br/>so JWT is never checked for segments
    G->>S: ServeSegment with playback token
    S->>S: Validate playback token (HMAC, expiry)
    alt Token invalid or expired
        S-->>G: 403
        G-->>H: 403
    else Token valid
        S->>S: Read .ts from MinIO
        S-->>G: video/MP2T data
        G-->>H: 200 .ts segment
    end
```

This design enables CDN-friendly segment delivery. The playback token is a short-lived HMAC-signed string that the streaming service validates independently of the auth service.

---

## 5. Media Pipeline: Upload to Playback

The full pipeline from creator upload to viewer playback, as an ASCII diagram (Mermaid sequence is too complex for this branching flow):

```
Creator                     API Gateway                  Storage               Worker                Viewer
   |                            |                          |                      |                     |
   | POST /api/v1/upload/init   |                          |                      |                     |
   |--------------------------->|                          |                      |                     |
   | {filename, size, type}     |                          |                      |                     |
   |<-- {upload_id, chunk_size}-|                          |                      |                     |
   |                            |                          |                      |                     |
   | POST /api/v1/upload/chunk  |                          |                      |                     |
   | (binary, chunk_index, id)  |                          |                      |                     |
   |--------------------------->|-- PUT chunk ----------->| MinIO (tmp)           |                     |
   |<-- 200 --------------------|<-------------------------|                      |                     |
   | (repeat for all chunks)    |                          |                      |                     |
   |                            |                          |                      |                     |
   | POST /upload/:id/complete  |                          |                      |                     |
   |--------------------------->|-- Merge chunks -------->| MinIO (input bucket)  |                     |
   |                            |-- Enqueue transcode --->| NATS JetStream        |                     |
   |<-- {content_id} -----------|                          |                      |                     |
   |                            |                          |                      |                     |
   |                            |                          |  PullSubscribe       |                     |
   |                            |                          |--------------------->| transcoder worker   |
   |                            |                          |                      | FFmpeg: input.mp4   |
   |                            |                          |                      |  -> 240p/480p/720p  |
   |                            |                          |                      |  -> 1080p .ts files |
   |                            |                          |                      |  -> .m3u8 manifest  |
   |                            |                          |-- PUT HLS output --->| MinIO (streamgate)  |
   |                            |                          |<--- ack (progress) ---|                     |
   |                            |                          |                      |                     |
   |                            |                          |                      |                     |
   | Viewer loads player page   |                          |                      |                     |
   | hls.js initializes         |                          |                      |                     |
   | GET /streaming/:id/manifest|                          |                      |                     |
   |--------------------------->|-- Read manifest ------->| MinIO (streamgate)    |                     |
   |<-- manifest.m3u8 ----------|<-------------------------|                      |                     |
   | GET /streaming/:id/seg/1.ts|                          |                      |                     |
   |--------------------------->|-- Read segment -------->| MinIO (streamgate)    |                     |
   |<-- video/MP2T -------------|<-------------------------|                      |                     |
   |                            |                          |                      |                     |
   | (repeat for all segments)  |                          |                      |                     |
```

### Key characteristics

- **Upload**: chunked resumable upload with magic byte detection (`pkg/gateway/upload_handlers.go`, 848 lines)
- **Transcoding queue**: NATS JetStream with `TRANSCODING` stream and `transcoding-worker` consumer (`pkg/storage/nats_queue.go`)
- **FFmpeg profiles**: 240p, 480p, 720p (default), 1080p -- configurable per content
- **Progress**: status events at 0-100% per profile, polled by h5-demo every 2s
- **Cache invalidation**: post-transcode hook in `pkg/gateway/gateway.go:78-85` invalidates streaming LRU cache

---

## Cross-References

- Master architecture: [ARCHITECTURE.md](../ARCHITECTURE.md#5-core-data-flow-auth--nft--streaming)
- Communication: [communication.md](communication.md)
- Microservices: [microservices.md](microservices.md)
- Auth handler: `pkg/gateway/auth_handlers.go`
- NFT handler: `pkg/gateway/nft_handlers.go`
- Streaming handler: `pkg/gateway/streaming_handlers.go`
- NFT gate middleware: `pkg/middleware/nft_gate.go`
- Route registration: `pkg/gateway/routes.go`
- NATS queue: `pkg/storage/nats_queue.go`
