#!/usr/bin/env bash
# Generates the ContentRegistry DEPLOYMENT RECORD (TRANSITION_PLAN batch C:
# "部署清单记录 chainID、address、deployTx、runtimeCodeHash、ABIHash、编译器和
# artifact 身份").
#
# WHY IT EXISTS
#   The running image is verified against this record at startup: the chain id,
#   that the address holds non-empty runtime code with the recorded code hash,
#   and that the ABI the image implements hashes to the recorded ABI hash. A
#   record that disagrees with the image or the chain is a startup failure, not a
#   warning — the alternative is an image that reverts every registration.
#
# WHAT IT DOES
#   1. builds the contracts (the record's ABI hash comes from the real artifact),
#   2. deploys ContentRegistry on the target RPC (or reads an existing address),
#   3. records deployTx, the deployed runtime code hash, the compiler identity and
#      the artifact (path + sha256),
#   4. writes deployments/content-registry.<chainID>.json.
#
# The ABI hash is the canonical hash defined in
# pkg/web3/contract/registry_manifest.go (CanonicalABIHash) and implemented here
# in Python: one line per entry
#   <type>\t<name>\t<inputs types>\t<outputs types>\t<stateMutability>
# sorted and SHA-256 hashed. A Go test asserts the checked-in record equals the
# runtime ABI hash, so any divergence between this implementation and the Go one
# fails the build rather than passing silently.
#
# Usage:
#   scripts/registry-manifest.sh [--rpc URL] [--chain-id N] [--address ADDR]
#                                [--deployer-key KEY] [--out PATH]
# Defaults target a local Anvil (http://localhost:8545, chain 31337); with no
# --address the script starts a throwaway Anvil and deploys the contract itself.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

RPC="${REGISTRY_MANIFEST_RPC:-http://localhost:8545}"
CHAIN_ID="${REGISTRY_MANIFEST_CHAIN_ID:-31337}"
ADDRESS=""
OUT=""
# Anvil account 0 — the same key the demo stack uses to deploy and sign.
DEPLOYER_KEY="${REGISTRY_MANIFEST_DEPLOYER_KEY:-0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80}"
# Deployed before ContentRegistry so the registry's address matches the demo
# stack's deterministic address. Empty disables the first deploy.
FIRST_DEPLOY="${REGISTRY_MANIFEST_FIRST_DEPLOY:-DemoNFT}"
ARTIFACT="contracts/out/ContentRegistry.sol/ContentRegistry.json"

while [ $# -gt 0 ]; do
    case "$1" in
        --rpc) RPC="$2"; shift 2 ;;
        --chain-id) CHAIN_ID="$2"; shift 2 ;;
        --address) ADDRESS="$2"; shift 2 ;;
        --deployer-key) DEPLOYER_KEY="$2"; shift 2 ;;
        --out) OUT="$2"; shift 2 ;;
        -h|--help) sed -n '2,35p' "$0"; exit 0 ;;
        *) printf 'unknown argument: %s\n' "$1" >&2; exit 2 ;;
    esac
done

[ -n "$OUT" ] || OUT="deployments/content-registry.${CHAIN_ID}.json"

for tool in forge cast python3; do
    command -v "$tool" >/dev/null 2>&1 || { printf 'FATAL: %s is required\n' "$tool" >&2; exit 1; }
done

printf '== building contracts (the ABI hash comes from the real artifact) ==\n'
( cd contracts && forge build >/dev/null )
[ -s "$ARTIFACT" ] || { printf 'FATAL: %s missing after forge build\n' "$ARTIFACT" >&2; exit 1; }

STARTED_ANVIL=""
cleanup() { [ -n "$STARTED_ANVIL" ] && kill "$STARTED_ANVIL" 2>/dev/null || true; }
trap cleanup EXIT

if ! cast chain-id --rpc-url "$RPC" >/dev/null 2>&1; then
    printf '== no chain at %s: starting a throwaway Anvil for the deployment ==\n' "$RPC"
    anvil --chain-id "$CHAIN_ID" --silent >/dev/null 2>&1 &
    STARTED_ANVIL=$!
    for _ in $(seq 1 30); do
        cast chain-id --rpc-url "$RPC" >/dev/null 2>&1 && break
        sleep 1
    done
    cast chain-id --rpc-url "$RPC" >/dev/null 2>&1 || { printf 'FATAL: anvil did not start\n' >&2; exit 1; }
fi

ACTUAL_CHAIN_ID="$(cast chain-id --rpc-url "$RPC")"
if [ "$ACTUAL_CHAIN_ID" != "$CHAIN_ID" ]; then
    printf 'FATAL: %s serves chain %s but the record was requested for %s\n' "$RPC" "$ACTUAL_CHAIN_ID" "$CHAIN_ID" >&2
    exit 1
