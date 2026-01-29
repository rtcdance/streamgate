# Local Lint Verification Guide

**Version**: 1.0.0  
**Last Updated**: 2025-01-29  
**Status**: ✅ Complete

## Overview

This guide explains how to run linting checks locally before pushing code to GitHub. Local linting ensures code quality and prevents CI/CD failures.

## Quick Start

### Run Linting Check
```bash
make lint
```

### Auto-Fix Issues
```bash
make lint-fix
```

### Verbose Output
```bash
make lint-verbose
```

## Prerequisites

### Install golangci-lint

**Option 1: Using Go**
```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

**Option 2: Using Homebrew (macOS)**
```bash
brew install golangci-lint
```

**Option 3: Using apt (Linux)**
```bash
sudo apt-get install golangci-lint
```

### Verify Installation
```bash
golangci-lint --version
```

## Scripts

### lint.sh - Linting Verification

Runs comprehensive linting checks on the codebase.

**Usage:**
```bash
scripts/lint.sh [OPTIONS]
```

**Options:**
- `-v, --verbose` - Enable verbose output
- `-c, --config FILE` - Use custom config file (default: .golangci.yml)
- `-t, --timeout TIME` - Set lint timeout (default: 5m)
- `-r, --report` - Generate lint report file
- `-h, --help` - Show help message

**Examples:**
```bash
# Standard lint check
scripts/lint.sh

# Verbose output
scripts/lint.sh -v

# Custom timeout
scripts/lint.sh -t 10m

# Generate report
scripts/lint.sh -r

# All options
scripts/lint.sh -v -t 10m -r
```

### lint-fix.sh - Auto-Fix Issues

Automatically fixes common linting issues.

**Usage:**
```bash
scripts/lint-fix.sh [OPTIONS]
```

**Options:**
- `-v, --verbose` - Enable verbose output
- `-h, --help` - Show help message

**Examples:**
```bash
# Auto-fix issues
scripts/lint-fix.sh

# Verbose output
scripts/lint-fix.sh -v
```

**What it fixes:**
- Code formatting (gofmt)
- Import organization (goimports)
- Lint checks (golangci-lint)

## Makefile Targets

### make lint
Runs linting checks using the lint.sh script.

```bash
make lint
```

### make lint-fix
Auto-fixes linting issues using the lint-fix.sh script.

```bash
make lint-fix
```

### make lint-verbose
Runs linting with verbose output.

```bash
make lint-verbose
```

## Pre-Commit Hook

A pre-commit hook is automatically installed at `.git/hooks/pre-commit`. This hook:

- Runs before each commit
- Checks staged Go files
- Prevents commits that fail linting
- Can be bypassed with `--no-verify` (not recommended)

### How It Works

1. **Automatic Check**: Hook runs automatically before commit
2. **Staged Files Only**: Only checks files being committed
3. **Fail on Error**: Prevents commit if linting fails
4. **Helpful Messages**: Shows how to fix issues

### Example

```bash
$ git commit -m "Add new feature"

=== StreamGate Pre-Commit Hook ===

=== Linting staged Go files ===
✓ Linting passed for staged files

=== Checking code formatting ===
✓ Code formatting is correct

✓ All pre-commit checks passed
```

### Bypass Hook (Not Recommended)

```bash
git commit --no-verify
```

## Configuration

### .golangci.yml

The linting configuration is in `.golangci.yml`. Key settings:

**Enabled Linters:**
- errcheck - Error checking
- gosimple - Code simplification
- govet - Go vet
- staticcheck - Static analysis
- gofmt - Code formatting
- goimports - Import organization
- gosec - Security scanning
- gocritic - Code criticism
- cyclop/gocyclo - Complexity checking

**Timeout:**
```yaml
run:
  timeout: 5m
```

**Excluded Paths:**
```yaml
skip-dirs:
  - vendor
  - .git
  - node_modules
```

**Excluded Files:**
```yaml
skip-files:
  - ".*_test.go$"
  - ".*_mock.go$"
```

## Workflow

### Before Committing

1. **Make changes** to your code
2. **Run linting**: `make lint`
3. **Fix issues**: `make lint-fix` (if needed)
4. **Review changes**: `git diff`
5. **Stage files**: `git add .`
6. **Commit**: `git commit -m "message"`

### If Linting Fails

1. **Review errors**: Check the lint output
2. **Auto-fix**: `make lint-fix`
3. **Manual fixes**: Fix remaining issues manually
4. **Verify**: `make lint`
5. **Commit**: `git commit -m "fix: lint issues"`

### Example Workflow

```bash
# Make changes
vim pkg/service/auth.go

