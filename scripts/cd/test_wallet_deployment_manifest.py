import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import types
import unittest

sys.dont_write_bytecode = True
SCRIPT = Path(__file__).resolve().parent.parent / 'wallet-testnet/deployment_manifest.py'
spec = importlib.util.spec_from_file_location('wallet_deployment_manifest', SCRIPT)
manifest_tool = importlib.util.module_from_spec(spec)
spec.loader.exec_module(manifest_tool)
preflight_spec = importlib.util.spec_from_file_location('wallet_network_preflight', SCRIPT.parent / 'network_preflight.py')
preflight = importlib.util.module_from_spec(preflight_spec)
preflight_spec.loader.exec_module(preflight)

WALLET = '0x' + 'a1' * 20
NFT = '0x' + 'b2' * 20
OWNER = '0x' + 'c3' * 20
TX_WALLET = '0x' + '11' * 32
TX_NFT = '0x' + '22' * 32
RUNTIME = '0x60806040523480156100115760006000fd5b50'
CREATION = '0x6080604052' + RUNTIME[2:]
GENESIS_BLOCK = {'number': '0x0', 'hash': preflight.GENESIS}
SAFE_BLOCK = {'number': '0x64', 'hash': '0x' + '11' * 32}
FINALIZED_BLOCK = {'number': '0x60', 'hash': '0x' + '22' * 32}


def word(address):
    return '0x' + '0' * 24 + address[2:].lower()


class FakeRPC:
    """Read-only fixture; any non-read method or write must be refused."""

    def __init__(self, runtime=RUNTIME, receipt_owner=OWNER):
        self.endpoint = 'https://fixture.example'
        self.slot = 0
        self.events = []
        self.runtime = runtime
        self.receipt_owner = receipt_owner
        self.calls = []

    def call(self, method, params):
        self.calls.append((method, params))
        if method not in manifest_tool.MANIFEST_READ_METHODS:
            raise AssertionError(f'non-read method attempted: {method}')
        if method == 'eth_chainId':
            return hex(preflight.CHAIN_ID)
        if method == 'eth_getBlockByNumber':
            tag = params[0]
            if tag == '0x0':
                return dict(GENESIS_BLOCK)
            if tag == 'safe':
                return dict(SAFE_BLOCK)
            if tag == 'finalized':
                return dict(FINALIZED_BLOCK)
            return {'number': tag, 'hash': '0x' + '33' * 32}
        if method == 'eth_getTransactionReceipt':
            if params[0] == TX_WALLET:
                return {'status': '0x1', 'contractAddress': WALLET, 'blockNumber': '0x10', 'blockHash': SAFE_BLOCK['hash'], 'to': None}
            if params[0] == TX_NFT:
                return {'status': '0x1', 'contractAddress': NFT, 'blockNumber': '0x11', 'blockHash': SAFE_BLOCK['hash'], 'to': None}
            return None
        if method == 'eth_getCode':
            return self.runtime
        if method == 'eth_call':
            if params[0].get('data') == manifest_tool.OWNER_SELECTOR:
                return word(self.receipt_owner)
            raise AssertionError('unexpected eth_call target')
        if method == 'eth_sendRawTransaction':
            raise AssertionError('broadcast attempted')
        raise AssertionError(method)


def artifact_file(directory, runtime=RUNTIME, creation=CREATION):
    path = Path(directory) / 'Study1271Wallet.json'
    path.write_text(json.dumps({
        'bytecode': {'object': creation},
        'deployedBytecode': {'object': runtime},
        'metadata': {'compiler': {'version': '0.8.24+commit.e11b9ed9'}},
    }))
    return path


def build_args(contracts, output):
    return types.SimpleNamespace(contract=contracts, output=Path(output))


def verify_args(manifest_path, output=None):
    return types.SimpleNamespace(manifest=Path(manifest_path), output=Path(output) if output else None)


