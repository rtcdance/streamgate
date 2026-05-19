# Release Process

## Checklist for v1.x.x releases

### Pre-Release

- [ ] `VERSION` file updated (no `-alpha`/`-beta` suffix unless intentional)
- [ ] `CHANGELOG.md` updated with user-facing changes (organized by audience)
- [ ] `go vet ./...` — zero warnings
- [ ] `go test -count=1 -race ./pkg/...` — all pass
- [ ] `make build-all` — all 10 binaries compile
- [ ] `make docker-bake` — Docker images build successfully
- [ ] End-to-end demo verified: `make demo` runs without errors

### Release

- [ ] Tag created: `git tag v1.x.x && git push origin v1.x.x`
- [ ] GitHub Release created with changelog summary
- [ ] Docker images tagged and pushed: `make docker-push`
- [ ] Release announcement drafted (see [launch-kit.md](docs/launch-kit.md))

### Post-Release

- [ ] Monitor issue tracker for regression reports
- [ ] Verify CI pipeline passes for tagged commit
- [ ] Update [product-roadmap.md](docs/product-roadmap.md) with completed items

## Version Scheme

Follows [Semantic Versioning](https://semver.org/):

- **Major**: Breaking API changes, incompatible database migrations
- **Minor**: New features, backward-compatible additions
- **Patch**: Bug fixes, performance improvements, security patches

Pre-release: `1.0.0-alpha.1`, `1.0.0-rc.1` (use for experimental builds)