# Check linting
make lint

# If issues found, auto-fix
make lint-fix

# Review changes
git diff

# Stage and commit
git add .
git commit -m "feat: add new auth method"
```

## Common Issues

### Issue: golangci-lint not found

**Solution:**
```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

### Issue: Timeout during linting

**Solution:** Increase timeout
```bash
scripts/lint.sh -t 10m
```

### Issue: Too many errors to fix manually

**Solution:** Use auto-fix script
```bash
make lint-fix
```

### Issue: Pre-commit hook blocking commits

**Solution 1:** Fix the issues
```bash
make lint-fix
git add .
git commit -m "fix: lint issues"
```

**Solution 2:** Bypass hook (not recommended)
```bash
git commit --no-verify
```

## Linting Rules

### Error Checking
- All errors must be handled
- No ignored errors
- Type assertions must be checked

### Code Style
- Consistent formatting
- Organized imports
- Proper naming conventions

### Security
- No hardcoded credentials
- Proper input validation
- Safe subprocess execution

### Performance
- Avoid unnecessary allocations
- Efficient algorithms
- Proper resource management

### Complexity
- Functions under 15 cyclomatic complexity
- Packages under 10 average complexity
- Clear, readable code

## Best Practices

### 1. Lint Early and Often
```bash
# After each change
make lint
```

### 2. Fix Issues Immediately
```bash
# Don't accumulate lint issues
make lint-fix
```

### 3. Review Lint Output
```bash
# Understand what needs fixing
make lint-verbose
```

### 4. Use Pre-Commit Hook
```bash
# Prevents bad commits
# Automatically runs before commit
```

### 5. Keep Configuration Updated
```bash
# Review .golangci.yml regularly
# Update rules as needed
```

## Integration with CI/CD

Local linting is integrated with GitHub Actions:

1. **Local Check**: Run `make lint` before pushing
2. **Pre-Commit Hook**: Prevents bad commits
3. **GitHub CI**: Runs full linting on push
4. **GitHub PR**: Checks all pull requests

### Workflow

```
Local Development
    ↓
make lint (local check)
    ↓
Pre-Commit Hook (automatic)
    ↓
git push
    ↓
GitHub CI (automatic)
    ↓
GitHub PR (if applicable)
```

## Development Setup

### Initial Setup

```bash
# Install dependencies
make dev-setup

# This installs:
# - golangci-lint
# - air (hot reload)
# - Other dev tools
```

### Daily Workflow

```bash
# Start development
make dev

# In another terminal, run linting
make lint

# Fix issues
make lint-fix

# Commit changes
git add .
git commit -m "message"
```

## Troubleshooting

### Linting Takes Too Long

**Solution:** Check for large files or complex code
```bash
# Run with verbose output
make lint-verbose

# Increase timeout
scripts/lint.sh -t 10m
```

### Pre-Commit Hook Not Running

**Solution:** Ensure hook is executable
```bash
chmod +x .git/hooks/pre-commit
```

### Lint Passes Locally but Fails in CI

**Solution:** Check Go version and dependencies
```bash
# Verify Go version
go version

# Update dependencies
go mod tidy
go mod download
```

## Advanced Usage

### Generate Lint Report

```bash
scripts/lint.sh -r
cat lint-report.txt
```

### Custom Configuration

```bash
scripts/lint.sh -c custom-config.yml
```

### Verbose Debugging

```bash
VERBOSE=true scripts/lint.sh
```

## Performance Metrics

### Typical Lint Times

- **Small changes**: 5-10 seconds
- **Full codebase**: 30-60 seconds
- **With report**: 60-90 seconds

### Optimization Tips

1. **Staged files only**: Pre-commit hook is faster
2. **Parallel execution**: Multiple linters run in parallel
3. **Caching**: Results cached between runs

## References

- [golangci-lint Documentation](https://golangci-lint.run/)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Effective Go](https://golang.org/doc/effective_go)

## Support

For issues or questions:

1. Check this guide
2. Review `.golangci.yml` configuration
3. Check GitHub Issues
4. Ask in team chat

## Summary

Local linting ensures code quality before pushing to GitHub:

- ✅ Run `make lint` before committing
- ✅ Use `make lint-fix` to auto-fix issues
- ✅ Pre-commit hook prevents bad commits
- ✅ Consistent with GitHub CI/CD
- ✅ Improves code quality

---

**Last Updated**: 2025-01-29  
**Version**: 1.0.0  
**Status**: ✅ Complete
