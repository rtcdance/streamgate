import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import types
import unittest
from unittest import mock

sys.dont_write_bytecode = True
SCRIPT = Path(__file__).resolve().parent.parent / 'wallet-testnet/deployment_manifest.py'
spec = importlib.util.spec_from_file_location('wallet_deployment_manifest', SCRIPT)
tool = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tool)
CANDIDATE = 'a' * 40
WALLET, NFT, OWNER = ('0x' + value * 20 for value in ('a1', 'b2', 'c3'))
TX_WALLET, TX_NFT = ('0x' + value * 32 for value in ('11', '22'))
RUNTIME = {'wallet': '0x60006000', 'nft': '0x60016001'}
CREATION = {role: '0x6080' + code[2:] for role, code in RUNTIME.items()}
RECEIPT_HASH = '0x' + '33' * 32
SAFE_HASH = '0x' + '44' * 32


class FakeRPC:
    def __init__(self, host, slot, fault=''):
        self.endpoint, self.host, self.slot = 'https://' + host, host, slot
        self.events, self.calls = [], []
        self.fault = fault
        self.receipt_reads = 0

    def call(self, method, params):
        if method not in tool.DeploymentRPC.allowed_methods:
            raise AssertionError('write attempted')
        self.calls.append((method, params))
        if method == 'eth_chainId':
            return hex(tool.CHAIN_ID)
        if method == 'eth_getBlockByNumber':
            tag = params[0]
            if tag == '0x0':
                return {'number': tag, 'hash': tool.GENESIS}
            if tag in ('safe', 'finalized', '0x64'):
                return {'number': '0x64', 'hash': SAFE_HASH}
            return {'number': tag, 'hash': '0x' + '99' * 32 if self.fault == 'noncanonical' else RECEIPT_HASH}
        if method in ('eth_getTransactionReceipt', 'eth_getTransactionByHash'):
            role = 'wallet' if params[0] == TX_WALLET else 'nft'
            contract = WALLET if role == 'wallet' else NFT
            height = '0x10' if role == 'wallet' else '0x11'
            if self.fault == 'pending':
                return None
            if method == 'eth_getTransactionReceipt':
                self.receipt_reads += 1
                return {'status': '0x0' if self.fault == 'revert' else '0x1', 'transactionHash': params[0],
                        'contractAddress': contract, 'blockNumber': height,
                        'blockHash': '0x' + '99' * 32 if self.fault == 'receipt-reorg' and self.receipt_reads > 1 else RECEIPT_HASH,
                        'to': None, 'gasUsed': '0x10000', 'effectiveGasPrice': '0x3b9aca00'}
            suffix = ('00' * 12 + OWNER[2:]) if role == 'wallet' else ''
            return {'hash': params[0], 'to': None, 'from': NFT if self.fault == 'sender' else OWNER,
                    'chainId': '0x1' if self.fault == 'tx-chain' else hex(tool.CHAIN_ID),
                    'blockHash': RECEIPT_HASH, 'blockNumber': height, 'value': '0x0', 'nonce': '0x1',
                    'input': '0xdead' if self.fault == 'constructor' else CREATION[role] + suffix}
        if method == 'eth_getCode':
            return '0xdead' if self.fault == 'runtime' else RUNTIME['wallet' if params[0] == WALLET else 'nft']
        if method == 'eth_call':
            if params[0]['to'] == WALLET:
                if params[0]['data'] != tool.OWNER_SELECTOR:
                    raise AssertionError('wrong wallet selector')
                return '0x' + '00' * 12 + (NFT if self.fault == 'owner' else OWNER)[2:]
            # The real DemoNFT has no owner(). The fake rejects the old bug.
            if params[0]['data'] == tool.OWNER_SELECTOR:
                raise AssertionError('DemoNFT has no global owner')
            if params[0]['data'] != tool.ERC721_INTERFACE_CALL:
                raise AssertionError('wrong ERC-165 selector')
            return '0x' + '00' * 31 + ('00' if self.fault == 'interface' else '01')
        raise AssertionError(method)


