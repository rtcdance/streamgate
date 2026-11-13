import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import types
import unittest
from unittest import mock

sys.dont_write_bytecode = True
SCRIPT = Path(__file__).with_name('wallet_restore_control.py')
spec = importlib.util.spec_from_file_location('wallet_restore_control', SCRIPT)
control = importlib.util.module_from_spec(spec); spec.loader.exec_module(control)
OLD = '11111111-1111-4111-8111-111111111111'


class WalletRestoreControlTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory(); self.addCleanup(directory.cleanup)
        self.root = Path(directory.name); self.barrier = self.root / '.private/barrier'; self.barrier.mkdir(parents=True)
        (self.barrier / 'ready.json').write_text(json.dumps({'phase': 'READY', 'oldEpoch': OLD, 'tokenExpiresAt': time.time() + 60, 'chainId': 31337}))
        self.args = types.SimpleNamespace(barrier=self.barrier, postgres='c' * 64, project='own-project', candidate='a' * 40, output=self.root / 'control.json')
        self.identity = {'Id': 'c' * 64, 'Name': '/own-project-postgres-1', 'State': {'Running': True},
                         'Config': {'Labels': {'com.docker.compose.project': 'own-project', 'com.streamgate.verify.run': 'own-project'}}}

    def test_foreign_or_stopped_database_is_never_mutated(self):
        for change in ('compose', 'run', 'name', 'stopped'):
            identity = copy.deepcopy(self.identity)
            if change in ('compose', 'run'): identity['Config']['Labels']['com.docker.compose.project' if change == 'compose' else 'com.streamgate.verify.run'] = 'other-project'
            elif change == 'name': identity['Name'] = '/other-postgres-1'
            else: identity['State']['Running'] = False
            with self.subTest(change=change), mock.patch.object(control.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, json.dumps([identity]))) as run:
                self.assertEqual(1, control.run(self.args))
                self.assertEqual(1, run.call_count)
                self.assertEqual(['docker', 'inspect'], run.call_args.args[0][:2])
                self.assertFalse(json.loads((self.barrier / 'done.json').read_text())['ok'])

    def fake_run(self, fault=''):
        epoch = OLD; reads = 0

        def run(command, **kwargs):
            nonlocal epoch, reads
            if command[:2] == ['docker', 'inspect']: return subprocess.CompletedProcess(command, 0, json.dumps([self.identity]))
            self.assertIn(control.DATABASE, command)
            if 'pg_dump' in command:
                kwargs['stdout'].write(b'-- controlled unit snapshot\n')
                return subprocess.CompletedProcess(command, 0, None)
            if kwargs.get('stdin') is not None:
                self.assertEqual(b'-- controlled unit snapshot\n', kwargs['stdin'].read())
                if fault == 'restore-fail': return subprocess.CompletedProcess(command, 17, b'')
                if fault == 'timeout': raise subprocess.TimeoutExpired(command, 15)
                return subprocess.CompletedProcess(command, 0, b'')
            sql = command[-1]
            if sql.startswith('UPDATE'): epoch = sql.split("'")[1]; return subprocess.CompletedProcess(command, 0, b'UPDATE 1\n')
            reads += 1
            value = '22222222-2222-4222-8222-222222222222' if fault == 'wrong-snapshot' and reads == 2 else epoch
            return subprocess.CompletedProcess(command, 0, (value + '\n').encode())
        return run

    def test_dump_restore_and_rotation_must_all_succeed_before_resume(self):
        with mock.patch.object(control.subprocess, 'run', side_effect=self.fake_run()):
            self.assertEqual(0, control.run(self.args))
        report = json.loads(self.args.output.read_text()); done = json.loads((self.barrier / 'done.json').read_text())
        self.assertTrue(done['ok']); self.assertNotEqual(OLD, done['newEpoch'])
        self.assertEqual(['before', 'dump', 'restore', 'restored', 'rotate', 'rotated'], [step['name'] for step in report['steps']])
        self.assertTrue(all(step['exit'] == 0 for step in report['steps']))
        self.assertFalse(report['completeAcceptance'])
        self.assertEqual(0o600, (self.barrier / 'postgres.sql').stat().st_mode & 0o777)

    def test_restore_failure_or_timeout_is_preserved_without_retry_or_rotation(self):
        for fault in ('restore-fail', 'timeout', 'wrong-snapshot'):
            with self.subTest(fault=fault), mock.patch.object(control.subprocess, 'run', side_effect=self.fake_run(fault)):
                self.assertEqual(1, control.run(self.args))
            report = json.loads(self.args.output.read_text())
            self.assertFalse(report['ok']); self.assertNotIn('rotate', [step['name'] for step in report['steps']])
            self.assertEqual(1, sum(step['name'] == 'restore' for step in report['steps']))
            if fault == 'timeout': self.assertTrue(report['steps'][-1]['timedOut']); self.assertIsNone(report['steps'][-1]['exit'])

    def test_expired_or_wrong_chain_sample_never_reaches_docker(self):
        for key, value in (('tokenExpiresAt', time.time() - 1), ('chainId', 1), ('phase', 'PASS')):
            ready = {'phase': 'READY', 'oldEpoch': OLD, 'tokenExpiresAt': time.time() + 60, 'chainId': 31337}; ready[key] = value
            (self.barrier / 'ready.json').write_text(json.dumps(ready))
            with self.subTest(key=key), mock.patch.object(control.subprocess, 'run') as run:
                self.assertEqual(1, control.run(self.args)); run.assert_not_called()


if __name__ == '__main__':
    unittest.main()
