#!/usr/bin/env python3
"""Build and verify the Sepolia deployment manifest (plan §10 TN-02).

The manifest binds one deployment of the W3 fixture contracts (Study1271Wallet
"controller wallet" and DemoNFT "N") to the compiled Foundry artifact: address,
owner, creation transaction, canonical receipt, and the runtime bytecode that
is actually on chain. Everything here is READ-ONLY: no transaction is ever
signed, broadcast or re-sent, and no Anvil deployment is ever accepted as the
public deployment.

Usage:
  deployment_manifest.py build --chain-rpc ENV_KEY --artifact PATH ... --output PATH
  deployment_manifest.py verify --manifest PATH ...

Both subcommands report BLOCKED/FAIL and exit nonzero instead of approximating:
a missing receipt, a code mismatch, or a foreign chain is never downgraded.
RPC endpoint values are never printed; only their SHA256 digests are recorded.
"""
import argparse
import hashlib
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

sys.dont_write_bytecode = True

# Reuse the read-only RPC client and the pinned network identity from the
# qualification script so both tools can never disagree about the chain.
_SCRIPT_DIR = Path(__file__).resolve().parent
import importlib.util

_spec = importlib.util.spec_from_file_location('wallet_network_preflight', _SCRIPT_DIR / 'network_preflight.py')
_preflight = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_preflight)
ReadOnlyRPC = _preflight.ReadOnlyRPC
CHAIN_ID = _preflight.CHAIN_ID
GENESIS = _preflight.GENESIS
READ_METHODS = _preflight.READ_METHODS
# Receipt reads are read-only but are not part of the qualification surface, so
# the manifest tool carries its own allow-list on top of the preflight's.
MANIFEST_READ_METHODS = READ_METHODS | {'eth_getTransactionReceipt'}

ROLES = ('wallet', 'nft')
# keccak256("owner()")[:4]; Study1271Wallet and DemoNFT are Ownable.
OWNER_SELECTOR = '0x8da5cb5b'


def _rpc_call(rpc, method, params):
    if method not in MANIFEST_READ_METHODS:
        raise AssertionError(f'write attempted: {method}')
    return rpc.call(method, params)


def _code_digest(code):
    if not isinstance(code, str) or not code.startswith('0x') or len(code) < 4:
        raise ValueError('malformed runtime bytecode from eth_getCode')
    return hashlib.sha256(bytes.fromhex(code[2:])).hexdigest(), code


def _artifact_identity(artifact_path):
    artifact = json.loads(Path(artifact_path).read_text())
    bytecode = artifact.get('bytecode', {})
    deployed = artifact.get('deployedBytecode', {})
    creation = bytecode.get('object') if isinstance(bytecode, dict) else bytecode
    runtime = deployed.get('object') if isinstance(deployed, dict) else deployed
    for field, value in (('creation', creation), ('runtime', runtime)):
        if not isinstance(value, str) or not value.startswith('0x'):
            raise ValueError(f'artifact has no {field} bytecode object')
    metadata = artifact.get('metadata') or {}
    return {
        'artifactPath': str(artifact_path),
        'artifactSHA256': hashlib.sha256(Path(artifact_path).read_bytes()).hexdigest(),
        'runtimeCodeSHA256': hashlib.sha256(bytes.fromhex(runtime[2:])).hexdigest(),
        'creationCodeSHA256': hashlib.sha256(bytes.fromhex(creation[2:])).hexdigest(),
        'compilerVersion': metadata.get('compiler', {}).get('version') if isinstance(metadata, dict) else None,
    }


def _receipt(rpc, tx_hash):
    if not isinstance(tx_hash, str) or not tx_hash.startswith('0x') or len(tx_hash) != 66:
        raise ValueError('creation transaction hash must be a 32-byte hex string')
    receipt = _rpc_call(rpc, 'eth_getTransactionReceipt', [tx_hash])
    if not isinstance(receipt, dict) or receipt.get('status') != '0x1':
        raise ValueError('creation transaction has no canonical success receipt')
    if not receipt.get('contractAddress'):
        raise ValueError('receipt does not create a contract')
    return receipt


def _on_chain_owner(rpc, address, block_tag):
    result = _rpc_call(rpc, 'eth_call', [{'to': address, 'data': OWNER_SELECTOR}, block_tag])
    if not isinstance(result, str) or len(result) != 66 or int(result[2:4], 16) not in (0x00, 0x01):
        raise ValueError('owner() call did not return an address word')
    word = result[2:]
    if any(ch != '0' for ch in word[:24]):
        raise ValueError('owner() word has non-zero ABI padding')
    return '0x' + word[24:]


