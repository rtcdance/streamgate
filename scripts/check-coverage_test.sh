#!/usr/bin/env bash
#
# Regression harness for scripts/check-coverage.sh.
#
# The gate only has value if it FAILS on the inputs it is supposed to reject.
# Each case below asserts an expected exit status, so a future change that
# silently loosens the gate turns this harness red.
#
# Usage: scripts/check-coverage_test.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECK="$HERE/check-coverage.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

pass=0
fail=0

# profile <name> <covered> <uncovered> — writes a two-line coverage profile.
profile() {
  local name="$1" covered="$2" uncovered="$3"
  {
    echo "mode: set"
    if [ "$covered" -gt 0 ]; then
      echo "example.com/m/a.go:1.1,2.2 $covered 1"
    fi
    if [ "$uncovered" -gt 0 ]; then
      echo "example.com/m/b.go:3.1,4.2 $uncovered 0"
    fi
  } >"$WORK/$name"
}

# expect <want: pass|fail> <label> <args...>
expect() {
  local want="$1" label="$2"
  shift 2
  local rc=0
  "$CHECK" "$@" >/dev/null 2>&1 || rc=$?
  if { [ "$want" = pass ] && [ "$rc" -eq 0 ]; } || { [ "$want" = fail ] && [ "$rc" -ne 0 ]; }; then
    echo "✓ $label"
    pass=$((pass + 1))
  else
    echo "✗ $label (wanted $want, got exit $rc)" >&2
    fail=$((fail + 1))
  fi
}

# --- boundary: exactly the floor passes, a hair below fails ---
profile exact 600 400
expect pass "600/1000 = 60% meets a 60% floor" "$WORK/exact" 60

profile just_below 599 400
expect fail "599/1000 = 59.9% is below a 60% floor" "$WORK/just_below" 60

# The trap this gate exists for: go tool cover rounds 59.96% up to a displayed
# "60.0%", which would pass a display-based check. Integer maths rejects it.
profile rounds_up 5996 4004
expect fail "5996/10000 = 59.96% (displays as 60.0%) still fails" "$WORK/rounds_up" 60

# The plan's explicit case: a near-60% ratio over 10001 statements.
profile big 6000 4001
expect fail "6000/10001 statements fails a 60% floor" "$WORK/big" 60

profile full 100 0
expect pass "100/100 = 100% passes" "$WORK/full" 60

profile zero 0 100
expect fail "0/100 = 0% fails" "$WORK/zero" 60

# --- a custom floor is honoured, not ignored ---
profile custom 85 15
expect pass "85/100 passes an 80% floor" "$WORK/custom" 80
expect fail "85/100 fails a 90% floor" "$WORK/custom" 90

# --- unusable inputs are hard failures, never silent passes ---
expect fail "missing profile file" "$WORK/does-not-exist" 60

: >"$WORK/empty"
expect fail "empty profile file" "$WORK/empty" 60

echo "mode: set" >"$WORK/header_only"
expect fail "header-only profile (no statements)" "$WORK/header_only" 60

echo "not a coverage profile" >"$WORK/garbage"
expect fail "malformed profile (no mode: header)" "$WORK/garbage" 60

expect fail "non-integer floor ('60%')" "$WORK/exact" "60%"
expect fail "out-of-range floor (0)" "$WORK/exact" 0
expect fail "out-of-range floor (101)" "$WORK/exact" 101

echo
echo "check-coverage harness: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
