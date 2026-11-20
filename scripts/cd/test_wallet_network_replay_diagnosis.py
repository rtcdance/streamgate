import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts/cd'))


def load(name, relative):
    spec = importlib.util.spec_from_file_location(name, ROOT / relative)
    module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
    return module


preflight = load('diagnosis_network_preflight', 'scripts/wallet-testnet/network_preflight.py')
diagnosis = load('diagnosis_tool', 'scripts/wallet-testnet/diagnose_network_replay.py')
CONTROLLER = '0x' + '11' * 20
SECRET = 'k3y-should-never-print'
PRIMARY = f'https://primary.example/rpc?key={SECRET}'
FALLBACK = 'https://fallback.example/rpc'
SAFE, FINALIZED = 7_000_000, 6_999_968


def block(number):
    return {'number': hex(number), 'hash': preflight.GENESIS if number == 0 else '0x' + hashlib.sha256(str(number).encode()).hexdigest()}


def answer(method, params):
    if method == 'eth_chainId':
        return hex(preflight.CHAIN_ID)
    if method == 'eth_getBlockByNumber':
        tag = params[0]
        return block({'safe': SAFE, 'finalized': FINALIZED}[tag] if tag in ('safe', 'finalized') else int(tag, 16))
    if method == 'eth_getCode':
        return '0x'
    if method == 'eth_getBalance':
        return hex(130_000_000_000_000_000)
    if method == 'eth_call':
        return '0x' + hashlib.sha256(bytes.fromhex('000102')).hexdigest()
    raise AssertionError(method)


class Response(io.BytesIO):
    def __enter__(self):
        return self

    def __exit__(self, *_):
        return False


def fake_urlopen(request, timeout):
    body = json.loads(request.data)
    return Response(json.dumps({'jsonrpc': '2.0', 'id': body['id'], 'result': answer(body['method'], body['params'])}).encode())


class NetworkReplayDiagnosisTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.env = patch.dict(os.environ, {'W3_SEPOLIA_RPC_PRIMARY': PRIMARY, 'W3_SEPOLIA_RPC_FALLBACK': FALLBACK})
        cls.env.start()
        output = Path(tempfile.mkdtemp()) / 'launch-report.network.json'
        with patch.object(preflight.urllib.request, 'urlopen', fake_urlopen), patch.object(preflight.time, 'sleep', lambda _: None), \
                patch.object(sys, 'argv', ['preflight', '--address', CONTROLLER, '--output', str(output)]):
            assert preflight.main() == 0
        cls.report = json.loads(output.read_text())
        import verify_web3_extension
        cls.verifier = verify_web3_extension

    @classmethod
    def tearDownClass(cls):
        cls.env.stop()

    def run_diagnosis(self, report, original_exit=0):
        lines = []
        code = diagnosis.diagnose(report, original_exit, CONTROLLER, self.verifier, out=lines.append)
        return code, '\n'.join(lines)

    def test_diagnose_OriginalPreflightReplaysCleanly(self):
        code, text = self.run_diagnosis(copy.deepcopy(self.report))
        self.assertEqual(code, 0, text)
        self.assertIn('PASS complete-1', text)

    def test_diagnose_TruncatedStreamFailsAtQualify(self):
        report = copy.deepcopy(self.report)
        report['providers'][1]['events'].pop()
        code, text = self.run_diagnosis(report)
        self.assertEqual(code, 1)
        self.assertIn('FAIL qualify', text)
        self.assertIn('phase_evidence.py', text)

    def test_diagnose_AlteredFieldNamesTheKey(self):
        report = copy.deepcopy(self.report)
        report['pinnedState']['balanceWei'] = '1'
        code, text = self.run_diagnosis(report)
        self.assertEqual(code, 1)
        self.assertIn('FAIL fields', text)
        self.assertIn('differing keys: pinnedState', text)

    def test_diagnose_ExtraResponseFailsAtComplete(self):
        report = copy.deepcopy(self.report)
        events = report['providers'][0]['events']
        last = events[-1]
        extra = dict(last, id=len(events) + 1, at=last['completedAt'], completedAt=last['completedAt'])
        events.append(extra)
        code, text = self.run_diagnosis(report)
        self.assertEqual(code, 1)
        self.assertIn('FAIL complete-0', text)

    def test_diagnose_ForeignEndpointNeverPrintsTheUrl(self):
        with patch.dict(os.environ, {'W3_SEPOLIA_RPC_PRIMARY': f'https://other.example/rpc?key={SECRET}'}):
            code, text = self.run_diagnosis(copy.deepcopy(self.report))
        self.assertEqual(code, 1)
        self.assertIn('FAIL stream-0', text)
        self.assertNotIn(SECRET, text)
        self.assertNotIn('example', text)


if __name__ == '__main__':
    unittest.main()
