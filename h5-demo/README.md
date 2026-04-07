# StreamGate H5 Demo

This page is the acceptance console for StreamGate. It is designed to validate the real end-to-end path, not just isolated API calls:

- wallet challenge login
- NFT verification
- protected HLS playback
- RPC failover visibility
- transcoding submit / status / tasks / profiles

## Quick Start

### 1. Start the backend

Recommended acceptance path:

```bash
cd streamgate
go run ./cmd/microservices/api-gateway/main.go
```

Recommended backend URL:

```text
http://localhost:9090
```

If you want to use the monolith path instead, you can, but the gateway path is the default acceptance path for this demo.

### 2. Open the demo

Open `/Users/mingo/Applications/workspace/web3/project/streamgate/h5-demo/index.html` in a browser.

You can:
- open the file directly, or
- serve `h5-demo/` with a small local static server

If the page is not hosted on the same origin as the backend, make sure the `Backend URL` field points to your actual StreamGate backend.

## Recommended Acceptance Flow

Use the page in this order:

1. `Backend`
2. `Wallet`
3. `Login`
4. `NFT`
5. `Playback`
6. `RPC`
7. `Transcoding`

The right-side checklist and the top summary bar should update as each step completes.

## Manual Acceptance Checklist

### Step 1: Backend

Action:
- Set `Backend URL` to `http://localhost:9090`
- Click `Save Backend URL`

Expected:
- `Backend` status becomes `Online`
- Acceptance `Step 1` turns green

If it fails:
- confirm the gateway is running
- confirm the backend port is really `9090`
- check whether your browser is blocking cross-origin requests

### Step 2: Wallet

Action:
- Click `Connect Wallet`

Expected:
- wallet address appears
- `Wallet` status becomes `Connected`
- Acceptance `Step 2` turns green

If it fails:
- confirm MetaMask is installed
- confirm MetaMask is unlocked
- confirm the page has permission to access the wallet

### Step 3: Login

Action:
- Click `Sign & Login`

Expected:
- the page requests a backend challenge
- MetaMask asks you to sign the backend-provided `message`
- login result shows a JWT preview
- `Auth` status becomes `Authenticated`
- Acceptance `Step 3` turns green

If it fails:
- confirm wallet is already connected
- confirm the backend implements `/api/v1/auth/challenge` and `/api/v1/auth/login`
- confirm the wallet signs the exact backend challenge, not a locally reconstructed message

### Step 4: NFT Verification

Action:
- Keep the default Sepolia contract
- Click `Verify NFT Ownership`

Expected:
- response shows `has_nft`, `balance`, `chain_id`, `cache_hit`
- if NFT ownership is valid, the playback section becomes visible
- Acceptance `Step 4` turns green

If it fails:
- confirm the connected wallet actually holds the expected NFT
- confirm the backend RPC is reachable
- confirm contract and chain are correct

### Step 5: Protected Playback

Action:
- Click `Play Video`

Expected:
- the page requests the manifest with `Authorization: Bearer <JWT>`
- manifest loads successfully
- video starts or at least enters playable state
- `Playback` status becomes green
- Acceptance `Step 5` turns green

Notes:
- manifest uses JWT + NFT verification
- segment access should rely on playback token flow from the backend

If it fails:
- confirm login succeeded and JWT exists
- confirm NFT verification already passed
- confirm the backend has a valid streaming route for the selected `video_id`

### Step 6: RPC Status

Action:
- Click `Load RPC Status`

Expected:
- output includes chains, active RPC, and candidate RPC node states
- Acceptance `Step 6` turns green

If it fails:
- confirm `/api/v1/web3/rpc-status` is exposed by your gateway
- confirm the backend has initialized chain clients

### Step 7: Transcoding

Action:
- Click `Submit Task`
- Then click `Load Status`
- Then click `Load Tasks`
- Then click `Load Profiles`

Expected:
- submit returns `task_id`
- status returns current task state
- tasks returns a list payload
- profiles returns supported transcoding profiles
- `Transcode` status becomes green
- Acceptance `Step 7` turns green

If it fails:
- confirm your gateway exposes `/api/v1/transcode/submit`
- confirm payload fields match current backend protocol:
  - `content_id`
  - `input_url`
  - `profile`
  - `priority`

## Test Configuration

| Setting | Value |
|---------|-------|
| Backend URL | `http://localhost:9090` |
| NFT Contract | `0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f` |
| Chain ID | `11155111` (Sepolia) |
| Demo Video ID | `demo` |
| Default Transcode Profile | `720p` |

## Real API Endpoints Exercised

- `GET /health` or `GET /api/v1/health`
- `POST /api/v1/auth/challenge`
- `POST /api/v1/auth/login`
- `POST /api/v1/nft/verify`
- `GET /api/v1/web3/rpc-status`
- `GET /api/v1/streaming/:id/manifest.m3u8`
- `GET /api/v1/streaming/:id/segment/:seg`
- `POST /api/v1/transcode/submit`
- `GET /api/v1/transcode/status/:id`
- `GET /api/v1/transcode/tasks`
- `GET /api/v1/transcode/profiles`

## Requirements

- MetaMask browser extension
- a wallet that can sign messages
- backend running on an accessible URL
- a test environment where NFT verification can succeed if you want full green acceptance

## What This Demo Is Good For

- manual acceptance of the main StreamGate path
- interview/demo walkthroughs
- quick regression checks after backend refactors

## What This Demo Does Not Claim

- it is not a full production dashboard
- it does not replace backend automated tests
- NFT metadata rendering is not the primary acceptance target here
