import copy
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
            with mock.patch.dict(verifier.os.environ, dict({'W3_EVIDENCE_DIR': '', 'W3_SEPOLIA_RECIPIENT': '0x' + '2' * 40}, **(env or {})), clear=False):
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
        with mock.patch.object(verifier.urllib.request, 'urlopen', side_effect=OSError('offline fixture')):
            entry = verifier.stage_h5('http://127.0.0.1:1/')
        self.assertEqual('TESTNET_BLOCKED', entry['status'])

    def test_automatable_passes_cannot_complete_public_wallet_acceptance(self):
        with mock.patch.object(verifier, 'stage_preflight', return_value={'status': 'PASS'}), \
                mock.patch.object(verifier, 'stage_manifest', return_value={'status': 'PASS'}), \
                mock.patch.object(verifier, 'stage_h5', return_value={'status': 'PASS'}):
            code, report = self.run_main(['--address', ADDRESS, '--manifest', '/test.json', '--candidate', 'a' * 40, '--h5-origin', 'http://localhost:1234'])
        self.assertEqual(2, code)
        self.assertEqual('TESTNET_BLOCKED', report['status'])
        self.assertFalse(report['completeAcceptance'])
        self.assertEqual('NOT_REQUESTED', report['faucet']['status'])
        self.assertFalse(report['faucet']['required'])
        self.assertNotIn('sepolia_faucet_claim', {item['item'] for item in report['userGated']})

    def test_preflight_only_success_has_network_scope(self):
        with mock.patch.object(verifier, 'stage_preflight', return_value={'status': 'PASS'}):
            code, report = self.run_main(['--address', ADDRESS, '--stage', 'preflight'])
        self.assertEqual(0, code)
        self.assertEqual('NETWORK_READY', report['status'])
        self.assertFalse(report['walletControlVerified'])
        self.assertFalse(report['completeAcceptance'])

    def test_manifest_receives_external_candidate_and_controller(self):
        with mock.patch.object(verifier, 'run_tool', return_value=(0, '', '', {})) as run:
            verifier.stage_manifest(Path('/fixture/manifest.json'), 'a' * 40, ADDRESS, Path('/ci/build-identity.json'))
        command = run.call_args.args[0]
        self.assertEqual('a' * 40, command[command.index('--candidate') + 1])
        self.assertEqual(ADDRESS, command[command.index('--controller') + 1])
        self.assertEqual('/ci/build-identity.json', command[command.index('--build-identity') + 1])
        with mock.patch.object(verifier, 'run_tool') as run:
            self.assertEqual('TESTNET_BLOCKED', verifier.stage_manifest(Path('/fixture.json'), '', ADDRESS, None)['status'])
            run.assert_not_called()

    def test_conflicting_rpc_env_writes_failure_report_without_endpoint_values(self):
        code, report = self.run_main(['--address', ADDRESS, '--stage', 'preflight'],
            env={'SEPOLIA_RPC_URL': 'https://current.example', 'SEPOLIA_RPC': 'https://legacy.example'})
        self.assertEqual(1, code)
        self.assertEqual('FAIL', report['status'])
        self.assertNotIn('https://', json.dumps(report))

    def test_make_entry_env_forwards_the_selected_artifacts(self):
        with mock.patch.object(verifier, 'stage_preflight', return_value={'status': 'PASS'}), \
                mock.patch.object(verifier, 'stage_manifest', return_value={'status': 'PASS'}) as manifest, \
                mock.patch.object(verifier, 'stage_h5', return_value={'status': 'PASS'}) as h5:
            code, _ = self.run_main([], env={'W3_SEPOLIA_ADDRESS': ADDRESS, 'W3_CANDIDATE_SHA': 'a' * 40,
                'W3_BUILD_IDENTITY': '/ci/build-identity.json', 'W3_DEPLOYMENT_MANIFEST': '/fixture/manifest.json', 'W3_H5_ORIGIN': 'http://localhost:1234'})
        self.assertEqual(2, code)
        manifest.assert_called_once_with(Path('/fixture/manifest.json'), 'a' * 40, ADDRESS, Path('/ci/build-identity.json'))
        h5.assert_called_once_with('http://localhost:1234')

    def extension_result(self):
        # Synthetic pipe contract only, never evidence of a public wallet run.
        return dict(schemaVersion=1, scope='W3_EXTENSION_FIXED_MATRIX', candidateSHA='a' * 40, controller=ADDRESS.lower(), recipient='0x' + '2' * 40,
            status='PASS WITH BOUNDARIES', completeAcceptance=True,
            cases={key: dict(status='PASS') for key in [*(f'CW-{i:02}' for i in range(1, 17)), *(f'TN-{i:02}' for i in range(1, 9))]},
            components=dict(source=dict(status='PASS'), ci=dict(status='PASS')))

    def test_full_make_entry_delegates_to_fixed_matrix_with_external_controller(self):
        with mock.patch.object(verifier, 'run_tool', return_value=(0, '', '', self.extension_result())) as run, \
                mock.patch.object(verifier, 'stage_preflight', side_effect=AssertionError('do not repeat completed transactions/preflight')):
            code, report = self.run_main([], env={'W3_EVIDENCE_DIR': '/original/evidence', 'W3_CANDIDATE_SHA': 'a' * 40, 'W3_SEPOLIA_ADDRESS': ADDRESS,
                'SEPOLIA_RPC_URL': 'https://primary.example', 'SEPOLIA_RPC_FALLBACK_URL': 'https://fallback.example', 'SEPOLIA_RPC': ''})
        self.assertEqual(0, code); self.assertEqual('PASS WITH BOUNDARIES', report['status']); self.assertTrue(report['completeAcceptance'])
        self.assertNotIn('userGated', report, 'never fabricate human wallet approvals')
        command = run.call_args.args[0]
        self.assertEqual(ADDRESS, command[command.index('--controller') + 1]); self.assertEqual('/original/evidence', command[command.index('--evidence') + 1])

    def test_false_complete_foreign_or_missing_matrix_and_nonzero_exit_never_turn_green(self):
        original = self.extension_result()
        changes = [lambda r: r.update(candidateSHA='b' * 40), lambda r: r.update(controller='0x' + '2' * 40), lambda r: r.update(recipient='0x' + '3' * 40),
            lambda r: r.update(completeAcceptance=False), lambda r: r['cases'].pop('TN-08'),
            lambda r: r['cases']['CW-15'].update(status='BLOCKED'), lambda r: r['components']['ci'].update(status='FAIL')]
        for change in changes:
            edited = copy.deepcopy(original); change(edited)
            with self.subTest(change=change), mock.patch.object(verifier, 'run_tool', return_value=(0, '', '', edited)):
                code, report = self.run_main(['--evidence', '/original/evidence', '--candidate', 'a' * 40, '--address', ADDRESS])
                self.assertEqual(1, code); self.assertFalse(report['completeAcceptance']); self.assertEqual(0, report['stages']['extension']['exit'])
        for exit_code in (1, 2, 137):
            with mock.patch.object(verifier, 'run_tool', return_value=(exit_code, '', '', original)):
                code, report = self.run_main(['--evidence', '/original/evidence', '--candidate', 'a' * 40, '--address', ADDRESS])
                self.assertNotEqual(0, code); self.assertFalse(report['completeAcceptance'])
                self.assertEqual(exit_code, report['stages']['extension']['exit'])

    def test_evidence_without_external_controller_or_candidate_blocks_before_child(self):
        with mock.patch.object(verifier, 'run_tool', side_effect=AssertionError('missing authority must not invoke the child')):
            code, report = self.run_main(['--evidence', '/original/evidence'], env={'W3_SEPOLIA_ADDRESS': '', 'W3_CANDIDATE_SHA': ''})
        self.assertEqual(2, code); self.assertFalse(report['completeAcceptance']); self.assertIsNone(report['stages']['extension']['exit'])


if __name__ == '__main__':
    unittest.main()