def artifacts(directory):
    result = {}
    names = {'wallet': ['owner', 'execute', 'setOwner', 'isValidSignature'],
             'nft': ['ownerOf', 'balanceOf', 'mint', 'transferFrom', 'supportsInterface']}
    for role, name in tool.ROLES.items():
        path = Path(directory) / (name + '.json')
        path.write_text(json.dumps({'abi': [{'type': 'function', 'name': n} for n in names[role]],
                                    'bytecode': {'object': CREATION[role]}, 'deployedBytecode': {'object': RUNTIME[role]},
                                    'metadata': {'compiler': {'version': '0.8.24+commit.e11b9ed9'}, 'settings': {'compilationTarget': {'src/' + name + '.sol': name}}}}))
        result[role] = path
    return result


class DeploymentManifestTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.artifacts = artifacts(self.root)
        self.output = self.root / 'bundle/manifest.json'
        self.args = types.SimpleNamespace(candidate=CANDIDATE, controller=OWNER, source_root=self.root, output=self.output,
                                          contract=[('wallet', WALLET, OWNER, TX_WALLET, str(self.artifacts['wallet'])), ('nft', NFT, OWNER, TX_NFT, str(self.artifacts['nft']))])
        self.patcher = mock.patch.object(tool, 'source_binding', return_value={'candidate-solidity-source': '0x' + '55' * 32})
        self.patcher.start(); self.addCleanup(self.patcher.stop)
        self.build_identity = self.root / 'ci-build-identity.json'
        self.build_identity.write_text(json.dumps({'schemaVersion': 1, 'candidateSHA': CANDIDATE,
            'contracts': {role: tool.artifact_identity(path, role, CANDIDATE, self.root)[0] for role, path in self.artifacts.items()}}))
        self.args.build_identity = self.build_identity

    def rpcs(self, fault=''):
        return [FakeRPC('primary.example', 0), FakeRPC('fallback.example', 1, fault)]

    def record(self):
        self.assertEqual(0, tool.build(self.args, rpcs=self.rpcs()))
        return json.loads(self.output.read_text())

    def verify(self, fault=''):
        args = types.SimpleNamespace(candidate=CANDIDATE, controller=OWNER, source_root=self.root, manifest=self.output, build_identity=self.build_identity, output=self.root / 'verified.json')
        code = tool.verify(args, rpcs=self.rpcs(fault))
        return code, json.loads(args.output.read_text())

    def test_both_roles_real_transport_contract_and_roundtrip(self):
        manifest = self.record()
        self.assertEqual(CANDIDATE, manifest['candidateSHA'])
        self.assertTrue(manifest['deploymentVerified'])
        self.assertFalse(manifest['completeAcceptance'])
        self.assertFalse(manifest['walletControlVerified'])
        self.assertEqual(2, len(manifest['providers']))
        for entry in manifest['contracts']:
            artifact = entry['artifact']
            self.assertEqual(artifact['artifactSHA256'], hashlib.sha256((self.output.parent / artifact['artifactPath']).read_bytes()).hexdigest())
            self.assertRegex(entry['runtimeCodeKeccak256'], r'^0x[0-9a-f]{64}$')
        code, report = self.verify()
        self.assertEqual(0, code)
        self.assertTrue(report['deploymentVerified'])
        self.assertNotIn('primary.example', self.output.read_text())

    def test_incomplete_foreign_and_failed_manifests_never_verify(self):
        good = self.record()
        for change in ('empty', 'missing-role', 'duplicate-role', 'failed', 'foreign-sha', 'foreign-network'):
            value = copy.deepcopy(good)
            if change == 'empty': value['contracts'] = []
            elif change == 'missing-role': value['contracts'].pop()
            elif change == 'duplicate-role': value['contracts'][1]['role'] = 'wallet'
            elif change == 'failed': value['deploymentVerified'] = False
            elif change == 'foreign-sha': value['candidateSHA'] = 'b' * 40
            else: value['chainId'] = 31337
            self.output.write_text(json.dumps(value))
            with self.subTest(change=change):
                code, report = self.verify()
                self.assertEqual(1, code)
                self.assertFalse(report['deploymentVerified'])

    def test_canonical_transaction_and_current_policy_counterexamples(self):
        self.record()
        for fault in ('noncanonical', 'revert', 'receipt-reorg', 'sender', 'tx-chain', 'constructor', 'runtime', 'owner', 'interface'):
            with self.subTest(fault=fault):
                code, report = self.verify(fault)
                self.assertEqual(1, code)
                self.assertFalse(report['deploymentVerified'])
        code, report = self.verify('pending')
        self.assertEqual(2, code)
        self.assertEqual('BLOCKED', report['status'])

    def test_artifact_and_record_tampering_rejected(self):
        good = self.record()
        path = self.output.parent / good['contracts'][0]['artifact']['artifactPath']
        raw = path.read_bytes(); value = json.loads(raw)
        value['abi'].append({'type': 'function', 'name': 'unapproved'})
        path.write_text(json.dumps(value))
        code, report = self.verify()
        self.assertEqual(1, code)
        self.assertFalse(report['deploymentVerified'])
        path.write_bytes(raw)
        good['contracts'][0]['deployment']['nonce'] += 1
        self.output.write_text(json.dumps(good))
        self.assertEqual(1, self.verify()[0])

    def test_self_consistent_rewritten_manifest_cannot_replace_ci_compilation(self):
        manifest = self.record()
        path = self.output.parent / manifest['contracts'][0]['artifact']['artifactPath']
        artifact = json.loads(path.read_text())
        artifact['abi'].append({'type': 'function', 'name': 'tampered'})
        path.write_text(json.dumps(artifact))
        identity, _, _ = tool.artifact_identity(path, 'wallet', CANDIDATE, self.root)
        identity['artifactPath'] = manifest['contracts'][0]['artifact']['artifactPath']
        manifest['contracts'][0]['artifact'] = identity
        self.output.write_text(json.dumps(manifest))
        code, report = self.verify()
        self.assertEqual(1, code)
        self.assertIn('compiled build', report['reason'])

    def test_changed_ci_index_or_wrong_candidate_rejected(self):
        self.record()
        value = json.loads(self.build_identity.read_text())
        for change in ('candidate', 'missing', 'abi'):
            index = copy.deepcopy(value)
            if change == 'candidate': index['candidateSHA'] = 'b' * 40
            elif change == 'missing': index['contracts'].pop('nft')
            else: index['contracts']['wallet']['abiSHA256'] = '0' * 64
            self.build_identity.write_text(json.dumps(index))
            with self.subTest(change=change): self.assertEqual(1, self.verify()[0])

    def test_export_build_copies_original_artifacts_and_refuses_overwrite(self):
        for role, name in tool.ROLES.items():
            directory = self.root / 'out' / (name + '.sol'); directory.mkdir(parents=True)
            (directory / (name + '.json')).write_bytes(self.artifacts[role].read_bytes())
        args = types.SimpleNamespace(candidate=CANDIDATE, source_root=self.root, artifact_root=self.root / 'out', output=self.root / 'ci/build-identity.json')
        self.assertEqual(0, tool.export_build(args))
        self.assertEqual(0, tool.export_build(args))
        index = json.loads(args.output.read_text())
        for role, identity in index['contracts'].items():
            self.assertEqual(self.artifacts[role].read_bytes(), (args.output.parent / identity['artifactPath']).read_bytes())
        path = self.root / 'out/Study1271Wallet.sol/Study1271Wallet.json'
        value = json.loads(path.read_text()); value['abi'].append({'type': 'function', 'name': 'changed'})
        path.write_text(json.dumps(value))
        with self.assertRaises(tool.EvidenceError): tool.export_build(args)

    def test_build_rejects_missing_role_duplicate_sender_and_single_provider(self):
        for mutation in ('missing', 'duplicate', 'sender', 'single-provider'):
            args = copy.deepcopy(self.args)
            if mutation == 'missing': args.contract.pop()
            elif mutation == 'duplicate': args.contract[1] = args.contract[0]
            elif mutation == 'sender': args.contract[1] = ('nft', NFT, WALLET, TX_NFT, str(self.artifacts['nft']))
            with self.subTest(mutation=mutation):
                code = tool.build(args, rpcs=self.rpcs()[:1] if mutation == 'single-provider' else self.rpcs())
                self.assertNotEqual(0, code)
                self.assertFalse(json.loads(args.output.read_text())['deploymentVerified'])

    def test_manifest_verification_never_overwrites_evidence(self):
        self.record(); original = self.output.read_bytes()
        args = types.SimpleNamespace(candidate=CANDIDATE, controller=OWNER, source_root=self.root, manifest=self.output, output=self.output)
        self.assertEqual(1, tool.verify(args, rpcs=self.rpcs()))
        self.assertEqual(original, self.output.read_bytes())

    def test_actual_rpc_allowlist_accepts_receipts_but_never_broadcasts(self):
        response = io.BytesIO(json.dumps({'jsonrpc': '2.0', 'id': 1, 'result': {'status': '0x1'}}).encode())
        with mock.patch.object(tool.network.urllib.request, 'urlopen', return_value=response):
            rpc = tool.DeploymentRPC('https://primary.example', 0)
            self.assertEqual({'status': '0x1'}, rpc.call('eth_getTransactionReceipt', [TX_WALLET]))
        with self.assertRaises(ValueError):
            tool.network.ReadOnlyRPC('https://primary.example', 0).call('eth_getTransactionReceipt', [TX_WALLET])
        with self.assertRaises(ValueError):
            rpc.call('eth_sendRawTransaction', ['0x1234'])

    def test_pinned_keccak_is_ethereum_keccak_not_sha3(self):
        self.assertEqual('0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470', tool.keccak(b''))
        self.assertNotEqual(hashlib.sha3_256(b'').hexdigest(), tool.keccak(b'')[2:])


