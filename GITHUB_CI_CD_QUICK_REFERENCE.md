# GitHub CI/CD Quick Reference

**Date**: 2025-01-29  
**Version**: 1.0.0

## Quick Start

### 1. Setup GitHub Secrets (One-time)

Go to repository Settings → Secrets and variables → Actions

Add these secrets:
```
DEPLOY_KEY=<your-ssh-private-key>
DEPLOY_HOST=<your-server-hostname>
DEPLOY_USER=<your-deploy-user>
KUBE_CONFIG=<base64-encoded-kubeconfig>
SLACK_WEBHOOK=<your-slack-webhook-url>
```

### 2. Push Code to Trigger CI

```bash
git add .
git commit -m "feat: Add new feature"
git push origin main
```

CI automatically runs:
- Lint & format check
- Security scan
- Build
- Unit tests
- Integration tests
- E2E tests
- Benchmark tests
- Coverage report

### 3. Create Release to Deploy

```bash
git tag v1.0.0
git push origin v1.0.0
```

Deployment automatically runs:
- Docker build & push
- Deploy to Docker Compose
- Deploy to Kubernetes
- Smoke tests
- Slack notification

## Workflow Status

### View Workflow Runs

1. Go to repository
2. Click "Actions" tab
3. Select workflow
4. View run details

### Check Test Results

1. Go to workflow run
2. Scroll to "Jobs" section
3. Click job to see logs
4. Check "Annotations" for errors

### Download Artifacts

1. Go to workflow run
2. Scroll to "Artifacts" section
3. Download desired artifact

## Common Tasks

### Run Tests Locally

```bash
# Unit tests
go test -v ./test/unit/...

# Integration tests
docker-compose up -d postgres redis
go test -v ./test/integration/...

# E2E tests
go test -v ./test/e2e/...

# All tests
make test
```

### View Coverage

```bash
# Generate coverage
go test -cover ./...

# View coverage report
go tool cover -html=coverage.out
```

### Debug Failed Test

```bash
# Run specific test
go test -v -run TestName ./test/...

# Run with verbose output
go test -v -race ./test/...

# Run with timeout
go test -timeout=30s ./test/...
```

### Build Docker Image Locally

```bash
# Monolith
docker build -f deploy/docker/Dockerfile.monolith -t streamgate:latest .

# Microservice
docker build -f deploy/docker/Dockerfile.api-gateway -t streamgate-api-gateway:latest .
```

### Deploy Locally

```bash
# Docker Compose
docker-compose up -d

# Kubernetes
kubectl apply -f deploy/k8s/

# Helm
helm install streamgate deploy/helm/
```

## Troubleshooting

### CI Fails with "Module not found"

**Solution**:
```bash
go mod download
go mod tidy
git push
```

### Tests Fail Locally but Pass in CI

**Solution**:
```bash
# Run with same environment as CI
docker-compose up -d postgres redis
export DB_HOST=localhost
export DB_PORT=5432
export DB_USER=streamgate
export DB_PASSWORD=streamgate
export DB_NAME=streamgate
export REDIS_HOST=localhost
export REDIS_PORT=6379
go test -v ./test/...
```

### Docker Build Fails

**Solution**:
```bash
# Check Dockerfile
cat deploy/docker/Dockerfile.monolith

# Build locally
docker build -f deploy/docker/Dockerfile.monolith .

# Check for errors
docker build --progress=plain -f deploy/docker/Dockerfile.monolith .
```

### Deployment Fails

**Solution**:
1. Check SSH key: `ssh-keyscan -H <host>`
2. Check kubeconfig: `kubectl cluster-info`
3. Check Slack webhook: `curl -X POST <webhook-url>`

## Workflow Files

### CI Pipeline
**File**: `.github/workflows/ci.yml`
**Trigger**: Push/PR to main/develop
**Duration**: ~60 minutes
**Jobs**: 9

### Docker Build
**File**: `.github/workflows/build.yml`
**Trigger**: Push to main, tags
**Duration**: ~30 minutes
**Jobs**: 11

### Deployment
**File**: `.github/workflows/deploy.yml`
**Trigger**: Tags (v*)
**Duration**: ~15 minutes
**Jobs**: 3

### Test Suite
**File**: `.github/workflows/test.yml`
**Trigger**: Push/PR, daily at 2 AM UTC
**Duration**: ~90 minutes
**Jobs**: 55+

## Test Categories

### Unit Tests (11 jobs)
- Analytics, Debug, Middleware, Models, Monitoring
- Optimization, Security, Service, Storage, Utility, Web3

### Integration Tests (19 jobs)
- Analytics, API, Auth, Content, Dashboard, Debug
- Middleware, ML, Models, Monitoring, Optimization, Scaling
- Security, Service, Storage, Streaming, Transcoding, Upload, Web3

### E2E Tests (25 jobs)
- Analytics, API Gateway, Auth Flow, Blue-Green, Canary
- Content Management, Core Functionality, Dashboard, Debug, HPA
- Middleware Flow, ML, Models, Monitoring, NFT Verification
- Optimization, Plugin Integration, Resource Optimization, Scaling
- Security, Streaming, Transcoding, Upload, Utility, Web3

### Other Tests
- Benchmark tests
- Load tests
- Security tests

## Performance Tips

### Speed Up CI

1. **Use caching**:
   ```yaml
   - uses: actions/cache@v3
     with:
       path: ~/go/pkg/mod
       key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
   ```

2. **Run tests in parallel**:
   ```bash
   go test -parallel 4 ./...
   ```

3. **Skip slow tests**:
   ```bash
   go test -short ./...
   ```

### Reduce Build Time

1. **Use Docker layer caching**:
   ```yaml
   cache-from: type=gha
   cache-to: type=gha,mode=max
   ```

2. **Parallel builds**:
   ```yaml
   strategy:
     matrix:
       service: [api-gateway, auth, cache, ...]
   ```

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

1. Create feature branch
2. Make changes
3. Push to GitHub
4. Create PR
5. Wait for CI to pass
6. Request review
7. Merge when approved

### Releases

1. Update version
2. Create tag: `git tag v1.0.0`
3. Push tag: `git push origin v1.0.0`
4. CI automatically builds and deploys
5. Create release notes

## Monitoring

### GitHub Actions Dashboard
- View workflow runs
- Check job status
- Download artifacts
- View logs

### Codecov Dashboard
- View coverage trends
- Compare coverage
- Set coverage goals

### Slack Notifications
- Deployment status
- Build failures
- Test results

## Integration

### Codecov
Coverage reports automatically uploaded.

**View**: https://codecov.io/gh/rtcdance/streamgate

### GitHub Security
Security scan results in:
- Security tab
- Code scanning alerts
- Dependabot alerts

### Slack
Deployment notifications sent to configured channel.

## Advanced

### Custom Test Runners

Add to workflow:
```yaml
- name: Run custom tests
  run: |
    go test -v -custom-flag ./test/...
```

### Matrix Builds

Test on multiple versions:
```yaml
strategy:
  matrix:
    go-version: ['1.20', '1.21', '1.22']
```

### Conditional Steps

Run conditionally:
```yaml
- name: Deploy
  if: startsWith(github.ref, 'refs/tags/v')
  run: ./deploy.sh
```

## Support

### Documentation
- [GitHub Actions Docs](https://docs.github.com/en/actions)
- [Go Testing Docs](https://golang.org/pkg/testing/)
- [Docker Docs](https://docs.docker.com/)

### Issues
1. Check existing issues
2. Create new issue with details
3. Include workflow logs
4. Include error messages

---

**Last Updated**: 2025-01-29  
**Version**: 1.0.0