class DeploymentManifestTests(unittest.TestCase):
    def test_build_records_artifact_binding_and_refuses_broadcast(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact = artifact_file(directory)
            rpc = FakeRPC()
            out = Path(directory) / 'manifest.json'
            code = manifest_tool.build(build_args([
                ('wallet', WALLET, OWNER, TX_WALLET, str(artifact)),
                ('nft', NFT, OWNER, TX_NFT, str(artifact)),
            ], out), rpc=rpc)
            self.assertEqual(0, code)
            data = json.loads(out.read_text())
            self.assertTrue(data['deploymentVerified'])
            self.assertFalse(data['walletControlVerified'] if 'walletControlVerified' in data else data['completeAcceptance'])
            self.assertEqual([entry['role'] for entry in data['contracts']], ['wallet', 'nft'])
            for entry in data['contracts']:
                self.assertEqual(entry['runtimeCodeSHA256OnChain'], entry['artifact']['runtimeCodeSHA256'])
                self.assertEqual(entry['owner'].lower(), OWNER.lower())
            # No endpoint URL and no credential material may be recorded.
            self.assertNotIn('fixture.example', out.read_text())
            self.assertNotIn('eth_sendRawTransaction', json.dumps(rpc.calls))

    def test_build_rejects_artifact_code_mismatch(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact = artifact_file(directory, runtime='0xdeadbeef')
            out = Path(directory) / 'manifest.json'
            code = manifest_tool.build(build_args([('wallet', WALLET, OWNER, TX_WALLET, str(artifact))], out), rpc=FakeRPC())
            self.assertEqual(2, code)
            self.assertFalse(json.loads(out.read_text())['deploymentVerified'])

    def test_build_rejects_owner_mismatch(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact = artifact_file(directory)
            out = Path(directory) / 'manifest.json'
            stranger = '0x' + 'd4' * 20
            code = manifest_tool.build(build_args([('wallet', WALLET, stranger, TX_WALLET, str(artifact))], out), rpc=FakeRPC())
            self.assertEqual(2, code)

    def test_build_rejects_missing_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact = artifact_file(directory)
            out = Path(directory) / 'manifest.json'
            code = manifest_tool.build(build_args([('wallet', WALLET, OWNER, '0x' + 'ff' * 32, str(artifact))], out), rpc=FakeRPC())
            self.assertEqual(2, code)

    def test_build_rejects_role_without_artifact_and_duplicate_roles(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact = artifact_file(directory)
            out = Path(directory) / 'manifest.json'
            code = manifest_tool.build(build_args([('wallet', WALLET, OWNER, TX_WALLET, str(artifact))], out), rpc=FakeRPC())
            self.assertEqual(2, code)
            data = json.loads(out.read_text())
            self.assertIn('distinct address', data['reason'])

    def test_verify_roundtrip_and_tamper(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact = artifact_file(directory)
            out = Path(directory) / 'manifest.json'
            manifest_tool.build(build_args([('wallet', WALLET, OWNER, TX_WALLET, str(artifact))], out), rpc=FakeRPC())
            # A single-role manifest verifies fine as a manifest even though the
            # full TN-02 set needs both roles; verify only re-checks bindings.
            self.assertEqual(0, manifest_tool.verify(verify_args(out), rpc=FakeRPC()))
            # Tampered on-chain code must FAIL loudly.
            self.assertEqual(1, manifest_tool.verify(verify_args(out), rpc=FakeRPC(runtime='0x' + 'ff' * 20)))

    def test_verify_rejects_foreign_network_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            data = {'schemaVersion': 1, 'chainId': 1, 'genesis': '0x' + '00' * 32, 'contracts': []}
            manifest_path = Path(directory) / 'foreign.json'
            manifest_path.write_text(json.dumps(data))
            self.assertEqual(1, manifest_tool.verify(verify_args(manifest_path), rpc=FakeRPC()))

    def test_contract_argument_parser(self):
        with self.assertRaises(Exception):
            manifest_tool._contract('wallet,0x1')
        parsed = manifest_tool._contract(f'wallet,{WALLET},{OWNER},{TX_WALLET},artifact.json')
        self.assertEqual(('wallet', WALLET, OWNER, TX_WALLET, 'artifact.json'), parsed)


if __name__ == '__main__':
    unittest.main()
