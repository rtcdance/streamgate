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

first_rc=0
go test -run "^$" -fuzz "^${target}\$" -fuzztime "${seconds}s" "./$package_dir" >"$log" 2>&1 || first_rc=$?
if [ "$first_rc" = 0 ]; then
  cat "$log"
  exit 0
fi

# Only the documented Go coordinator shutdown shape qualifies. A substring
# plus a blacklist cannot distinguish that shape from a mixed assertion or
# infrastructure failure. Reject unknown lines, other targets, partial output,
# repeated diagnostics and non-test exits; never convert those into a retry PASS.
if [ "$first_rc" = 1 ] && awk -v target="$target" '
  { sub(/\r$/, ""); sub(/[[:space:]]+$/, "") }
  /^[[:space:]]*$/ { next }
  /^fuzz: elapsed: / {
    if (failed) { bad=1; next }
    if ($0 ~ /^fuzz: elapsed: [0-9.hms]+, gathering baseline coverage: [0-9]+\/[0-9]+ completed(, now fuzzing with [0-9]+ workers)?$/) next
    if ($0 ~ /^fuzz: elapsed: [0-9.hms]+, execs: [0-9]+ \([0-9.]+\/sec\)(, new interesting: [0-9]+ \(total: [0-9]+\))?$/) {
      if ($0 ~ /execs: [1-9][0-9]* /) progress=1
      next
    }
    bad=1; next
  }
  /^=== RUN[[:space:]]/ {
    name=$0; sub(/^=== RUN[[:space:]]+/, "", name)
    if (failed || name != target) bad=1
    next
  }
  /^--- FAIL: / {
    name=$0; sub(/^--- FAIL: /, "", name); sub(/ \([0-9.]+s\)$/, "", name)
    if (failed || name != target || $0 !~ / \([0-9.]+s\)$/) bad=1
    failed++; next
  }
  /^[[:space:]]+context deadline exceeded$/ {
    if (failed != 1 || deadline) bad=1
    deadline++; next
  }
  /^=== NAME([[:space:]]|$)/ {
    name=$0; sub(/^=== NAME[[:space:]]*/, "", name)
    if (!deadline || (name != "" && name != target)) bad=1
    next
  }
  /^FAIL$/ { if (!deadline || overall++) bad=1; next }
  /^exit status 1$/ { if (!overall || exited++) bad=1; next }
  /^FAIL[[:space:]]+[^[:space:]]+[[:space:]]+[0-9.]+s$/ {
    if (!overall || summary++) bad=1
    next
  }
  !failed && !progress && /^[a-z0-9][a-zA-Z0-9_.~+\/-]*$/ { next }
  !failed && !progress && /^go: downloading [^[:space:]]+ [^[:space:]]+$/ { next }
  { bad=1 }
  END { exit !(progress && failed == 1 && deadline == 1 && overall == 1 && summary == 1 && !bad) }
' "$log"; then
  cat "$log"
  echo "RETRY: $package_dir/$target hit the fuzz deadline with no failing input; retrying once with -parallel=1" >&2
  go test -run "^$" -fuzz "^${target}\$" -fuzztime "${seconds}s" -parallel=1 "./$package_dir"
  exit $?
fi

cat "$log"
exit "$first_rc"
