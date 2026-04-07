# StreamGate Job Priority Recommendation

This document gives a concrete recommendation for where to use the current project most aggressively.

## Recommended priority order

### Priority 1: Media Backend / Streaming Backend Engineer

This is the strongest fit right now.

Why:

- your original background already matches the problem space
- the project has a believable protected streaming path
- transcoding, worker scheduling, retry, and task lifecycle are now visible
- Web3 becomes an advantage instead of a distraction

Best pitch:

- I built a protected media backend and used Web3 as the authorization layer.

What interviewers will likely like:

- HLS manifest protection
- playback token design
- worker/transcoder foundations
- queueing and retry thinking

### Priority 2: Go Backend Engineer

This is also a strong fit.

Why:

- the project demonstrates Go service design well
- there is clear API design and stateful backend logic
- tests cover meaningful service and route paths
- monolith-to-microservice evolution is easy to explain

Best pitch:

- I used a media product scenario to demonstrate Go backend architecture and service evolution.

What interviewers will likely like:

- auth flow
- routing and handler design
- task APIs
- worker logic
- focused tests

### Priority 3: Web3 Backend Engineer

This is a good secondary target, but not the first one to lean on.

Why:

- you do have real wallet login and NFT verification
- the project shows chain-backed authorization in a real backend context
- but the deepest strength is still backend integration, not protocol depth

Best pitch:

- I focus on turning wallets and NFT ownership into real product-side authorization.

What to avoid:

- do not sell yourself as a smart contract or protocol-heavy candidate

### Priority 4: Platform / Infra-adjacent Backend Roles

This is possible, but should be selective.

Why:

- there is some good worker and service-architecture material
- but observability, deployment hardening, and full production operations are not yet the strongest part

Best pitch:

- I have backend systems instincts and queue/worker/service lifecycle thinking, with room to deepen the infra side.

## Recommended application strategy

### First wave

Apply most aggressively to:

- media backend roles
- streaming backend roles
- Go backend roles in content/media/video companies

### Second wave

Apply selectively to:

- Web3 backend roles that value backend product integration
- creator economy / digital membership / content access companies

### Third wave

Apply carefully to:

- platform roles
- infra-heavy backend roles

Only do this if the job description still values application/backend delivery, not pure infra ownership.

## Best one-line positioning by role

### For media backend jobs

- I built an NFT-gated streaming backend in Go that protects media access using wallet auth and NFT authorization.

### For Go backend jobs

- I used a real media access-control scenario to demonstrate Go backend services, queueing, auth, and task APIs.

### For Web3 backend jobs

- I focused on backend integration of wallet identity and NFT-based access control, not just contract demos.

## Resume guidance

If you only get one project bullet block, the recommended framing is:

- `StreamGate: NFT-Gated Streaming Backend in Go`
- wallet challenge authentication, NFT authorization, protected HLS manifest flow
- playback token design for segment access
- transcoding task API, worker scheduling, retry and cancellation

## What to say if asked “why not just apply as a pure Web3 engineer?”

Suggested answer:

Because my strongest differentiation is not just that I learned Web3.  
It is that I can take an existing media backend skill set and apply Web3 where it creates business value: identity and authorization for protected content.

That makes me especially strong for backend teams that need practical product integration, not only protocol depth.

## Final recommendation

If you want the highest interview conversion with the current project state, prioritize:

1. Media backend / streaming backend
2. Go backend
3. Web3 backend
4. Platform-adjacent backend

That ordering best matches both your background and what this project already proves.
