# GitHub CI/CD Pipeline Guide

**Date**: 2025-01-29  
**Version**: 1.0.0  
**Status**: ✅ Complete

## Overview

StreamGate includes a comprehensive GitHub Actions CI/CD pipeline that automatically runs tests, builds Docker images, and deploys to production.

## Workflows

### 1. CI Pipeline (`.github/workflows/ci.yml`)

Runs on every push and pull request to main/develop branches.

#### Jobs:

**Lint & Format Check**
- Runs golangci-lint
- Checks code formatting with gofmt
- Runs go vet

**Security Scan**
- Runs gosec for security vulnerabilities
- Uploads results to GitHub Security tab

**Build**
- Compiles monolithic application
- Compiles all 9 microservices
- Uploads binaries as artifacts

**Unit Tests**
- Runs all unit tests with race detector
- Generates coverage reports
- Uploads coverage artifacts

**Integration Tests**
- Starts PostgreSQL and Redis services
- Initializes database with migrations
- Runs integration tests
- Generates coverage reports

**E2E Tests**
- Starts PostgreSQL and Redis services
- Initializes database with migrations
- Runs end-to-end tests
- Generates coverage reports

**Benchmark Tests**
- Runs performance benchmarks
- Uploads benchmark results

**Coverage Report**
- Merges all coverage reports
- Uploads to Codecov

**Quality Gate**
- Ensures all jobs pass
- Blocks merge if any job fails

### 2. Docker Build & Push (`.github/workflows/build.yml`)

Runs on push to main branch and on tags.

#### Jobs:

**Build Monolith Docker Image**
- Builds Docker image for monolithic application
- Pushes to GitHub Container Registry
- Caches layers for faster builds

**Build Microservices Docker Images**
- Builds Docker images for all 9 microservices
- Pushes to GitHub Container Registry
- Runs in parallel for all services

**Scan Images**
- Runs Trivy vulnerability scanner
- Uploads results to GitHub Security tab

### 3. Deployment (`.github/workflows/deploy.yml`)

Runs on tags (e.g., v1.0.0).

#### Jobs:

**Deploy with Docker Compose**
- Deploys to production server via SSH
- Pulls latest images
- Runs smoke tests

**Deploy to Kubernetes**
- Deploys using Helm
- Waits for rollout
- Runs smoke tests

**Notify Deployment**
- Sends Slack notification
- Reports deployment status

### 4. Comprehensive Test Suite (`.github/workflows/test.yml`)

Runs on every push, pull request, and daily schedule.

#### Jobs:

**Unit Tests** (11 parallel jobs)
- Analytics tests
- Debug tests
- Middleware tests
- Models tests
- Monitoring tests
- Optimization tests
- Security tests
- Service tests
- Storage tests
- Utility tests
- Web3 tests

**Integration Tests** (19 parallel jobs)
- Analytics integration
- API integration
- Auth integration
- Content integration
- Dashboard integration
- Debug integration
- Middleware integration
- ML integration
- Models integration
- Monitoring integration
- Optimization integration
- Scaling integration
- Security integration
- Service integration
- Storage integration
- Streaming integration
- Transcoding integration
- Upload integration
- Web3 integration

**E2E Tests** (25 parallel jobs)
- Analytics E2E
- API Gateway E2E
- Auth Flow E2E
- Blue-Green Deployment E2E
- Canary Deployment E2E
- Content Management E2E
- Core Functionality E2E
- Dashboard E2E
- Debug E2E
- HPA Scaling E2E
- Middleware Flow E2E
- ML E2E
- Models E2E
- Monitoring Flow E2E
- NFT Verification E2E
- Optimization E2E
- Plugin Integration E2E
- Resource Optimization E2E
- Scaling E2E
- Security E2E
- Streaming Flow E2E
- Transcoding Flow E2E
- Upload Flow E2E
- Utility Functions E2E
- Web3 Integration E2E

**Benchmark Tests**
- Performance benchmarks
- Memory profiling
- CPU profiling

**Load Tests**
- Concurrent load testing
- Database load testing
- Cache load testing

**Security Tests**
- Security audit tests
- Compliance tests
- Encryption tests
- Key management tests

**Test Summary**
- Aggregates all test results
- Generates summary report

## Triggering Workflows

### Manual Trigger

Push to main or develop branch:
```bash
git push origin main
```

### Pull Request

Create a pull request:
```bash
git push origin feature-branch
# Create PR on GitHub
```

### Tag Release

Create a tag to trigger deployment:
```bash
git tag v1.0.0
git push origin v1.0.0
```

### Scheduled

Tests run daily at 2 AM UTC (configured in test.yml).

## Configuration

### Secrets

Add these secrets to GitHub repository settings:

**For Docker Registry:**
- `GITHUB_TOKEN` - Automatically provided

**For Deployment:**
- `DEPLOY_KEY` - SSH private key
- `DEPLOY_HOST` - Deployment server hostname
- `DEPLOY_USER` - Deployment user

**For Kubernetes:**
- `KUBE_CONFIG` - Base64-encoded kubeconfig

**For Notifications:**
- `SLACK_WEBHOOK` - Slack webhook URL

### Environment Variables

