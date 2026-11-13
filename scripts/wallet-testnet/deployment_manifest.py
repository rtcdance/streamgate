#!/usr/bin/env python3
"""Bind both Sepolia fixtures to a frozen candidate and canonical deployment transactions."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
from datetime import datetime, timezone

sys.dont_write_bytecode = True
SCRIPT_DIR = Path(__file__).resolve().parent
REPO_ROOT = SCRIPT_DIR.parents[1]
_spec = importlib.util.spec_from_file_location('wallet_network_preflight', SCRIPT_DIR / 'network_preflight.py')
network = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(network)
CHAIN_ID, GENESIS = network.CHAIN_ID, network.GENESIS
ROLES = {'wallet': 'Study1271Wallet', 'nft': 'DemoNFT'}
OWNER_SELECTOR = '0x8da5cb5b'
ERC721_INTERFACE_CALL = '0x01ffc9a780ac58cd' + '00' * 28
MANIFEST_READ_METHODS = frozenset(network.READ_METHODS | {'eth_getTransactionReceipt', 'eth_getTransactionByHash'})


class EvidenceError(ValueError):
    pass


class BlockedError(ValueError):
    pass


class DeploymentRPC(network.ReadOnlyRPC):
    # Extend the actual transport allow-list, not a disconnected wrapper list.
    allowed_methods = MANIFEST_READ_METHODS


def address(value):
    if not isinstance(value, str) or not re.fullmatch(r'0x[0-9a-fA-F]{40}', value) or int(value, 16) == 0:
        raise EvidenceError('a nonzero 20-byte address is required')
    return value.lower()


def hash32(value):
    if not isinstance(value, str) or not re.fullmatch(r'0x[0-9a-fA-F]{64}', value):
        raise EvidenceError('a 32-byte transaction or block hash is required')
    return value.lower()


def hex_bytes(value):
    if not isinstance(value, str) or not re.fullmatch(r'0x(?:[0-9a-fA-F]{2})+', value):
        raise EvidenceError('nonempty, linked bytecode is required')
    return bytes.fromhex(value[2:])


def keccak(value):
    result = subprocess.run(['cast', 'keccak', '--threads', '1'], input='0x' + value.hex(), text=True,
                            capture_output=True, timeout=10)
    if result.returncode:
        raise BlockedError('the pinned Foundry Keccak tool failed')
    return hash32(result.stdout.strip())


def candidate_id(value):
    if not isinstance(value, str) or not re.fullmatch(r'[0-9a-f]{40}', value):
        raise EvidenceError('the exact frozen candidate SHA is required')
    return value


def source_binding(metadata, role, candidate, root):
    root = Path(root).resolve()
    expected_target = {'src/' + ROLES[role] + '.sol': ROLES[role]}
    if metadata.get('settings', {}).get('compilationTarget') != expected_target:
        raise EvidenceError('artifact compilation target does not match its role')
    # A document-only evidence commit may follow C, but its contract tree and
    # submodules must still be byte-identical to the frozen candidate.
    commands = [
        ['git', '-C', str(root), 'cat-file', '-e', candidate + ':contracts/' + next(iter(expected_target))],
        ['git', '-C', str(root), 'diff', '--quiet', '--ignore-submodules=none', candidate, '--', 'contracts'],
    ]
    for command in commands:
        if subprocess.run(command, capture_output=True).returncode:
            raise EvidenceError('contract source tree differs from the frozen candidate')
    sources = metadata.get('sources')
    if not isinstance(sources, dict) or not sources:
        raise EvidenceError('compiler source identities are missing')
    identities = {}
    for name, identity in sources.items():
        path = (root / 'contracts' / name).resolve()
        if not path.is_relative_to(root / 'contracts') or not path.is_file():
            raise EvidenceError('compiler source is outside the candidate contracts')
        digest = keccak(path.read_bytes())
        if not isinstance(identity, dict) or digest != identity.get('keccak256'):
            raise EvidenceError('compiler source digest does not match the candidate')
        identities[name] = digest
    return identities


def artifact_identity(path, role, candidate, source_root):
    raw = Path(path).read_bytes()
    artifact = json.loads(raw)
    metadata = artifact.get('metadata')
    if isinstance(metadata, str):
        metadata = json.loads(metadata)
    if not isinstance(metadata, dict) or metadata.get('compiler', {}).get('version') != '0.8.24+commit.e11b9ed9':
        raise EvidenceError('artifact compiler is not the pinned Solidity 0.8.24')
    abi = artifact.get('abi')
    if not isinstance(abi, list) or not abi:
        raise EvidenceError('artifact ABI is missing')
    functions = {item.get('name') for item in abi if isinstance(item, dict) and item.get('type') == 'function'}
    required = {'isValidSignature', 'owner', 'execute', 'setOwner'} if role == 'wallet' else {'ownerOf', 'balanceOf', 'mint', 'transferFrom', 'supportsInterface'}
    if not required <= functions:
        raise EvidenceError('artifact ABI does not implement the expected role')
    creation = hex_bytes(artifact.get('bytecode', {}).get('object'))
    runtime = hex_bytes(artifact.get('deployedBytecode', {}).get('object'))
    identity = {'artifactSHA256': hashlib.sha256(raw).hexdigest(),
                'abiSHA256': hashlib.sha256(json.dumps(abi, sort_keys=True, separators=(',', ':')).encode()).hexdigest(),
                'creationCodeSHA256': hashlib.sha256(creation).hexdigest(),
                'runtimeCodeSHA256': hashlib.sha256(runtime).hexdigest(),
                'runtimeCodeKeccak256': keccak(runtime),
                'compilerVersion': metadata['compiler']['version'],
                'sources': source_binding(metadata, role, candidate, source_root)}
    return identity, creation, runtime


def compiled_identity(path, candidate, entries):
    # This index must be supplied from the selected candidate's original CI
    # capsule, independently of the deployment manifest. Final acceptance also
    # binds its digest to that CI archive; this deployment-only tool cannot.
    raw = Path(path).read_bytes()
    index = json.loads(raw)
    if not isinstance(index, dict) or index.get('schemaVersion') != 1 or index.get('candidateSHA') != candidate:
        raise EvidenceError('compiled build index belongs to a different candidate')
    contracts = index.get('contracts')
    if not isinstance(contracts, dict) or set(contracts) != set(ROLES):
        raise EvidenceError('compiled build index must contain both exact contract roles')
    for entry in entries:
        expected = contracts[entry['role']]
        if not isinstance(expected, dict):
            raise EvidenceError('compiled build identity is malformed')
        expected = {key: value for key, value in expected.items() if key != 'artifactPath'}
        if expected != entry['artifact']:
            raise EvidenceError('deployment artifact differs from the selected compiled build')
    return {'candidateSHA': candidate, 'sha256': hashlib.sha256(raw).hexdigest()}


def export_build(args):
    candidate = candidate_id(args.candidate)
    result = {'schemaVersion': 1, 'candidateSHA': candidate, 'scope': 'CANDIDATE_COMPILATION_ONLY', 'contracts': {}}
    for role, name in ROLES.items():
        source = args.artifact_root / (name + '.sol') / (name + '.json')
        identity, _, _ = artifact_identity(source, role, candidate, args.source_root)
        relative = Path('artifacts') / candidate / (name + '.json')
        destination = args.output.parent / relative
        if not destination.resolve().is_relative_to(args.output.parent.resolve()):
            raise EvidenceError('compiled artifact output escapes its bundle')
        destination.parent.mkdir(parents=True, exist_ok=True)
        if destination.exists() and destination.read_bytes() != source.read_bytes():
            raise EvidenceError('compiled bundle already contains different artifact bytes')
        if source.resolve() != destination.resolve():
            shutil.copyfile(source, destination)
        result['contracts'][role] = dict(identity, artifactPath=relative.as_posix())
    args.output.parent.mkdir(parents=True, exist_ok=True)
    if args.output.exists() and json.loads(args.output.read_text()) != result:
        raise EvidenceError('compiled build index is immutable')
    args.output.write_text(json.dumps(result, indent=2) + '\n')
    print('COMPILED_BUILD_EXPORTED: candidate compilation only, no deployment or acceptance')
    return 0


def providers(injected=None):
    result = injected if injected is not None else [DeploymentRPC(os.environ.get(key, ''), index) for index, key in enumerate(('W3_SEPOLIA_RPC_PRIMARY', 'W3_SEPOLIA_RPC_FALLBACK'))]
    if len(result) != 2 or len({rpc.host for rpc in result}) != 2:
        raise BlockedError('two distinct qualified Sepolia HTTPS providers are required')
    return result


def receipt_and_transaction(rpc, entry, controller, creation, safe_height):
    tx_hash = hash32(entry['creationTxHash'])
    receipt = rpc.call('eth_getTransactionReceipt', [tx_hash])
    if receipt is None:
        raise BlockedError('creation transaction is still pending or unavailable')
    if not isinstance(receipt, dict) or network.quantity(receipt.get('status')) != 1:
        raise EvidenceError('creation transaction did not succeed')
    if hash32(receipt.get('transactionHash')) != tx_hash or receipt.get('to') is not None or address(receipt.get('contractAddress')) != entry['address']:
        raise EvidenceError('creation receipt identity mismatch')
    height, block_hash = network.quantity(receipt.get('blockNumber')), hash32(receipt.get('blockHash'))
    if height > safe_height:
        raise BlockedError('creation receipt has not reached safe')
    anchor = network.header(rpc.call('eth_getBlockByNumber', [hex(height), False]), height)
    if anchor['hash'] != block_hash:
        raise EvidenceError('creation receipt is not canonical')
    tx = rpc.call('eth_getTransactionByHash', [tx_hash])
    if not isinstance(tx, dict) or hash32(tx.get('hash')) != tx_hash or tx.get('to') is not None or address(tx.get('from')) != controller:
        raise EvidenceError('creation transaction sender or identity mismatch')
    if network.quantity(tx.get('chainId')) != CHAIN_ID or hash32(tx.get('blockHash')) != block_hash or network.quantity(tx.get('blockNumber')) != height or network.quantity(tx.get('value')) != 0:
        raise EvidenceError('creation transaction chain, block or value mismatch')
    expected_input = creation + (bytes.fromhex('00' * 12 + controller[2:]) if entry['role'] == 'wallet' else b'')
    if hex_bytes(tx.get('input')) != expected_input:
        raise EvidenceError('creation transaction is not the candidate bytecode and constructor')
    return {'transactionHash': tx_hash, 'blockNumber': height, 'blockHash': block_hash,
            'deployer': controller, 'nonce': network.quantity(tx.get('nonce')),
            'inputSHA256': hashlib.sha256(expected_input).hexdigest(),
            'gasUsed': str(network.quantity(receipt.get('gasUsed'))),
            'effectiveGasPrice': str(network.quantity(receipt.get('effectiveGasPrice')))}


def contract_observation(rpc, entry, controller, identity, creation, runtime, safe):
    deployment = receipt_and_transaction(rpc, entry, controller, creation, safe['number'])
    for tag in (hex(deployment['blockNumber']), hex(safe['number'])):
        code = hex_bytes(rpc.call('eth_getCode', [entry['address'], tag]))
        if code != runtime:
            raise EvidenceError('deployed runtime differs from the candidate artifact')
    call = {'to': entry['address'], 'gas': '0x7a120'}
    if entry['role'] == 'wallet':
        word = rpc.call('eth_call', [dict(call, data=OWNER_SELECTOR), hex(safe['number'])])
        if not isinstance(word, str) or not re.fullmatch(r'0x0{24}[0-9a-fA-F]{40}', word) or address('0x' + word[-40:]) != controller:
            raise EvidenceError('contract wallet owner does not match controller A')
    else:
        word = rpc.call('eth_call', [dict(call, data=ERC721_INTERFACE_CALL), hex(safe['number'])])
        if word != '0x' + '00' * 31 + '01':
            raise EvidenceError('the NFT does not implement ERC-721')
    receipt_again = rpc.call('eth_getTransactionReceipt', [deployment['transactionHash']])
    if not isinstance(receipt_again, dict) or hash32(receipt_again.get('blockHash')) != deployment['blockHash'] or network.quantity(receipt_again.get('blockNumber')) != deployment['blockNumber'] or network.quantity(receipt_again.get('status')) != 1:
        raise EvidenceError('creation receipt changed during verification')
    return {'role': entry['role'], 'address': entry['address'], 'artifact': identity,
            'deployment': deployment, 'runtimeCodeSHA256': identity['runtimeCodeSHA256'],
            'runtimeCodeKeccak256': identity['runtimeCodeKeccak256']}


def verify_contracts(entries, controller, candidate, source_root, rpcs, artifact_paths):
    heads = [network.network_head(rpc) for rpc in rpcs]
    height = min(head['safe']['number'] for head in heads)
    anchors = [network.header(rpc.call('eth_getBlockByNumber', [hex(height), False]), height) for rpc in rpcs]
    if anchors[0] != anchors[1]:
        raise EvidenceError('providers disagree on the safe anchor')
    observations = []
    for entry, artifact_path in zip(entries, artifact_paths):
        identity, creation, runtime = artifact_identity(artifact_path, entry['role'], candidate, source_root)
        pair = [contract_observation(rpc, entry, controller, identity, creation, runtime, anchors[0]) for rpc in rpcs]
        if pair[0] != pair[1]:
            raise EvidenceError('providers disagree on deployment evidence')
        observations.append(pair[0])
    for rpc in rpcs:
        if network.header(rpc.call('eth_getBlockByNumber', [hex(height), False]), height) != anchors[0]:
            raise EvidenceError('safe anchor changed during verification')
    total_fee = sum(int(item['deployment']['gasUsed']) * int(item['deployment']['effectiveGasPrice']) for item in observations)
    if total_fee > network.MAX_FEE_WEI:
        raise EvidenceError('deployment fees alone exceed the testnet budget')
    return observations, anchors[0], total_fee


def validate_roles(entries, controller):
    if not isinstance(entries, list) or len(entries) != 2 or {item.get('role') for item in entries if isinstance(item, dict)} != set(ROLES):
        raise EvidenceError('exactly one wallet and one NFT role are required')
    identities = [address(item.get('address')) for item in entries]
    if len(set(identities + [controller])) != 3:
        raise EvidenceError('controller and both contract addresses must be distinct')
    for item in entries:
        hash32(item.get('creationTxHash'))
        if item['address'] != address(item['address']):
            raise EvidenceError('manifest addresses must use normalized lowercase hex')


def record_provider_evidence(rpcs):
    return [{'slot': rpc.slot, 'endpointSHA256': hashlib.sha256(rpc.endpoint.encode()).hexdigest(), 'events': rpc.events} for rpc in rpcs]


def write_report(output, report, status, reason=None):
    report.update(status=status, deploymentVerified=status in ('MANIFEST_RECORDED', 'MANIFEST_VERIFIED'), completeAcceptance=False, walletControlVerified=False)
    if reason:
        report['reason'] = reason
    if output is not None:
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(report, indent=2) + '\n')
    print(status + ': read-only canonical deployment verification; no signatures or broadcasts')
    return 0 if report['deploymentVerified'] else 2 if status == 'BLOCKED' else 1


def build(args, rpcs=None):
    report = {'schemaVersion': 2, 'scope': 'ENVIRONMENT_READ_ONLY', 'at': datetime.now(timezone.utc).isoformat(), 'chainId': CHAIN_ID, 'genesis': GENESIS, 'contracts': []}
    active = []
    try:
        candidate, controller = candidate_id(args.candidate), address(args.controller)
        entries = [{'role': role, 'address': address(contract), 'deployer': address(deployer), 'creationTxHash': hash32(tx), 'artifactInput': artifact} for role, contract, deployer, tx, artifact in args.contract]
        validate_roles(entries, controller)
        if any(item['deployer'] != controller for item in entries):
            raise EvidenceError('both deployments must be sent by controller A')
        active = providers(rpcs)
        observations, anchor, fee = verify_contracts(entries, controller, candidate, args.source_root, active, [item['artifactInput'] for item in entries])
        compilation = compiled_identity(args.build_identity, candidate, observations)
        for entry, observation in zip(entries, observations):
            relative = Path('artifacts') / candidate / (ROLES[entry['role']] + '.json')
            destination = args.output.parent / relative
            if not destination.resolve().is_relative_to(args.output.parent.resolve()):
                raise EvidenceError('artifact output escapes its evidence bundle')
            destination.parent.mkdir(parents=True, exist_ok=True)
            if destination.exists() and destination.read_bytes() != Path(entry['artifactInput']).read_bytes():
                raise EvidenceError('existing bundle contains different artifact bytes')
            if Path(entry['artifactInput']).resolve() != destination.resolve():
                shutil.copyfile(entry['artifactInput'], destination)
            observation['creationTxHash'] = entry['creationTxHash']
            observation['artifact']['artifactPath'] = relative.as_posix()
        report.update(candidateSHA=candidate, controller=controller, compiledBuild=compilation, contracts=observations, safeAnchor=anchor, deploymentFeesWei=str(fee))
        status, reason = 'MANIFEST_RECORDED', None
    except (EvidenceError, TypeError, KeyError) as error:
        status, reason = 'FAIL', str(error)
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        status, reason = 'BLOCKED', str(error)
    report['providers'] = record_provider_evidence(active)
    return write_report(args.output, report, status, reason)


def verify(args, rpcs=None):
    report, active = {'schemaVersion': 2, 'scope': 'ENVIRONMENT_READ_ONLY'}, []
    try:
        if args.output is not None and args.output.resolve() == args.manifest.resolve():
            raise EvidenceError('verification output must not overwrite the source manifest')
        manifest = json.loads(args.manifest.read_text())
        if not isinstance(manifest, dict) or manifest.get('schemaVersion') != 2 or manifest.get('deploymentVerified') is not True:
            raise EvidenceError('a successfully recorded schema-v2 manifest is required')
        candidate, controller = candidate_id(args.candidate), address(args.controller)
        if manifest.get('candidateSHA') != candidate or manifest.get('controller') != controller or manifest.get('chainId') != CHAIN_ID or manifest.get('genesis') != GENESIS:
            raise EvidenceError('candidate, controller or network identity mismatch')
        entries = manifest.get('contracts')
        validate_roles(entries, controller)
        artifact_paths = []
        for entry in entries:
            relative = Path(entry.get('artifact', {}).get('artifactPath', ''))
            path = (args.manifest.parent / relative).resolve()
            if relative.is_absolute() or not path.is_relative_to(args.manifest.parent.resolve()) or not path.is_file():
                raise EvidenceError('a bundled candidate artifact is missing or outside the evidence bundle')
            artifact_paths.append(path)
        active = providers(rpcs)
        observations, anchor, fee = verify_contracts(entries, controller, candidate, args.source_root, active, artifact_paths)
        compilation = compiled_identity(args.build_identity, candidate, observations)
        if compilation != manifest.get('compiledBuild'):
            raise EvidenceError('compiled build index differs from the recorded deployment')
        for old, current in zip(entries, observations):
            expected_artifact = dict(old['artifact'])
            expected_artifact.pop('artifactPath', None)
            if expected_artifact != current['artifact'] or old.get('deployment') != current['deployment']:
                raise EvidenceError('recorded artifact or deployment identity differs from current evidence')
        report.update(candidateSHA=candidate, controller=controller, compiledBuild=compilation, chainId=CHAIN_ID, genesis=GENESIS, contracts=observations, safeAnchor=anchor, deploymentFeesWei=str(fee))
        status, reason = 'MANIFEST_VERIFIED', None
    except (EvidenceError, TypeError, KeyError) as error:
        status, reason = 'FAIL', str(error)
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        status, reason = 'BLOCKED', str(error)
    report['providers'] = record_provider_evidence(active)
    output = args.output if args.output is None or args.output.resolve() != args.manifest.resolve() else None
    return write_report(output, report, status, reason)


def _contract(value):
    parts = value.split(',')
    if len(parts) != 5 or parts[0] not in ROLES:
        raise argparse.ArgumentTypeError('expected wallet|nft,address,deployer,creation-tx,artifact')
    try:
        address(parts[1]); address(parts[2]); hash32(parts[3])
    except EvidenceError as error:
        raise argparse.ArgumentTypeError(str(error)) from None
    return tuple(parts)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    for command in ('build', 'verify'):
        target = sub.add_parser(command)
        target.add_argument('--candidate', required=True)
        target.add_argument('--controller', required=True)
        target.add_argument('--source-root', type=Path, default=REPO_ROOT)
        target.add_argument('--build-identity', type=Path, required=True, help='original candidate CI compilation index, independent of the deployment manifest')
        target.add_argument('--output', type=Path, required=command == 'build')
        if command == 'build':
            target.add_argument('--contract', action='append', type=_contract, required=True)
        else:
            target.add_argument('--manifest', type=Path, required=True)
        target.set_defaults(func=build if command == 'build' else verify)
    target = sub.add_parser('export-build')
    target.add_argument('--candidate', required=True)
    target.add_argument('--source-root', type=Path, default=REPO_ROOT)
    target.add_argument('--artifact-root', type=Path, default=REPO_ROOT / 'contracts/out')
    target.add_argument('--output', type=Path, required=True)
    target.set_defaults(func=export_build)
    args = parser.parse_args()
    return args.func(args)


if __name__ == '__main__':
    raise SystemExit(main())