class SourceBindingTests(unittest.TestCase):
    def test_candidate_source_digest_and_dirty_tree(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); path = root / 'contracts/src/Study1271Wallet.sol'
            path.parent.mkdir(parents=True); path.write_text('pragma solidity ^0.8.24; contract Study1271Wallet {}\n')
            subprocess.run(['git', 'init', '-q', str(root)], check=True)
            subprocess.run(['git', '-C', str(root), 'add', 'contracts'], check=True)
            subprocess.run(['git', '-C', str(root), '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'fixture'], check=True)
            candidate = subprocess.check_output(['git', '-C', str(root), 'rev-parse', 'HEAD'], text=True).strip()
            metadata = {'settings': {'compilationTarget': {'src/Study1271Wallet.sol': 'Study1271Wallet'}}, 'sources': {'src/Study1271Wallet.sol': {'keccak256': tool.keccak(path.read_bytes())}}}
            self.assertTrue(tool.source_binding(metadata, 'wallet', candidate, root))
            bad = copy.deepcopy(metadata); bad['sources']['src/Study1271Wallet.sol']['keccak256'] = '0x' + 'ff' * 32
            with self.assertRaises(tool.EvidenceError): tool.source_binding(bad, 'wallet', candidate, root)
            bad = copy.deepcopy(metadata); bad['sources']['../../outside.sol'] = {'keccak256': '0x' + 'ff' * 32}
            with self.assertRaises(tool.EvidenceError): tool.source_binding(bad, 'wallet', candidate, root)
            path.write_text(path.read_text() + 'contract Tampered {}\n')
            with self.assertRaises(tool.EvidenceError): tool.source_binding(metadata, 'wallet', candidate, root)


if __name__ == '__main__':
    unittest.main()
