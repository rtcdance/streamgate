# StreamGate H5 Demo

This page is the acceptance console for StreamGate. It is designed to validate the real end-to-end path, not just isolated API calls:

- wallet challenge login
- NFT verification
- protected HLS playback
- RPC failover visibility
- transcoding submit / status / tasks / profiles

## Quick Start

### 1. Start the backend (dual mode)

StreamGate ships in **two modes** that share the same infrastructure. Pick one — the demo behavior is identical:

| Mode | Frontend (H5 demo) | API | Container count | Boot time |
|------|--------------------|-----|-----------------|-----------|
| **Monolith** (default for dev) | `http://localhost:18000/demo/` | `http://localhost:18080` | 7 | ~30 s |
| **Microservices** (default for prod) | `http://localhost:18001/demo/` | `http://localhost:28080` | 15 | ~2 min |

```bash
# From the repo root
make deploy-monolith          # 7 containers, ~30s
make deploy-microservices     # 15 containers, ~2min
make deploy-status            # Check what is up
make deploy-teardown          # Stop everything
```

> Both modes expose the **same h5-demo app** on different ports. Switching modes is as simple as `make deploy-teardown && make deploy-monolith` (or `deploy-microservices`). The h5-demo directory is bind-mounted, so code edits reflect on `:18000` and `:18001` immediately — no rebuild needed.
>
> Need both running side-by-side for comparison? Use `make fullchain-deploy` (17 containers, all 6 infra + monolith + 9 microservices, ~5 min). See [DEPLOY.md](../DEPLOY.md) for details.

### Point the demo at the backend

The default Backend URL inside the page is `http://localhost:18080` (monolith). To use microservices instead, change the `Backend URL` field in the top bar to `http://localhost:28080` and click `Save Backend URL`. Both backends share the same JWT secret, the same Anvil RPC, and the same NFT contract, so the rest of the flow is identical.

### Scripted acceptance (optional)

If you want a repeatable acceptance run after deploying, use the dedicated scripts in the repo root:

```bash
./scripts/verify-deploy.sh                # 8 health checks against current stack
./scripts/fullchain-acceptance.sh 18080   # full API acceptance (monolith)
./scripts/fullchain-acceptance.sh 28080   # full API acceptance (microservices)
```

The fullchain script runs 11 checks: infrastructure (PostgreSQL/Redis/MinIO/NATS/Anvil), backend health, auth challenge roundtrip, NFT verify roundtrip, manifest auth, transcode submit/status/tasks/profiles, and Prometheus metrics.

### 2. Open the demo

Open `/Users/mingo/Applications/workspace/web3/project/streamgate/h5-demo/index.html` in a browser.

You can:
- open the file directly, or
- serve `h5-demo/` with a small local static server

If the page is not hosted on the same origin as the backend, make sure the `Backend URL` field points to your actual StreamGate backend.
If MetaMask reports that it is unavailable while you open `index.html` directly, enable MetaMask access to file URLs or serve `h5-demo/` over HTTP.
If you serve the page from a local static server on `localhost:8080`, do not treat `8080` as the backend. The H5 demo backend should still point to `http://localhost:18080` (monolith) or `http://localhost:28080` (microservices).

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
- Set `Backend URL` to `http://localhost:18080` (monolith) or `http://localhost:28080` (microservices)
- Click `Save Backend URL`

Expected:
- `Backend` status becomes `Online`
- Acceptance `Step 1` turns green

If it fails:
- confirm the gateway is running (`make deploy-status` from the repo root)
- confirm the backend port is really `18080` (monolith) or `28080` (microservices)
- check whether your browser is blocking cross-origin requests
- run `./scripts/verify-deploy.sh monolith` or `./scripts/verify-deploy.sh microservices` for an 8-point health check

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
- if the page is opened via `file://`, either enable MetaMask access to file URLs or serve `h5-demo/` over HTTP
- if multiple injected wallets exist, make sure MetaMask is the active provider for this page
- if the error lists other detected providers, switch the page back to MetaMask or temporarily disable the competing extension

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
- the current deterministic acceptance evidence for playback-token-protected segment access comes from `scripts/run-docker-acceptance.sh` and the gateway route tests it runs

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

| Setting | Value | Notes |
|---------|-------|-------|
| Backend URL (monolith) | `http://localhost:18080` | Default in-page |
| Backend URL (microservices) | `http://localhost:28080` | Switch in top bar |
| NFT Contract | `0x5FbDB2315678afecb367f032d93F642f64180aa3` | StreamGate Demo NFT on local Anvil (public mint `0x6a627842`) |
| Chain ID | `31337` | Local Anvil (Anvil testnet, not Sepolia) |
| Anvil RPC | `http://localhost:18545` | Bundled with the full stack |
| Demo Video ID | `demo` | |
| Default Transcode Profile | `720p` | |

> **Note for Sepolia testnet users**: The previous `0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f` contract and chain ID `11155111` are still valid for a hosted Sepolia deployment, but the bundled Anvil full stack uses the demo contract above. The h5-demo page auto-detects chain ID 31337 and routes to the bundled Anvil RPC.

## Docker Ports (host-exposed)

| Service | Port | Notes |
|---------|------|-------|
| Monolith API | `18080` | `make deploy-monolith` |
| Monolith metrics | `19090` | `/metrics` on monolith |
| API Gateway (microservices) | `28080` | `make deploy-microservices` |
| H5 demo (monolith nginx) | `18000` | Same app, monolith upstream |
| H5 demo (microservices nginx) | `18001` | Same app, microservices upstream |
| MinIO API | `19000` | Object storage S3 API |
| MinIO Console | `19001` | Web UI (minioadmin/minioadmin) |
| PostgreSQL | `15432` | Persistence |
| Redis | `16379` | Challenge / cache support |
| NATS | `14222` | Event bus |
| Consul | `28500` | Service discovery UI (microservices only) |
| Anvil RPC | `18545` | Local testnet (chain ID 31337) |

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
