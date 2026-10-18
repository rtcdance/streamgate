#!/usr/bin/env bash
#
# Fail-closed runner for the REQUIRED fuzz contract (TRANSITION_PLAN section 0.2 D).
#
# A blanket "every Fuzz target" pass would keep succeeding if a required target
# were renamed or deleted, so the required set is registered in
# scripts/cd/required-fuzz.json and asserted here. Every entry must:
#   * exist as a `func <Name>(_ *testing.F)` in the named package, and
#   * complete its mutation budget with a result that is not a failure.
# An unknown exit status, a crasher, or a run that exceeds its budget is a
# failure, never a skip. The seed corpus runs in the ordinary `go test` gate;
# this runner is the mutation half of the contract.
#
# Usage: run-required-fuzz.sh <fuzz-seconds>      (PR: 5, final candidate: 30)
#   FUZZ_LOG_DIR            where logs and the identity list land (default: /artifacts)
#   REQUIRED_FUZZ_MANIFEST  override the manifest path
set -euo pipefail

seconds="${1:?usage: run-required-fuzz.sh <fuzz-seconds>}"
case "$seconds" in
  '' | *[!0-9]*) echo "fuzz seconds must be a positive integer, got '$seconds'" >&2; exit 2 ;;
esac
[ "$seconds" -gt 0 ] || { echo "fuzz seconds must be positive, got $seconds" >&2; exit 2; }

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
manifest="${REQUIRED_FUZZ_MANIFEST:-$here/required-fuzz.json}"
log_dir="${FUZZ_LOG_DIR:-/artifacts}"
module="github.com/rtcdance/streamgate/"

mkdir -p "$log_dir"
plan="$log_dir/required-fuzz.plan"
identities="$log_dir/required-fuzz.identities"
: > "$identities"

# The manifest itself is validated fail-closed: an empty or malformed required
# set must never make this gate vacuously pass.
python3 - "$manifest" <<'PY' > "$plan"
import json
import sys

document = json.load(open(sys.argv[1], encoding="utf-8"))
entries = document.get("required")
if not isinstance(entries, list) or not entries:
    raise SystemExit("required fuzz manifest is empty")
seen = set()
for entry in entries:
    package, fuzz = entry.get("package"), entry.get("fuzz")
    if not isinstance(package, str) or not package:
        raise SystemExit(f"invalid required fuzz entry (package): {entry!r}")
    if not isinstance(fuzz, str) or not fuzz.startswith("Fuzz"):
        raise SystemExit(f"invalid required fuzz entry (fuzz): {entry!r}")
    if (package, fuzz) in seen:
        raise SystemExit(f"duplicate required fuzz identity: {package}:{fuzz}")
    seen.add((package, fuzz))
    print(package, fuzz)
PY

budget=$((seconds + 180))
failed=0
while read -r package fuzz <&3; do
  dir="./${package#"$module"}"

  # The target must exist, or the contract is broken even if every other fuzz
  # target in the tree still passes.
  pattern="^func ${fuzz}\([a-zA-Z_][a-zA-Z0-9_]* \*testing\.F\)"
  if ! grep -rqE "$pattern" --include='*_test.go' "$dir" 2>/dev/null; then
    echo "GATE FAILURE: required fuzz target ${fuzz} is missing from ${dir}" >&2
    failed=1
    continue
  fi

  # Record the identity so a blanket pass elsewhere can skip a duplicate run.
  printf '%s %s\n' "${dir#./}" "$fuzz" >> "$identities"

  log="$log_dir/required-fuzz-${fuzz}.log"
  echo "-- required fuzz ${package}:${fuzz} (seeds + ${seconds}s mutation) --"
  rc=0
  go test -count=1 -run '^$' -fuzz "^${fuzz}\$" \
    -fuzztime "${seconds}s" -timeout "${budget}s" "$dir" >"$log" 2>&1 || rc=$?

  # A deadline message and progress output cannot establish successful
  # completion. Preserve every nonzero result, including coordinator shutdown
  # failures, so infrastructure failures cannot silently satisfy this gate.
  if [ "$rc" -eq 0 ]; then
    echo "   PASS ${fuzz} (budget ${budget}s; log: ${log})"
  else
    echo "GATE FAILURE: required fuzz ${fuzz} exited ${rc}; log: ${log}" >&2
    tail -n 25 "$log" >&2 || true
    failed=1
  fi
done 3< "$plan"

if [ "$failed" -ne 0 ]; then
  echo "GATE FAILURE: the required fuzz contract did not pass" >&2
  exit 1
fi
echo "required fuzz: every registered target completed (${seconds}s mutation each)"
