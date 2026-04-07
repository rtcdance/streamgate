# StreamGate Remaining Work Items

## Current conclusion

The core refactor is complete enough for:

- resume use
- interview explanation
- live demo
- targeted applications to media backend, streaming backend, and Go backend roles

The remaining items are mostly about hardening, completeness, and production-readiness.

## P1: Worth doing next

### 1. RPC high availability

Current state:

- basic multi-RPC failover now exists in the chain client
- full production-grade failover policy is still incomplete
- startup fallback and all-endpoint failure handling now have automated coverage
- failed endpoints now enter a cooldown window before reuse
- runtime RPC status is now queryable from the chain client / service layer

Why it matters:

- this is the biggest remaining gap if you want to sound more production-oriented

Suggested next work:

- multiple RPC endpoints per chain
- failover policy
- simple circuit breaker / retry / backoff behavior

### 2. NFT metadata completeness

Current state:

- ownership verification is real
- metadata story is still partial

Why it matters:

- not critical for backend/media interviews
- useful if you want a more complete NFT product story

Suggested next work:

- unify metadata fetching path
- make it explicit which path is chain-native and which path is off-chain URI fetch

### 3. Observability cleanup

Current state:

- metrics paths exist
- observability is usable but not fully unified

Why it matters:

- helps platform-oriented storytelling
- helps production-readiness discussion

Suggested next work:

- finalize one metrics path
- make auth / nft / streaming / transcoding naming more uniform

### 4. Worker / transcoder deeper lifecycle coverage

Current state:

- core queue / retry / cancel / status paths are covered

Why it matters:

- useful if you want to lean harder into media backend depth

Suggested next work:

- more pagination/filter tests
- visibility under concurrent submission
- tighter task-state invariants

## P2: Nice to have

### 1. More realistic transcoding control plane

- better job ownership model
- richer task metadata
- more realistic FFmpeg profile orchestration

### 2. Better deployment and ops story

- runtime configuration cleanup
- service dependency readiness strategy
- stronger production deployment notes

### 3. More end-to-end demo polish

- cleaner demo data
- simpler wallet-signing helper flow
- more polished output examples

## Not required before applying

These do not need to be finished before you start applying:

- full DRM
- fully productionized RPC failover
- full metadata completeness
- full infra/platform hardening
- full multi-chain story

## Recommended next priority order

1. RPC high availability
2. Worker / transcoder deeper lifecycle coverage
3. Observability cleanup
4. NFT metadata completeness

## Practical advice

If your goal is interview conversion, the best next step is not to keep expanding endlessly.

The best next step is:

- apply with the current project
- practice the spoken story
- keep one or two well-chosen hardening items in progress

That gives you both a strong current story and a credible “what I would improve next” answer.
