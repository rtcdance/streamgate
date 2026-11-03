#!/usr/bin/env bash
# Required wallet-session storage semantics on a REAL Redis for the durability
# profile (plan §10 W3-05 / CW-12). Unlike the chain variant this suite needs
# no Anvil: the storage contract is chain-independent. A missing Redis address
# is a configuration failure, never a skip.
set -euo pipefail
: "${W3_REDIS_ADDR:?BLOCKED: a real Redis address is required for the durability wallet-session gate}"
output="${1:?usage: run_wallet_session_durability_tests.sh OUTPUT_DIRECTORY}"
mkdir -p "$output"
rc=0
go test -race -count=1 -timeout 3m -tags=integration -json \
  -run '^TestWalletSession_RealRedisDurability$' \
  ./pkg/storage > "$output/wallet-session-durability.json" 2> "$output/wallet-session-durability.stderr" || rc=$?
printf '%s\n' "$rc" > "$output/wallet-session-durability.exit"
cat "$output/wallet-session-durability.stderr"
python3 scripts/cd/assert_required_tests.py --suite wallet-session-durability-tests --exit-code "$rc" "$output/wallet-session-durability.json"
