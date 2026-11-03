"""Closeout-gate rejection controls; mocked source is never candidate evidence."""
import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

import verify_web3_extension as gate

CANDIDATE = 'a' * 40


class ExtensionCloseoutTests(unittest.TestCase):
    def cost(self):
        proof = dict(method='eth_call', params=[dict(to='0x' + '1' * 40, input='0x1626ba7e', gas='0x7a120'), '0x10'],
            result='0x1626ba7e' + '00' * 28, upstreamStatus=200, inputRedacted=True, inputSHA256='a' * 64, inputBytes=196)
        return dict(schemaVersion=1, candidateSHA=CANDIDATE, evidenceContext='CANDIDATE', chainId=31337, scope='REAL_CW_SEGMENT_RPC', originalExit=0,
            wallet='0x' + '1' * 40, rpcCallFormat='SELECTOR_AND_INPUT_DIGEST', admissionRPC=[proof], segmentRPC=[], refreshRPC=[copy.deepcopy(proof)],
            lease=dict(startedAtMs=10000, finishedAtMs=11000, capabilityExpiresAtMs=50000, accessExpiresAtMs=60000, capabilitySHA256='b' * 64),
            segments=[dict(transport=transport, status=200, subject='0x' + '1' * 40, bytes=188, sha256='c' * 64, credentialSHA256='b' * 64) for transport in ('http', 'grpc') for _ in range(100)])

    def evaluate(self, root, changes=None):
        selection = dict(schemaVersion=1, candidateSHA=CANDIDATE, controller='0x' + '1' * 40, recipient='0x' + '2' * 40,
                         producer='missing-original-producer.json')
        selection.update(changes or {})
        (root / 'extension.json').write_text(json.dumps(selection))
        args = SimpleNamespace(candidate=CANDIDATE, controller='0x' + '1' * 40, recipient='0x' + '2' * 40, evidence=root, source_root=gate.ROOT)
        # Only isolate the source-pin precondition in rejection controls. No
        # acceptance component, CI metadata or public-chain result is mocked.
        with mock.patch.object(gate, 'git', side_effect=lambda source, candidate, path: (gate.ROOT / path).read_bytes()), \
                mock.patch.object(gate.ci_provenance, 'default_run', side_effect=AssertionError('missing producer must block before any GitHub request')):
            return gate.verify(args)

    def test_missing_original_evidence_blocks_all_fixed_cases_without_network_or_success(self):
        with tempfile.TemporaryDirectory() as temporary:
            report, code = self.evaluate(Path(temporary))
        self.assertEqual(2, code); self.assertEqual('BLOCKED', report['status']); self.assertFalse(report['completeAcceptance'])
        self.assertEqual(set(gate.CASES), set(report['cases'])); self.assertEqual(24, len(report['cases']))
        self.assertTrue(all(case['status'] == 'BLOCKED' for case in report['cases'].values()))
        self.assertEqual('PASS', report['components']['source']['status'])

    def test_caller_cannot_shrink_matrix_declare_success_or_select_foreign_candidate(self):
        for change in [dict(required=['CW-01']), dict(status='PASS'), dict(passed=True), dict(completeAcceptance=True), dict(candidateSHA='b' * 40), dict(controller='0x' + '2' * 40), dict(recipient='0x' + '3' * 40)]:
            with self.subTest(change=change), tempfile.TemporaryDirectory() as temporary:
                report, code = self.evaluate(Path(temporary), change)
                self.assertEqual(1, code); self.assertEqual('FAIL', report['components']['source']['status'])
                self.assertFalse(report['completeAcceptance']); self.assertEqual(24, len(report['cases']))

    def test_path_traversal_absolute_and_symlink_inputs_are_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); evidence = gate.Evidence(root)
            (root / 'original').write_text('0'); (root / 'link').symlink_to(root / 'original')
            for name in ('../foreign', '/tmp/foreign', 'link'):
                with self.subTest(path=name), self.assertRaises(ValueError): evidence.path(name)
            self.assertEqual(0, evidence.exit('original'))
            (root / 'bad-exit').write_text('PASS')
            with self.assertRaises(ValueError): evidence.exit('bad-exit')

    def test_cli_preserves_existing_report_and_does_not_print_private_marker(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); output = root / 'original.json'; output.write_text('preserve-original-private-marker')
            result = subprocess.run([sys.executable, str(Path(gate.__file__)), '--candidate', CANDIDATE,
                '--evidence', str(root), '--output', str(output)], capture_output=True, text=True)
            self.assertEqual(1, result.returncode); self.assertEqual('preserve-original-private-marker', output.read_text())
            self.assertNotIn('private-marker', result.stdout + result.stderr)

    def test_segment_cost_rejects_claimed_counts_without_real_events_or_deliveries(self):
        for value in [{}, dict(schemaVersion=1, candidateSHA=CANDIDATE, chainId=31337, scope='REAL_CW_SEGMENT_RPC', originalExit=0,
                              admissionRPC=[], segmentRPC=[], refreshRPC=[], segments=[], requests=100, proofCalls=0)]:
            with self.assertRaises(ValueError): gate.segment_cost(value, CANDIDATE)

    def test_missing_public_rpc_is_dependency_blocked(self):
        with mock.patch.dict(gate.os.environ, {'W3_SEPOLIA_RPC_PRIMARY': '', 'W3_SEPOLIA_RPC_FALLBACK': ''}):
            with self.assertRaises(gate.oracle.deployment.BlockedError): gate.public_endpoints()

    def test_rpc_cost_requires_original_pinned_proofs_and_all_deliveries_within_lease(self):
        value = self.cost(); gate.segment_cost(value, CANDIDATE)
        changes = [lambda r: r['admissionRPC'][0]['params'][0].update(gas='0x7a121'),
            lambda r: r['refreshRPC'][0]['params'].__setitem__(1, 'latest'),
            lambda r: r['admissionRPC'][0].update(result='0x00' + '1626ba7e' + '00' * 27),
            lambda r: r['admissionRPC'][0].update(inputRedacted=False),
            lambda r: r['segmentRPC'].append(r['admissionRPC'][0]), lambda r: r['segments'].pop(),
            lambda r: r['segments'][0].update(credentialSHA256='0' * 64),
            lambda r: r['lease'].update(finishedAtMs=50000), lambda r: r.update(evidenceContext='DEVELOPMENT')]
        for change in changes:
            edited = copy.deepcopy(value); change(edited)
            with self.subTest(change=change), self.assertRaises(ValueError): gate.segment_cost(edited, CANDIDATE)
        value['evidenceContext'] = 'DEVELOPMENT'; gate.segment_cost_component(value)


if __name__ == '__main__':
    unittest.main()
