#!/usr/bin/env bash
#
# Single source of truth for the package set measured by the Go coverage gate.
#
# Both `make test-ci` and .github/workflows/ci.yml consume this so the local and
# CI gates can never drift apart: if the set changes, it changes in one place.
#
# The excluded trees are measured by dedicated jobs (e2e, load, chain
# integration), not by the unit-coverage gate. A tree that must be excluded is
# listed here explicitly; nothing is excluded implicitly.
set -euo pipefail

# Fail loudly if the package list itself is empty: an empty set would make the
# coverage gate vacuously pass.
packages="$(go list ./... \
  | grep -v '/test/e2e' \
  | grep -v '/test/load' \
  | grep -v '/test/performance' \
  | grep -v '/test/integration/web3')"

# test/performance is test-only files, so it contributes no coverage
# statements; it is excluded because it holds wall-clock threshold tests that
# must run alone (`go test -p 1`): inside the ~90-package parallel batch they
# observe scheduler contention, not the code (measured 104ms vs 50ms steady).
# verify-docker's unit stage and `make test-ci` run it serially afterwards; the
# CI perf job runs it with -p 1.

if [ -z "$packages" ]; then
  echo "✗ no packages selected for the coverage gate" >&2
  exit 1
fi

printf '%s\n' "$packages"
