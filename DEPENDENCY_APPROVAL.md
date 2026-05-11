# Dependency Approval Log

**Purpose**: Track and approve all external dependencies before adding to go.mod

## Approval Process

1. AI proposes dependency with justification
2. Architect reviews: license, maintenance, size, alternatives
3. Architect approves or rejects
4. Only approved dependencies can be added

## Pending Approvals

<!-- Add new dependencies here -->

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
