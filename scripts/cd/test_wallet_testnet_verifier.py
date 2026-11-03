import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

sys.dont_write_bytecode = True
SCRIPT = Path(__file__).resolve().parent.parent / 'wallet-testnet/verify_web3_testnet.py'
spec = importlib.util.spec_from_file_location('wallet_testnet_verifier', SCRIPT)
verifier = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verifier)

ADDRESS = '0x732E24a0BcAb6fA41b1C9148BCF1135587bC76c1'


class SepoliaEnvMappingTests(unittest.TestCase):
    def test_current_variables_map_to_tool_names(self):
        with mock.patch.dict(verifier.os.environ, {'SEPOLIA_RPC_URL': 'https://a.example', 'SEPOLIA_RPC_FALLBACK_URL': 'https://b.example', 'SEPOLIA_RPC': ''}, clear=False):
            env = verifier.sepolia_env()
        self.assertEqual('https://a.example', env['W3_SEPOLIA_RPC_PRIMARY'])
        self.assertEqual('https://b.example', env['W3_SEPOLIA_RPC_FALLBACK'])

    def test_legacy_maps_to_primary(self):
        with mock.patch.dict(verifier.os.environ, {'SEPOLIA_RPC': 'https://legacy.example', 'SEPOLIA_RPC_URL': '', 'SEPOLIA_RPC_FALLBACK_URL': ''}, clear=False):
            env = verifier.sepolia_env()
        self.assertEqual('https://legacy.example', env['W3_SEPOLIA_RPC_PRIMARY'])

    def test_disagreement_raises_without_echoing(self):
        with mock.patch.dict(verifier.os.environ, {'SEPOLIA_RPC': 'https://legacy.example', 'SEPOLIA_RPC_URL': 'https://current.example'}, clear=False):
            with self.assertRaises(ValueError) as caught:
                verifier.sepolia_env()
        self.assertNotIn('https://', str(caught.exception))


class VerifyWeb3TestnetTests(unittest.TestCase):
    def run_main(self, argv, env=None):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / 'report.json'
            with mock.patch.dict(verifier.os.environ, env or {}, clear=False):
                code = verifier.main(argv + ['--output', str(output)])
            report = json.loads(output.read_text())
        return code, report

    def test_missing_address_is_blocked_not_skipped(self):
        code, report = self.run_main(['--stage', 'preflight'], env={'W3_SEPOLIA_ADDRESS': ''})
        self.assertEqual(2, code)
        self.assertEqual('TESTNET_BLOCKED', report['status'])
        self.assertFalse(report['completeAcceptance'])

    def test_preflight_without_rpc_endpoints_blocks(self):
        # Empty endpoint values make the child preflight BLOCKED before any
        # network traffic, so this test is offline by construction.
        code, report = self.run_main(
            ['--stage', 'preflight', '--address', ADDRESS],
            env={'W3_SEPOLIA_RPC_PRIMARY': '', 'W3_SEPOLIA_RPC_FALLBACK': '', 'SEPOLIA_RPC_URL': '', 'SEPOLIA_RPC': ''})
        self.assertEqual(2, code)
        self.assertEqual('TESTNET_BLOCKED', report['status'])
        self.assertEqual('TESTNET_BLOCKED', report['stages']['preflight']['status'])
        self.assertNotEqual(0, report['stages']['preflight']['exit'])

    def test_full_entry_reports_user_gated_items(self):
        code, report = self.run_main(
            ['--address', ADDRESS],
            env={'W3_SEPOLIA_RPC_PRIMARY': '', 'W3_SEPOLIA_RPC_FALLBACK': '', 'SEPOLIA_RPC_URL': '', 'SEPOLIA_RPC': ''})
        self.assertEqual(2, code)
        self.assertEqual('TESTNET_BLOCKED', report['status'])
        self.assertTrue(report['userGated'])
        for item in report['userGated']:
            self.assertEqual('BLOCKED', item['status'])
            self.assertEqual('user', item['owner'])
        self.assertIn('BLOCKED', report['stages']['manifest']['status'])

    def test_h5_unreachable_origin_is_blocked(self):
        entry = verifier.stage_h5('http://127.0.0.1:1/')
        self.assertEqual('TESTNET_BLOCKED', entry['status'])


if __name__ == '__main__':
    unittest.main()