def build(args, rpc=None):
    manifest = {
        'schemaVersion': 1,
        'scope': 'ENVIRONMENT_READ_ONLY',
        'deploymentVerified': False,
        'completeAcceptance': False,
        'at': datetime.now(timezone.utc).isoformat(),
        'chainId': CHAIN_ID,
        'genesis': GENESIS,
        'contracts': [],
    }
    primary = rpc if rpc is not None else ReadOnlyRPC(os.environ.get('W3_SEPOLIA_RPC_PRIMARY', ''), 0)
    try:
        # The deployment must live on the pinned Sepolia identity, established
        # freshly here rather than assumed from an earlier run.
        _preflight.network_head(primary)
        for spec in args.contract:
            role, address, owner_arg, tx_hash, artifact = spec
            if role not in ROLES:
                raise ValueError(f'unknown contract role {role}')
            receipt = _receipt(primary, tx_hash)
            if receipt['contractAddress'].lower() != address.lower():
                raise ValueError('receipt contractAddress does not match the recorded address')
            code_at_receipt = _rpc_call(primary, 'eth_getCode', [address, receipt['blockNumber']])
            digest, _ = _code_digest(code_at_receipt)
            owner_on_chain = _on_chain_owner(primary, address, 'safe')
            entry = {
                'role': role,
                'address': address,
                'owner': owner_arg,
                'creationTxHash': tx_hash,
                'creationBlock': _preflight.quantity(receipt['blockNumber']),
                'receiptBlockHex': receipt['blockNumber'],
                'runtimeCodeSHA256OnChain': digest,
            }
            if owner_on_chain.lower() != owner_arg.lower():
                raise ValueError('on-chain owner() does not match the recorded owner')
            identity = _artifact_identity(artifact)
            if identity['runtimeCodeSHA256'] != digest:
                raise ValueError('compiled artifact runtime bytecode does not match on-chain code')
            entry['artifact'] = identity
            manifest['contracts'].append(entry)
        addresses = [entry['address'].lower() for entry in manifest['contracts']]
        if len(addresses) != len(set(addresses)) or set(entry['role'] for entry in manifest['contracts']) != set(ROLES):
            raise ValueError('manifest must bind each role to a distinct address')
        manifest['deploymentVerified'] = True
        status = 'MANIFEST_RECORDED'
    except (ValueError, OSError, KeyError, TypeError) as error:
        status = 'BLOCKED'
        manifest['reason'] = str(error)
    finally:
        manifest['providers'] = [{'slot': primary.slot, 'endpointSHA256': hashlib.sha256(primary.endpoint.encode()).hexdigest(), 'events': primary.events}]
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(manifest, indent=2) + '\n')
    print(f'{status}: read-only deployment manifest; wallet flows are NOT verified by this tool')
    return 0 if status == 'MANIFEST_RECORDED' else 2


def verify(args, rpc=None):
    try:
        manifest = json.loads(Path(args.manifest).read_text())
    except (OSError, ValueError) as error:
        print(f'BLOCKED: cannot read manifest: {error}')
        return 2
    manifest = dict(manifest)
    primary = rpc if rpc is not None else ReadOnlyRPC(os.environ.get('W3_SEPOLIA_RPC_PRIMARY', ''), 0)
    try:
        if manifest.get('chainId') != CHAIN_ID or manifest.get('genesis') != GENESIS:
            raise ValueError('manifest was recorded for a different network identity')
        # The live network must still be the pinned Sepolia identity.
        _preflight.network_head(primary)
        for entry in manifest.get('contracts', []):
            tx = entry.get('creationTxHash')
            receipt = _receipt(primary, tx)
            if receipt['contractAddress'].lower() != entry['address'].lower():
                raise ValueError('receipt contractAddress no longer matches the manifest')
            digest, _ = _code_digest(_rpc_call(primary, 'eth_getCode', [entry['address'], 'safe']))
            if digest != entry.get('runtimeCodeSHA256OnChain'):
                raise ValueError('on-chain runtime bytecode no longer matches the manifest')
            artifact = entry.get('artifact') or {}
            if artifact.get('runtimeCodeSHA256') != digest:
                raise ValueError('artifact binding no longer matches on-chain code')
            if _on_chain_owner(primary, entry['address'], 'safe').lower() != entry['owner'].lower():
                raise ValueError('on-chain owner() no longer matches the manifest')
        manifest['deploymentVerified'] = True
        status = 'MANIFEST_VERIFIED'
        code = 0
    except (ValueError, OSError, KeyError, TypeError) as error:
        manifest['deploymentVerified'] = False
        manifest['reason'] = str(error)
        status = 'FAIL'
        code = 1
    finally:
        manifest['providers'] = manifest.get('providers', []) + [{'slot': primary.slot, 'endpointSHA256': hashlib.sha256(primary.endpoint.encode()).hexdigest(), 'events': primary.events}]
        if args.output:
            args.output.parent.mkdir(parents=True, exist_ok=True)
            args.output.write_text(json.dumps(manifest, indent=2) + '\n')
    print(f'{status}: read-only manifest verification; nothing was broadcast')
    return code


def _contract(value):
    parts = value.split(',')
    if len(parts) != 5:
        raise argparse.ArgumentTypeError('expected role,address,owner,txhash,artifact')
    for index in (1, 2, 3):
        if not parts[index].startswith('0x'):
            raise argparse.ArgumentTypeError(f'field {index} must be hex with 0x prefix')
    return (parts[0], parts[1], parts[2], parts[3], parts[4])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)

    build_parser = sub.add_parser('build', help='record a fresh deployment into a manifest')
    build_parser.add_argument('--contract', action='append', type=_contract, required=True, metavar='role,address,owner,txhash,artifact')
    build_parser.add_argument('--output', type=Path, required=True)
    build_parser.set_defaults(func=build)

    verify_parser = sub.add_parser('verify', help='re-verify a recorded manifest against the chain')
    verify_parser.add_argument('--manifest', type=Path, required=True)
    verify_parser.add_argument('--output', type=Path, default=None)
    verify_parser.set_defaults(func=verify)

    args = parser.parse_args()
    return args.func(args)


if __name__ == '__main__':
    raise SystemExit(main())
