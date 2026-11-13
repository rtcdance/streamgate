#!/usr/bin/env bash
# Capture the existing coverage run once; individual CW evidence must come
# from that same execution, rather than a second targeted run or a PASS summary.
set -euo pipefail
output="${1:?usage: run_unit_tests.sh OUTPUT_DIRECTORY}"
mkdir -p "$output"
# ci-packages.sh keeps no-test-file packages (docs/, migrations/, serviceerrors)
# for the plain go test coverage gate; the required-tests checker below demands
# every streamed package complete "pass" while `go test -json` ends those with
# a package-level "skip". Stream only packages that actually carry tests.
all_packages="$(./scripts/ci-packages.sh | grep -v '^github.com/rtcdance/streamgate/test/performance$')"
packages="$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' $all_packages)"
if [ -z "$packages" ]; then
  echo "✗ no packages with tests selected for the required unit gate" >&2
  exit 1
fi
rc=0
go test -race -count=1 -json -covermode=atomic -coverprofile="$output/coverage.out" \
    $packages > "$output/unit-tests.json" 2> "$output/unit-tests.stderr" || rc=$?
printf '%s\n' "$rc" > "$output/unit-tests.exit"
cat "$output/unit-tests.stderr"
python3 scripts/cd/assert_required_tests.py --suite wallet-unit-tests --exit-code "$rc" "$output/unit-tests.json"
