# H5 Demo Acceptance Checklist

Use this as the shortest on-site checklist when you want to validate the StreamGate demo quickly.

## Pre-check

- [ ] Backend gateway is running
- [ ] Recommended backend URL is `http://localhost:29090`
- [ ] MetaMask is installed and unlocked
- [ ] If using `file://`, MetaMask file URL access is enabled; otherwise serve `h5-demo/` over HTTP
- [ ] The wallet is connected to the expected test chain

## Core Flow

### 1. Backend
- [ ] Set `Backend URL`
- [ ] Click `Save Backend URL`
- [ ] `Backend` status becomes `Online`

### 2. Wallet
- [ ] Click `Connect Wallet`
- [ ] Wallet address is displayed
- [ ] `Wallet` status becomes `Connected`
- [ ] No `MetaMask is not available in this page` error is shown

### 3. Login
- [ ] Click `Sign & Login`
- [ ] MetaMask signs backend challenge
- [ ] JWT preview appears
- [ ] `Auth` status becomes `Authenticated`

### 4. NFT
- [ ] Click `Verify NFT Ownership`
- [ ] Response contains `has_nft`
- [ ] Response contains `balance`
- [ ] Response contains `cache_hit`

### 5. Playback
- [ ] Click `Play Video`
- [ ] Manifest loads successfully
- [ ] `Playback` status becomes green
- [ ] Manifest access is only allowed after JWT + NFT verification

### 6. RPC
- [ ] Click `Load RPC Status`
- [ ] Response shows active RPC / candidate RPCs

### 7. Transcoding
- [ ] Click `Submit Task`
- [ ] `task_id` is returned
- [ ] Click `Load Status`
- [ ] Click `Load Tasks`
- [ ] Click `Load Profiles`

## Protected Segment Evidence

- [ ] Run `scripts/run-docker-acceptance.sh`
- [ ] Confirm the route tests for manifest and playback-token segment access pass
- [ ] Treat this as the current deterministic acceptance evidence for playback-token-protected segment access

## Full Green Acceptance

The run is considered fully accepted when:

- [ ] the right-side progress checklist is fully green
- [ ] the top summary shows full completion
- [ ] no critical step is left in failed state

## If Something Fails

Use these shortcuts first:

- Backend failures: confirm gateway port and CORS
- Login failures: confirm the exact backend challenge is signed
- NFT failures: confirm wallet really owns the NFT on the selected chain
- Playback failures: confirm JWT exists and NFT verification already passed
- Wallet detection failures: serve the page over HTTP or enable MetaMask access to file URLs
- RPC failures: confirm `/api/v1/web3/rpc-status` is exposed
- Transcoding failures: confirm current payload fields match backend protocol
