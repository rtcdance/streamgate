# H5 Demo Acceptance Checklist

Use this as the shortest on-site checklist when you want to validate the StreamGate demo quickly.

## Pre-check

- [ ] Backend gateway is running
- [ ] Recommended backend URL is `http://localhost:9090`
- [ ] MetaMask is installed and unlocked

## Core Flow

### 1. Backend
- [ ] Set `Backend URL`
- [ ] Click `Save Backend URL`
- [ ] `Backend` status becomes `Online`

### 2. Wallet
- [ ] Click `Connect Wallet`
- [ ] Wallet address is displayed
- [ ] `Wallet` status becomes `Connected`

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

### 6. RPC
- [ ] Click `Load RPC Status`
- [ ] Response shows active RPC / candidate RPCs

### 7. Transcoding
- [ ] Click `Submit Task`
- [ ] `task_id` is returned
- [ ] Click `Load Status`
- [ ] Click `Load Tasks`
- [ ] Click `Load Profiles`

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
- RPC failures: confirm `/api/v1/web3/rpc-status` is exposed
- Transcoding failures: confirm current payload fields match backend protocol