Set in workflow files:
- `GO_VERSION` - Go version (default: 1.21)
- `GOPROXY` - Go proxy URL (default: https://goproxy.io,direct)
- `REGISTRY` - Container registry (default: ghcr.io)

## Test Coverage

### Coverage Targets

- **Unit Tests**: 100% coverage
- **Integration Tests**: 100% coverage
- **E2E Tests**: 100% coverage
- **Overall**: 100% coverage

### Coverage Reports

Coverage reports are:
1. Generated for each test run
2. Merged into a single report
3. Uploaded to Codecov
4. Available as artifacts

### Viewing Coverage

1. **GitHub Actions**: Download coverage artifacts
2. **Codecov**: Visit codecov.io dashboard
3. **Local**: Run `go test -cover ./...`

## Performance

### Build Time

- **Lint & Format**: ~2 minutes
- **Security Scan**: ~3 minutes
- **Build**: ~5 minutes
- **Unit Tests**: ~10 minutes
- **Integration Tests**: ~15 minutes
- **E2E Tests**: ~20 minutes
- **Benchmark Tests**: ~5 minutes
- **Total**: ~60 minutes

### Parallel Execution

- Unit tests: 11 parallel jobs
- Integration tests: 19 parallel jobs
- E2E tests: 25 parallel jobs
- Microservice builds: 9 parallel jobs

### Caching

- Go modules cached between runs
- Docker layers cached with buildx
- Artifacts cached for 5 days

## Troubleshooting

### Build Failures

**Issue**: Build fails with "module not found"

**Solution**:
```bash
go mod download
go mod tidy
git push
```

**Issue**: Docker build fails

**Solution**:
```bash
docker build -f deploy/docker/Dockerfile.monolith .
```

### Test Failures

**Issue**: Tests fail locally but pass in CI

**Solution**:
```bash
# Run with same environment as CI
docker-compose up -d postgres redis
go test -v ./test/...
```

**Issue**: Database connection fails

**Solution**:
```bash
# Check database is running
docker-compose ps
# Check migrations
psql -h localhost -U streamgate -d streamgate -c "\dt"
```

### Deployment Failures

**Issue**: Deployment fails with SSH error

**Solution**:
1. Verify `DEPLOY_KEY` secret is set
2. Verify `DEPLOY_HOST` is correct
3. Check SSH key permissions: `chmod 600 ~/.ssh/deploy_key`

**Issue**: Kubernetes deployment fails

**Solution**:
1. Verify `KUBE_CONFIG` secret is set
2. Check kubeconfig is base64-encoded
3. Verify cluster is accessible

## Best Practices

### Commit Messages

Use conventional commits:
```
feat: Add new feature
fix: Fix bug
test: Add tests
docs: Update documentation
ci: Update CI/CD
```

### Pull Requests

1. Create feature branch: `git checkout -b feature/name`
2. Make changes and commit
3. Push to GitHub: `git push origin feature/name`
4. Create pull request
5. Wait for CI to pass
6. Request review
7. Merge when approved

### Releases

1. Update version in code
2. Create tag: `git tag v1.0.0`
3. Push tag: `git push origin v1.0.0`
4. CI automatically builds and deploys
5. Create release notes on GitHub

## Monitoring

### GitHub Actions Dashboard

View workflow runs:
1. Go to repository
2. Click "Actions" tab
3. Select workflow
4. View run details

### Artifacts

Download test artifacts:
1. Go to workflow run
2. Scroll to "Artifacts" section
3. Download desired artifact

### Logs

View detailed logs:
1. Go to workflow run
2. Click job name
3. Expand step to see logs

## Integration with Other Tools

### Codecov

Coverage reports automatically uploaded to Codecov.

**View coverage**: https://codecov.io/gh/rtcdance/streamgate

### GitHub Security

Security scan results available in:
1. Security tab
2. Code scanning alerts
3. Dependabot alerts

### Slack Notifications

Deployment notifications sent to Slack channel.

**Configure**: Add `SLACK_WEBHOOK` secret

## Advanced Configuration

### Custom Test Runners

Add custom test runners in workflow:
```yaml
- name: Run custom tests
  run: |
    go test -v -custom-flag ./test/...
```

### Matrix Builds

Run tests on multiple Go versions:
```yaml
strategy:
  matrix:
    go-version: ['1.20', '1.21', '1.22']
```

### Conditional Steps

Run steps conditionally:
```yaml
- name: Deploy
  if: startsWith(github.ref, 'refs/tags/v')
  run: ./deploy.sh
```

## Maintenance

### Regular Updates

1. Update Go version quarterly
2. Update GitHub Actions versions
3. Review and update dependencies
4. Monitor security advisories

### Cleanup

1. Delete old artifacts (auto-cleanup after 5 days)
2. Archive old workflow runs
3. Update documentation

## Support

### Documentation

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [Go Testing Documentation](https://golang.org/pkg/testing/)
- [Docker Documentation](https://docs.docker.com/)

### Issues

Report issues:
1. Check existing issues
2. Create new issue with details
3. Include workflow logs
4. Include error messages

---

**Status**: ✅ Complete  
**Last Updated**: 2025-01-29  
**Version**: 1.0.0

