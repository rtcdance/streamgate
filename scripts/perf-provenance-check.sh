#!/usr/bin/env bash
# ============================================================
# StreamGate SG-07 — perf result provenance gate
# ============================================================
# Usage: perf-provenance-check.sh <result_dir> <matrix_repeats>
#
# A perf result is an acceptance artefact only if it is bound to the build that
# was actually measured. This gate exists because the weaker form — "the JSON has
# the fields" — cannot tell this run's numbers from an earlier run's: a stale
# `warm_cache-viewers-1.json` left in the result directory satisfies it on its
# own. The gate therefore requires, in addition:
#
#   1. every *-viewers-*.json under <result_dir> carries its provenance fields;
#   2. ALL of them are stamped with ONE commit, and that commit is the HEAD of
#      this checkout (`git rev-parse HEAD` — the same fallback the harness uses)
#      — so a file inherited from another run or another build fails here;
#   3. the four fault-matrix scenarios are all present;
#   4. when the matrix was repeated (matrix_repeats > 1), every round-N
#      directory holds the full warm set (1/10/50) — a repeat that silently did
#      not happen is a failure, not a gap — and the result directory root holds
#      NO warm_cache result, which is what proves the pre-run clean-up worked.
#
# Non-zero exit fails the perf stage. The companion step in verify-docker.sh
# removes the previous run's result files before measuring, so requirement 4's
# stale-file check covers the interval the clean-up cannot: files written by a
# second process (or an interrupted run) while this one is in flight.
# ============================================================
set -euo pipefail

RESULT_DIR="${1:?usage: perf-provenance-check.sh <result_dir> <matrix_repeats>}"
MATRIX_REPEATS="${2:-1}"
[ -d "$RESULT_DIR" ] || { echo "perf-provenance: FAIL: $RESULT_DIR does not exist" >&2; exit 1; }

# The harness stamps cfg override > PERF_COMMIT > `git rev-parse HEAD`; the
# candidate side of the perf stage sets neither override, so HEAD is the only
# build these results may be attributed to.
HEAD_SHA="$(git rev-parse HEAD)" || { echo "perf-provenance: FAIL: cannot resolve HEAD" >&2; exit 1; }

python3 - "$RESULT_DIR" "$MATRIX_REPEATS" "$HEAD_SHA" <<'PY'
import glob, json, os, sys

root, repeats, head = sys.argv[1], int(sys.argv[2]), sys.argv[3]
FIELDS = ("commit", "scenario", "content_id", "viewers", "success_count", "fixture_hash")
REQUIRED_SCENARIOS = {"warm_cache", "cold_cache", "rpc_primary_down", "rpc_all_down"}
WARM_SET = {1, 10, 50}

# The matrix may write straight into the result dir (repeats = 1) or into
# per-repeat round-N sub-directories; both layouts must be covered.
files = sorted(glob.glob(os.path.join(root, "**", "*-viewers-*.json"), recursive=True))
if not files:
    sys.exit(f"no scenario result files found under {root}")

records = {}
for f in files:
    try:
        r = json.load(open(f))
    except Exception as exc:
        sys.exit(f"{f}: not readable as JSON ({exc})")
    for key in FIELDS:
        if r.get(key) in (None, "", []):
            sys.exit(f"{f}: missing provenance field {key!r}")
    records[os.path.relpath(f, root)] = r

for rel, r in sorted(records.items()):
    print(f"provenance ok: {rel} commit={str(r['commit'])[:12]} "
          f"scenario={r['scenario']} success={r['success_count']} "
          f"errors={r['error_count']} fixture_hash={str(r['fixture_hash'])[:16]}")

commits = sorted({r["commit"] for r in records.values()})
if len(commits) != 1:
    sys.exit(f"results are stamped with more than one commit {commits} — "
             f"a stale result from an earlier run was counted")
if commits[0] != head:
    sys.exit(f"results are stamped {commits[0]} but HEAD is {head} — "
             f"they do not measure this candidate")

scenarios = {r["scenario"] for r in records.values()}
missing = sorted(REQUIRED_SCENARIOS - scenarios)
if missing:
    sys.exit(f"the fault matrix is incomplete — no result for: {missing}")

if repeats > 1:
    for rep in range(1, repeats + 1):
        d = f"round-{rep}"
        warm = {r["viewers"] for rel, r in records.items()
                if rel.split(os.sep, 1)[0] == d and r["scenario"] == "warm_cache"}
        if not warm:
            sys.exit(f"{d}: no warm_cache result — repeat {rep} did not happen")
        gap = sorted(WARM_SET - warm)
        if gap:
            sys.exit(f"{d}: warm matrix incomplete — no result for viewers {gap}")
    root_scenarios = {r["scenario"] for rel, r in records.items() if os.sep not in rel}
    if "warm_cache" in root_scenarios:
        sys.exit("the result dir root holds a warm_cache result while the matrix was "
                 "repeated — a stale file from another run survived; run with a clean "
                 "result directory")
    print(f"provenance: {repeats} repeat(s) × warm {sorted(WARM_SET)} complete, "
          f"fault matrix complete, single commit {head}")

print(f"provenance: PASS ({len(records)} result file(s) bound to {head[:12]})")
PY
