# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| 1.x     | ✅ Active |
| < 1.0   | ❌ Pre-release |

## Reporting a Vulnerability

We take security vulnerabilities seriously. Please report them responsibly.

**Do not file a public issue.** Instead, report vulnerabilities via:

- **GitHub Private Vulnerability Reporting**: Use the "Report a vulnerability" button at
  `https://github.com/rtcdance/streamgate/security/advisories` (recommended)
- **Email**: security@streamgate.dev (alternative, may have delays)
- **PGP Key**: Available at `https://streamgate.dev/security/pgp-key.asc`

Include the following details in your report:

1. Description of the vulnerability
2. Steps to reproduce
3. Affected versions and components
4. Potential impact
5. Any suggested mitigation (optional)

### Response Timeline

| Timeframe | Action |
|-----------|--------|
| < 24h | Acknowledgment of receipt |
| < 72h | Initial triage and severity assessment |
| < 7d | Fix development (SEV-1/SEV-2) |
| < 30d | Fix release (SEV-3/SEV-4) |

## Security Features

### Authentication
- EIP-191/712 SIWE for wallet-based authentication
- JWT with RS256 (asymmetric) — auth-service signs, all other services verify
- Challenge nonces with 5-minute expiry (enforced via Redis Lua script)

### NFT Gating
- BlockTagSafe (-4) for reads to prevent reorg bypass
- BlockHash-bound cache entries for reorg detection
- JWT session binding ties verification result to specific session

### Infrastructure
- External Secrets Operator for secrets management
- RBAC with least privilege on Kubernetes
- All secrets injected via environment variables (not files/config)

## Dependencies

See `DEPENDENCY_APPROVAL.md` for the formal dependency review process.
Vulnerability scanning is performed by:
- **Trivy** — Docker image scanning in CI
- **gosec** — Go static analysis in CI
- **slither** — Solidity contract analysis in CI
- **Dependabot** — Automated dependency update PRs
