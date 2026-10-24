#!/usr/bin/env bash
#
# Run ONE fuzz target for its mutation budget, fail-closed.
#
# The gate's assertion is "the target found no failing input in <seconds>". A
# shared or emulated host can expire the coordinator's own deadline while a
# starved worker is being descheduled: go then exits non-zero with
# "context deadline exceeded" and writes NO failing input. That is a statement
# about the scheduler, not about the target, so it is retried ONCE on a single
# worker — the full budget still runs and any real crash still fails. Every
# other non-zero exit is propagated immediately.
#
# Usage: run-fuzz-target.sh <package-dir> <FuzzTarget> [seconds]
set -euo pipefail

package_dir="${1:?usage: run-fuzz-target.sh <package-dir> <FuzzTarget> [seconds]}"
target="${2:?usage: run-fuzz-target.sh <package-dir> <FuzzTarget> [seconds]}"
# Default only when the argument is absent; an explicitly empty value is a
# caller bug and must not silently become a full run.
seconds="${3-30}"
case "$seconds" in
  '' | *[!0-9]*) echo "fuzz seconds must be a positive integer, got '$seconds'" >&2; exit 2 ;;
esac
[ "$seconds" -gt 0 ] || { echo "fuzz seconds must be positive, got $seconds" >&2; exit 2; }

log="$(mktemp)"
trap 'rm -f "$log"' EXIT

if go test -run "^$" -fuzz "^${target}\$" -fuzztime "${seconds}s" "./$package_dir" >"$log" 2>&1; then
  cat "$log"
  exit 0
fi

# Only the coordinator shutdown race documented in TRANSITION_PLAN section 0.2
# qualifies: real progress was made, the deadline expired, and nothing else went
# wrong. A crash, a seed-corpus failure, a panic, a hung worker or a broken
# coordinator handshake is final and must never be retried.
if ! grep -q "fuzz: elapsed:" "$log"; then
  cat "$log"
  exit 1
fi
if grep -qE "Failing input written to|failure while testing seed corpus entry|panic:|hung or terminated unexpectedly|communicating with fuzzing coordinator" "$log"; then
  cat "$log"
  exit 1
fi

if grep -q "context deadline exceeded" "$log"; then
  cat "$log"
  echo "RETRY: $package_dir/$target hit the fuzz deadline with no failing input; retrying once with -parallel=1" >&2
  go test -run "^$" -fuzz "^${target}\$" -fuzztime "${seconds}s" -parallel=1 "./$package_dir"
  exit $?
fi

cat "$log"
exit 1
