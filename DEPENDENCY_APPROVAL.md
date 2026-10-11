# Dependency Approval Log

**Purpose**: Track and approve all external dependencies before adding to go.mod

## Approval Process

1. AI proposes dependency with justification
2. Architect reviews: license, maintenance, size, alternatives
3. Architect approves or rejects
4. Only approved dependencies can be added

## Pending Approvals

<!-- Add new dependencies here -->

## Approved Version Bumps (security)

| Date | Dependency | From → To | Reason |
|------|-----------|-----------|--------|
| 2026-10-11 | golang.org/x/crypto | v0.49.0 → v0.55.0 | 23 HIGH CVEs in x/crypto/ssh (Trivy blocking gate, CI run 38105648347) |
| 2026-10-11 | golang.org/x/crypto | v0.54.0 → v0.55.0 (→ v0.57.0) | CVE-2026-56854 (HIGH, x/crypto/ssh). Trivy Docker run 38112149624. Later raised again to v0.57.0 as a transitive requirement of `x/net v0.60.0` |
| 2026-10-11 | google.golang.org/grpc | v1.82.2 → v1.83.1 → **v1.83.2** | Two chained CVEs. CVE-2026-84304 (DoS) fixed in 1.83.1; the bump to 1.83.1 then exposed CVE-2026-84445 (DoS), fixed in 1.83.2. Note v1.82.2 was *not* affected by 84445 — the intermediate step moved into its vulnerable range. Both `go 1.25.0` |
| 2026-10-11 | golang.org/x/text | v0.40.0 → v0.41.0 (→ v0.42.0) | Transitive requirement of x/crypto v0.55.0. No direct usage change |
| 2026-10-11 | golang.org/x/net | v0.57.0 → v0.60.0 | CVE-2026-78669 (HIGH, net/http http2 DoS). Declares `go 1.26.0` — this is the bump that required raising the module's `go` directive |
| 2026-10-11 | **Go toolchain** | 1.25 → **1.26** (`go.mod` adds `toolchain go1.26.9`) | 3 stdlib HIGH (CVE-2026-78667 / -78669 / -97031: net/http, http2, crypto/tls) fixed only in 1.26.9 / 1.27.2 — the Go 1.25 line receives **no** patch. Approved by the human owner; see `TRANSITION_PLAN.md` baseline amendment 2026-10-11 |

### Indirect dependencies pulled in transitively by the 2026-10-11 bump

Not direct dependencies — no code imports them. They enter the module graph via
`golang.org/x/net v0.60.0` / `golang.org/x/crypto v0.57.0`. Logged per the
"不新增 Go 依赖（见 DEPENDENCY_APPROVAL.md）" convention, since `go.mod` now carries them.

| Module | Pulled in by | Notes |
|--------|--------------|-------|
| `github.com/miekg/dns` | `golang.org/x/net v0.60.0` | transitive, `// indirect` |
| `github.com/opentracing/opentracing-go` | `golang.org/x/net v0.60.0` | transitive, `// indirect` |
| `go.uber.org/atomic` | `golang.org/x/crypto v0.57.0` | transitive, `// indirect` |

## Resolved — Go baseline raised to 1.26 (was: blocked on Go 1.25)

The four entries below were all reported by the Docker Trivy gate (`severity CRITICAL,HIGH`,
`exit-code 1`). Every fixed version required a Go toolchain ≥ 1.26, which conflicted with the
then-current "Go 版本已对齐 1.25" baseline. **Resolved on 2026-10-11** by raising the baseline to
Go 1.26 with an explicit `toolchain go1.26.9` floor rather than by adding `--ignore-unfixed`/`.trivyignore`
exceptions — the gate keeps blocking on real, fixable findings.

| CVE | Component | Was | Fixed by |
|-----|-----------|-----|----------|
| CVE-2026-78669 | golang.org/x/net | v0.57.0 | `x/net v0.60.0` (requires `go 1.26.0`) |
| CVE-2026-78667 | stdlib (net/http) | go1.25.14 | Go toolchain 1.26.9 |
| CVE-2026-78669 | stdlib (net/http http2) | go1.25.14 | Go toolchain 1.26.9 |
| CVE-2026-97031 | stdlib (crypto/tls) | go1.25.14 | Go toolchain 1.26.9 |

**Evidence note**: `e13f31d`'s commit message claimed "x/net v0.57 (CVE-2026-78669 fix)". The scan run
on that very commit (38112149624) reported `golang.org/x/net v0.57.0` as **still vulnerable**, fixed in
`0.60.0`. The claim was not supported by its own artifact — corrected by this bump.



## Approved Dependencies

### Standard Library Preference
- **Rule**: Use Go stdlib when sufficient
- **Examples**:
  - ✅ Use `net/http` not `gin` for simple HTTP
  - ✅ Use `encoding/json` not `jsoniter` unless proven bottleneck
  - ✅ Use `database/sql` with driver, not ORM

### Current Approved

#### github.com/ethereum/go-ethereum v1.13.0
- **Purpose**: Ethereum client library
- **Why**: Core Web3 functionality (RPC, ABI, crypto)
- **Alternatives**: None (standard Ethereum library)
- **License**: LGPL-3.0
- **Size**: ~50MB
- **Approved by**: [architect-name]
- **Date**: 2026-03-30

#### github.com/prometheus/client_golang v1.18.0
- **Purpose**: Metrics collection
- **Why**: Industry standard for observability
- **Alternatives**: Custom metrics (too much work)
- **License**: Apache-2.0
- **Size**: 5MB
- **Approved by**: [architect-name]
- **Date**: 2026-03-30

#### go.uber.org/zap v1.26.0
- **Purpose**: Structured logging
- **Why**: High performance, structured logs
- **Alternatives**: log/slog (Go 1.24+, consider migration)
- **License**: MIT
- **Size**: 500KB
- **Approved by**: [architect-name]
- **Date**: 2026-03-30

## Rejected Dependencies

### Example: github.com/gin-gonic/gin
- **Reason**: net/http sufficient for our use case
- **Alternative**: Use stdlib net/http with chi router if needed
- **Rejected by**: [architect-name]
- **Date**: 2026-03-30

## Dependency Review Criteria

### Must Answer
1. **Why needed?** What problem does it solve?
2. **Why not stdlib?** What's missing from standard library?
3. **Alternatives?** What other options exist?
4. **Maintenance?** Last commit, open issues, stars
5. **License?** Compatible with project license
6. **Size?** Impact on binary size
7. **Security?** Known vulnerabilities

### Red Flags
- ❌ Last commit >1 year ago
- ❌ Many open critical issues
- ❌ Viral license (GPL without LGPL exception)
- ❌ >10MB for simple functionality
- ❌ Known CVEs without fixes

### Approval Threshold
- **Low risk**: <1MB, MIT/Apache, active maintenance → Auto-approve
- **Medium risk**: 1-10MB, LGPL, moderate activity → Architect review
- **High risk**: >10MB, GPL, inactive, CVEs → Reject or deep review