fi

DEPLOY_TX=""
if [ -z "$ADDRESS" ]; then
    # The demo stack deploys DemoNFT first and ContentRegistry second, which is
    # what makes the registry land on its deterministic 0xe7f1725E… address.
    # Recording a record for a DIFFERENT address than the stack actually runs
    # would be worse than useless, so the script reproduces that order.
    if [ -n "$FIRST_DEPLOY" ]; then
        printf '== deploying %s first (reproducing the demo stack deploy order) ==\n' "$FIRST_DEPLOY"
        ( cd contracts && forge create \
            --rpc-url "$RPC" \
            --private-key "$DEPLOYER_KEY" \
            --json --legacy --broadcast \
            "src/${FIRST_DEPLOY}.sol:${FIRST_DEPLOY}" \
            --out /tmp/registry-manifest-out \
            --cache-path /tmp/registry-manifest-cache >/dev/null )
    fi
    printf '== deploying ContentRegistry ==\n'
    DEPLOY_JSON="$(cd contracts && forge create \
        --rpc-url "$RPC" \
        --private-key "$DEPLOYER_KEY" \
        --json --legacy --broadcast \
        src/ContentRegistry.sol:ContentRegistry \
        --out /tmp/registry-manifest-out \
        --cache-path /tmp/registry-manifest-cache)"
    ADDRESS="$(printf '%s' "$DEPLOY_JSON" | python3 -c 'import sys,json;print(json.load(sys.stdin)["deployedTo"])')"
    DEPLOY_TX="$(printf '%s' "$DEPLOY_JSON" | python3 -c 'import sys,json;print(json.load(sys.stdin)["transactionHash"])')"
else
    printf '== using the existing ContentRegistry at %s ==\n' "$ADDRESS"
    # The deploy transaction is part of the record's provenance. Without a
    # deployment the script cannot invent one, so a recorded address must be
    # accompanied by its tx hash explicitly.
    DEPLOY_TX="${REGISTRY_MANIFEST_DEPLOY_TX:-unknown-for-preexisting-address}"
fi

CODE="$(cast code --rpc-url "$RPC" "$ADDRESS")"
[ "$CODE" != "0x" ] || { printf 'FATAL: no runtime code at %s — nothing to record\n' "$ADDRESS" >&2; exit 1; }
RUNTIME_CODE_HASH="$(cast keccak "$CODE")"

COMPILER="$(python3 - "$ARTIFACT" <<'PY'
import json, sys
with open(sys.argv[1]) as fh:
    art = json.load(fh)
meta = art.get("metadata")
if isinstance(meta, str):
    meta = json.loads(meta)
compiler = (meta or {}).get("compiler", {}).get("version", "unknown")
print("solc " + compiler)
PY
)"

ABI_HASH="$(python3 - "$ARTIFACT" <<'PY'
import hashlib, json, sys

# The canonical ABI hash defined in pkg/web3/contract/registry_manifest.go.
# Keep this byte-for-byte identical to that implementation: the Go test asserts
# the checked-in record matches the runtime ABI hash.
with open(sys.argv[1]) as fh:
    abi = json.load(fh)["abi"]

lines = []
for entry in abi:
    ins = ",".join(i.get("type", "") for i in entry.get("inputs") or [])
    outs = ",".join(o.get("type", "") for o in entry.get("outputs") or [])
    lines.append("\t".join([
        entry.get("type", ""),
        entry.get("name", "") or "",
        ins,
        outs,
        entry.get("stateMutability", "") or "",
    ]))
lines.sort()
print(hashlib.sha256("\n".join(lines).encode("utf-8")).hexdigest())
PY
)"

ARTIFACT_HASH="$(python3 -c 'import hashlib,sys;print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$ARTIFACT")"

mkdir -p "$(dirname "$OUT")"
python3 - "$OUT" "$CHAIN_ID" "$ADDRESS" "$DEPLOY_TX" "$RUNTIME_CODE_HASH" "$ABI_HASH" "$COMPILER" "$ARTIFACT" "$ARTIFACT_HASH" <<'PY'
import json, sys
out, chain_id, address, deploy_tx, code_hash, abi_hash, compiler, artifact, artifact_hash = sys.argv[1:10]
record = {
    "chain_id": int(chain_id),
    "address": address,
    "deploy_tx": deploy_tx,
    "runtime_code_hash": code_hash,
    "abi_hash": abi_hash,
    "compiler": compiler,
    "artifact": artifact,
    "artifact_hash": artifact_hash,
}
with open(out, "w") as fh:
    json.dump(record, fh, indent=2, sort_keys=False)
    fh.write("\n")
print("wrote " + out)
PY

printf '== deployment record ==\n'
cat "$OUT"
