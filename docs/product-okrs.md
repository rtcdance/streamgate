# StreamGate Product OKRs

## North Star Metric

**"Time from first encounter to first NFT-gated video playback"**

Target: Under 10 minutes (from `git clone` to seeing a video behind token gating).

This metric captures the entire first-user experience: discovery → setup → success. Every team initiative should ultimately ladder up to reducing this time.

---

## Q3 2026 OKRs

### Objective 1: Reduce first-playback time to < 10 minutes

| KR | Metric | Current | Target | Owner |
|----|--------|---------|--------|-------|
| KR 1.1 | Demo mode `STREAMGATE_DEMO_MODE=true` works without blockchain | ✅ Done | 100% of new users can start without blockchain | Engineering |
| KR 1.2 | Pre-seeded example video included in `make demo` | ❌ Missing | "demo" includes playable content | Engineering |
| KR 1.3 | Web admin UI for content upload | ❌ Missing | Upload video via browser, not CLI | Product |
| KR 1.4 | Interactive CLI init command (`streamgate init`) | ❌ Missing | Guided setup with prompts | Engineering |

### Objective 2: Build developer community

| KR | Metric | Current | Target | Owner |
|----|--------|---------|--------|-------|
| KR 2.1 | GitHub Stars | ~TBD | 500 | Marketing |
| KR 2.2 | First-time issue response time | ~TBD | < 24h | Engineering |
| KR 2.3 | Documentation site with search | ❌ Missing | Published | Product |
| KR 2.4 | Published npm package | ❌ Missing | `@streamgate/client` on npm | Engineering |

### Objective 3: Establish competitive positioning

| KR | Metric | Current | Target | Owner |
|----|--------|---------|--------|-------|
| KR 3.1 | Competitive analysis published | ✅ Done | Updated quarterly | Product |
| KR 3.2 | Product roadmap published | ✅ Done | Updated monthly | Product |
| KR 3.3 | Public demo deployed (streamgate.dev) | ❌ Missing | Live demo URL | Ops |

---

## How We Measure

- **First-playback time**: Manual tracking (timed from `git clone` to `curl` returns manifest)
- **GitHub Stars**: GitHub API, weekly snapshot
- **Issue response**: GitHub API, first maintainer comment timestamp - issue creation timestamp
- **npm downloads**: npm registry API, weekly

## Review Cadence

- OKR review: Monthly during sprint planning
- Roadmap update: After each quarterly OKR review
- Competitive analysis: Quarterly