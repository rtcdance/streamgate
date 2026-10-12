#!/usr/bin/env bash
#
# Enforce a total-statement coverage floor from a Go coverage profile.
#
# This is the single gate used by both `make test-ci` and CI. It deliberately
# does NOT use `go tool cover -func` + `bc`: that path prints a rounded
# percentage, so 59.6% can display as "60.0%" and slip through, and `bc` is not
# guaranteed present. Here the comparison is pure integer arithmetic over the
# profile's own statement counts:
#
#     covered * 100 >= total * MIN_PERCENT
#
# Every unusable input (missing, empty, header-only, malformed, zero
# statements, non-integer floor) is a hard failure — never a silent pass.
#
# Usage: check-coverage.sh [profile] [min-percent]
set -euo pipefail

PROFILE="${1:-coverage.out}"
MIN_PERCENT="${2:-60}"

fail() {
  echo "✗ coverage gate: $*" >&2
  exit 1
}

# The floor must be a positive integer; a typo like "60%" or "abc" is a
# misconfiguration, not a reason to skip the gate.
case "$MIN_PERCENT" in
  '' | *[!0-9]*) fail "min percent must be a positive integer, got '$MIN_PERCENT'" ;;
esac
if [ "$MIN_PERCENT" -le 0 ] || [ "$MIN_PERCENT" -gt 100 ]; then
  fail "min percent must be within 1..100, got '$MIN_PERCENT'"
fi

[ -f "$PROFILE" ] || fail "profile not found: $PROFILE"
[ -s "$PROFILE" ] || fail "profile is empty: $PROFILE"
head -n 1 "$PROFILE" | grep -q '^mode:' || fail "profile is missing a 'mode:' header: $PROFILE"

# Profile lines look like:
#   github.com/x/y/file.go:10.20,12.5 2 1
# where $2 is the statement count and $3 is the execution count. Coverage is
# counted in STATEMENTS, matching `go tool cover` semantics.
COUNTS="$(awk '
  NR > 1 && NF >= 3 {
    stmts = $2 + 0
    total += stmts
    if ($3 + 0 > 0) covered += stmts
  }
  END { printf "%d %d", covered + 0, total + 0 }
' "$PROFILE")"
COVERED="${COUNTS%% *}"
TOTAL="${COUNTS##* }"

[ "$TOTAL" -gt 0 ] || fail "profile records no statements (total=0): $PROFILE"

# Integer-only comparison: never rely on a rounded display value.
if [ $((COVERED * 100)) -lt $((TOTAL * MIN_PERCENT)) ]; then
  printf '✗ coverage gate: %d/%d statements = %d%% (truncated) is below %s%%\n' \
    "$COVERED" "$TOTAL" "$((COVERED * 100 / TOTAL))" "$MIN_PERCENT" >&2
  exit 1
fi

printf '✓ coverage gate: %d/%d statements = %d%% meets %s%%\n' \
  "$COVERED" "$TOTAL" "$((COVERED * 100 / TOTAL))" "$MIN_PERCENT"
