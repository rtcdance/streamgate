#!/usr/bin/env bash
set -euo pipefail
: "${W3_REDIS_ADDR:?BLOCKED: a real Redis address is required for contract-wallet acceptance}"
output="${1:?usage: run_wallet_session_tests.sh OUTPUT_DIRECTORY}"
mkdir -p "$output"
rc=0
go test -race -count=1 -p 2 -timeout 5m -tags=anvil,integration -json \
  -run '^(TestCWLoginLifecycle_RealRedisAnvil|TestCWProof_ConcurrentChainsAnvil)$' \
  ./test/integration/web3 > "$output/wallet-session-tests.json" 2> "$output/wallet-session-tests.stderr" || rc=$?
printf '%s\n' "$rc" > "$output/wallet-session-tests.exit"
cat "$output/wallet-session-tests.stderr"
python3 scripts/cd/assert_required_tests.py --suite wallet-session-tests --exit-code "$rc" "$output/wallet-session-tests.json"
