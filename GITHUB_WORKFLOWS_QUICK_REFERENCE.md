# GitHub Workflows - Quick Reference

**Updated**: 2025-01-29  
**Status**: ✅ All workflows support master and main branches

## Branch Support Matrix

| Workflow | main | master | develop | PR | Tags |
|----------|------|--------|---------|----|----|
| **CI Pipeline** | ✅ | ✅ | ✅ | ✅ | - |
| **Docker Build** | ✅ | ✅ | ❌ | ✅ | ✅ |
| **Deployment** | ✅ | ✅ | ❌ | ❌ | ✅ |
| **Test Suite** | ✅ | ✅ | ✅ | ✅ | - |

## Quick Commands

### Push to main branch
```bash
git push origin main
```
**Triggers**: CI, Docker Build, Test Suite

### Push to master branch
```bash
git push origin master
```
**Triggers**: CI, Docker Build, Test Suite

### Push to develop branch
```bash
git push origin develop
```
**Triggers**: CI, Test Suite

### Create release
```bash
git tag v1.0.0
git push origin v1.0.0
```
**Triggers**: Docker Build, Deployment

### Create pull request
```bash
git push origin feature/my-feature
# Then create PR on GitHub
```
**Triggers**: CI, Docker Build (if PR to main/master), Test Suite

## Workflow Details

### CI Pipeline
- **File**: `.github/workflows/ci.yml`
- **Branches**: main, master, develop
- **Jobs**: Lint, Security, Build, Tests, Coverage
- **Duration**: ~60 minutes

### Docker Build
- **File**: `.github/workflows/build.yml`
- **Branches**: main, master
- **Jobs**: Build 10 Docker images
- **Duration**: ~30 minutes

### Deployment
- **File**: `.github/workflows/deploy.yml`
- **Branches**: main, master (with tags)
- **Jobs**: Deploy to production
- **Duration**: ~15 minutes

### Test Suite
- **File**: `.github/workflows/test.yml`
- **Branches**: main, master, develop
- **Jobs**: 55+ test jobs
- **Duration**: ~90 minutes
- **Schedule**: Daily at 2 AM UTC

## Monitoring Workflows

### View workflow runs
1. Go to GitHub repository
2. Click "Actions" tab
3. Select workflow to view runs

### Check specific branch
1. Click "Actions" tab
2. Filter by branch name
3. View workflow runs

### View workflow logs
1. Click on workflow run
2. Click on job name
3. View detailed logs

## Troubleshooting

### Workflow not triggering
- Check branch name (case-sensitive)
- Verify branch exists in repository
- Check branch protection rules

### Docker build not running on develop
- This is expected behavior
- Docker build only runs on main/master

### Deployment not running
- Ensure tag is created (v*)
- Ensure push is to main or master
- Check deployment secrets are configured

## Configuration

### Add new branch
Edit `.github/workflows/*.yml`:
```yaml
on:
  push:
    branches: [ main, master, develop, staging ]
```

### Change workflow trigger
Edit `.github/workflows/*.yml`:
```yaml
on:
  push:
    branches: [ main, master ]
  pull_request:
    branches: [ main, master ]
```

### Add schedule
Edit `.github/workflows/*.yml`:
```yaml
on:
  schedule:
    - cron: '0 2 * * *'  # Daily at 2 AM UTC
```

## Best Practices

1. **Use main as primary branch**
   - Keep main stable
   - Require PR reviews
   - Protect main branch

2. **Use master as backup**
   - Mirror of main
   - For compatibility
   - Optional

3. **Use develop for development**
   - Daily development
   - Feature branches
   - Experimental code

4. **Use tags for releases**
   - Semantic versioning (v1.0.0)
   - Triggers deployment
   - Creates release

5. **Monitor workflow runs**
   - Check Actions tab regularly
   - Review failed workflows
   - Fix issues promptly

## Files Modified

- ✅ `.github/workflows/ci.yml`
- ✅ `.github/workflows/build.yml`
- ✅ `.github/workflows/deploy.yml`
- ✅ `.github/workflows/test.yml`

## Summary

All GitHub Actions workflows now support both `master` and `main` branches. Push code to either branch and workflows will automatically trigger.

---

**Last Updated**: 2025-01-29  
**Status**: ✅ Complete
