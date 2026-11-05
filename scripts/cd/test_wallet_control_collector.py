"""Synthetic collector safety contracts, never wallet or public-chain acceptance."""
from contextlib import redirect_stdout
import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

from test_public_controls import fixture

spec = importlib.util.spec_from_file_location('tested_control_collector', Path(__file__).resolve().parents[1] / 'wallet-testnet/observe_controls.py')
tool = importlib.util.module_from_spec(spec); spec.loader.exec_module(tool)
CANDIDATE = 'a' * 40


class PublicCollectorTests(unittest.TestCase):
    def test_browser_transport_is_explicit_and_cua_never_receives_a_browser_wide_endpoint(self):
        self.assertEqual({'W3_BROWSER_TRANSPORT': 'cua'}, tool.browser_environment(SimpleNamespace(transport='cua', cdp=None)))
        self.assertEqual({'W3_CDP_ENDPOINT': 'http://localhost:9222'}, tool.browser_environment(SimpleNamespace(transport='cdp', cdp='http://localhost:9222')))
        for args in (SimpleNamespace(transport='cua', cdp='http://localhost:9222'), SimpleNamespace(transport='cdp', cdp=None), SimpleNamespace(transport='unknown', cdp=None)):
            with self.subTest(transport=args.transport), self.assertRaises(ValueError): tool.browser_environment(args)

    def test_private_context_is_exclusive_and_does_not_replace_originals(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'context.json'
            tool.private_json(path, dict(synthetic=True))
            self.assertEqual(0o600, path.stat().st_mode & 0o777)
            with self.assertRaises(FileExistsError): tool.private_json(path, dict(synthetic=False))
            self.assertEqual(dict(synthetic=True), json.loads(path.read_text()))

    def runtime(self, egress=True):
        ctx = dict(candidateSHA=CANDIDATE, project='sg-w3-aaaaaaaa-unit', container='unit-container', image='sha256:' + 'b' * 64,
                   dataNetwork='unit-data', egressNetwork='unit-egress', egressAliases=['monolith', 'original-alias'], observerName='unit-observer')
        value = dict(Id=ctx['container'], Image=ctx['image'], State=dict(Running=True), Config=dict(Labels={
            'com.streamgate.w3.candidate': CANDIDATE, 'com.streamgate.w3.testnet': ctx['project']}), NetworkSettings=dict(Networks={ctx['dataNetwork']: {}}))
        if egress: value['NetworkSettings']['Networks'][ctx['egressNetwork']] = {}
        return ctx, value

    def test_parent_restores_only_original_rpc_network_and_aliases(self):
        ctx, value = self.runtime(egress=False)
        with mock.patch.object(tool.launch.wallet_runtime, 'command', side_effect=[json.dumps([value]).encode(), b'', b'']) as call:
            tool.restore(ctx)
        self.assertEqual(['docker', 'network', 'connect', '--alias', 'monolith', '--alias', 'original-alias', 'unit-egress', 'unit-container'], call.call_args_list[1].args[0])
        self.assertEqual(3, call.call_count)

    def test_foreign_or_stopped_runtime_never_receives_recovery_mutations(self):
        for change in [lambda v: v.update(Image='foreign-image'), lambda v: v['State'].update(Running=False),
                       lambda v: v['Config']['Labels'].update({'com.streamgate.w3.candidate': 'b' * 40}),
                       lambda v: v['NetworkSettings']['Networks'].pop('unit-data')]:
            ctx, value = self.runtime(egress=False); change(value)
            with mock.patch.object(tool.launch.wallet_runtime, 'command', return_value=json.dumps([value]).encode()) as call:
                with self.assertRaises(ValueError): tool.restore(ctx)
                self.assertEqual(1, call.call_count)

    def test_healthy_runtime_needs_no_network_restart_or_client_removal(self):
        ctx, value = self.runtime()
        with mock.patch.object(tool.launch.wallet_runtime, 'command', side_effect=[json.dumps([value]).encode(), b'']) as call:
            tool.restore(ctx)
        self.assertEqual(2, call.call_count)
        self.assertEqual(['docker', 'ps', '-aq'], call.call_args.args[0][:3])

    def fragments(self, root):
        report, private_raw, config, oracles = fixture()
        recipient = '0x' + '9' * 40
        stack = root / 'stack'; stack.mkdir()
        tool.private_json(stack / 'stack.env.json', dict(W3_CANDIDATE_SHA=CANDIDATE, W3_JWT_SECRET=config['jwtSecret']))
        for role in ('eoa', 'contract'):
            directory = root / role; directory.mkdir()
            chain = dict(oracles[role + '-transferred']['report'], phase=role + '-transferred', recipient=recipient,
                         lastOperationCreatedAtMs=config['transferStartedAt'][role])
            oracle = root / (role + '-oracle.json'); oracle.write_text(json.dumps(chain))
            oracle_exit = root / (role + '-oracle.exit'); oracle_exit.write_text('0\n')
            transfer = copy.deepcopy(report['transfers'][role]); transfer['oracleSHA256'] = hashlib.sha256(oracle.read_bytes()).hexdigest()
            value = {key: report[key] for key in ('candidateSHA', 'controller', 'wallet', 'nft', 'content', 'origin', 'mode')}
            value.update(schemaVersion=1, component='SEPOLIA_PUBLIC_CONTROL_PHASE', phase=role, status='CONTROL_PHASE_OBSERVED',
                         clientExit=dict(code=0, signal=None), completeAcceptance=False, publicWalletSignoff=False,
                         privateSHA256=hashlib.sha256(private_raw).hexdigest(), transfer=transfer)
            if role == 'contract': value.update(dependencyOutage=report['dependencyOutage'], reload=report['reload'])
            tool.private_json(directory / 'result.json', value)
            tool.private_json(directory / 'context.private.json', dict(recipient=recipient, sessionBinding=config['sessionBindings'][role],
                                                                       oracle=str(oracle), oracleExit=str(oracle_exit), stackDirectory=str(stack)))
            tool.private_json(directory / 'collector.json', dict(scope='SEPOLIA_CONTROL_COLLECTOR', candidateSHA=CANDIDATE,
                                                                 status='CONTROL_PHASE_OBSERVED', originalNodeExit=0, restoreExit=0))
            (directory / 'controls.private.json').write_bytes(private_raw); (directory / 'collector.exit').write_text('0\n')
        argv = ['collector', 'merge', '--candidate', CANDIDATE, '--controller', config['controller'], '--recipient', recipient,
                '--wallet', config['wallet'], '--nft', config['nft'], '--content', config['content'], '--origin', config['origin'],
                '--mode', config['mode'], '--eoa', str(root / 'eoa'), '--contract', str(root / 'contract'), '--output', str(root / 'merged')]
        return argv, config

    def run_merge(self, argv):
        with mock.patch.object(sys, 'argv', argv), mock.patch.object(tool, 'pinned'), redirect_stdout(io.StringIO()) as output:
            code = tool.merge_main()
        return code, output.getvalue()

    def test_complete_synthetic_control_fragments_merge_without_exporting_runtime_key(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); argv, config = self.fragments(root)
            code, log = self.run_merge(argv)
            self.assertEqual(0, code); self.assertNotIn(config['jwtSecret'], log)
            report = json.loads((root / 'merged/result.json').read_text())
            self.assertFalse(report['completeAcceptance']); self.assertNotIn('jwtSecret', report)
            self.assertNotIn(config['jwtSecret'], json.dumps(report))
            self.assertEqual('0', (root / 'merged/controls.exit').read_text().strip())
            self.assertEqual(0o600, (root / 'merged/controls.private.json').stat().st_mode & 0o777)

    def test_nonzero_original_exit_changed_private_bytes_or_wrong_runtime_key_reject_merge(self):
        for change in ('exit', 'chain-exit', 'private', 'key'):
            with self.subTest(change=change), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary); argv, _ = self.fragments(root)
                if change == 'exit': (root / 'eoa/collector.exit').write_text('1\n')
                elif change == 'chain-exit': (root / 'eoa-oracle.exit').write_text('137\n')
                elif change == 'private':
                    with (root / 'eoa/controls.private.json').open('ab') as stream: stream.write(b' ')
                else:
                    path = root / 'stack/stack.env.json'; values = json.loads(path.read_text()); values['W3_JWT_SECRET'] = 'different-secret-from-the-actual-runtime'
                    path.write_text(json.dumps(values))
                code, _ = self.run_merge(argv)
                self.assertEqual(1, code); self.assertFalse((root / 'merged').exists())


if __name__ == '__main__': unittest.main()
