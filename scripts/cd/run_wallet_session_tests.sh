#!/usr/bin/env bash
set -euo pipefail
: "${W3_REDIS_ADDR:?BLOCKED: a real Redis address is required for contract-wallet acceptance}"
output="${1:?usage: run_wallet_session_tests.sh OUTPUT_DIRECTORY}"
mkdir -p "$output"
# The go test process runs with the package directory as its cwd, so any
# relative evidence path would land under test/integration/web3/; hand the
# suites an absolute directory instead.
output="$(cd "$output" && pwd)"
: "${W3_CANDIDATE_SHA:=${GITHUB_SHA:-}}"
: "${W3_CANDIDATE_SHA:?BLOCKED: exact source candidate required for RPC evidence}"
rc=0
W3_SEGMENT_RPC_OUTPUT="$output/wallet-segment-rpc.json" \
W3_EVIDENCE_CONTEXT="${W3_EVIDENCE_CONTEXT:-CANDIDATE}" W3_CANDIDATE_SHA="$W3_CANDIDATE_SHA" \
go test -race -count=1 -p 2 -timeout 5m -tags=anvil,integration -json \
  -run '^(TestCWLoginLifecycle_RealRedisAnvil|TestCWProof_ConcurrentChainsAnvil)$' \
  ./test/integration/web3 > "$output/wallet-session-tests.json" 2> "$output/wallet-session-tests.stderr" || rc=$?
printf '%s\n' "$rc" > "$output/wallet-session-tests.exit"
cat "$output/wallet-session-tests.stderr"
python3 scripts/cd/assert_required_tests.py --suite wallet-session-tests --exit-code "$rc" "$output/wallet-session-tests.json"
