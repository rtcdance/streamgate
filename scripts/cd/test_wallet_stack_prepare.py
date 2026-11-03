import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('wallet_stack_prepare', Path(__file__).resolve().parents[1] / 'wallet-testnet/prepare_stack.py')
tool = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tool)
CANDIDATE = 'a' * 40


class StackPreparationContract(unittest.TestCase):
    def test_default_ci_requires_exact_candidate_and_successful_terminal_state(self):
        good = dict(id=123, run_attempt=3, head_sha=CANDIDATE, html_url='https://github.com/example/streamgate/actions/runs/123',
                    status='completed', conclusion='success', path='.github/workflows/ci.yml')
        with mock.patch.object(tool.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout=json.dumps(good))) as command:
            self.assertEqual(3, tool.ci_run('example/streamgate', '123', CANDIDATE)['run_attempt'])
            self.assertEqual(['gh', 'api', 'repos/example/streamgate/actions/runs/123'], command.call_args.args[0])
        for key, value in [('id', 124), ('run_attempt', 0), ('head_sha', 'b' * 40), ('status', 'in_progress'), ('conclusion', 'failure'), ('path', '.github/workflows/other.yml')]:
            with self.subTest(field=key), mock.patch.object(tool.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout=json.dumps(dict(good, **{key: value})))):
                with self.assertRaises(ValueError): tool.ci_run('example/streamgate', '123', CANDIDATE)

    def test_actual_transport_schema_accepts_preserved_earlier_attempt_but_not_foreign_or_tampered_payload(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); payload = root / 'payload'; capsule = payload / 'candidate'; capsule.mkdir(parents=True)
            identity = capsule / 'build-identity.json'; identity.write_text('{}')
            entry = dict(path='candidate/build-identity.json', size=2, sha256=hashlib.sha256(b'{}').hexdigest())
            producer = root / 'transport.json'
            value = dict(schemaVersion=1, name='ci-acceptance-' + CANDIDATE, sourceSHA=CANDIDATE, runID='123', runAttempt='1', workflow='CI', job='acceptance',
                         entries=[entry], bundleSHA256=tool.evidence_transport.bundle_hash([entry]))
            args = SimpleNamespace(producer=producer, producer_root=payload, ci_root=capsule, build_identity=identity)
            producer.write_text(json.dumps(value)); run = dict(id=123, run_attempt=3)
            tool.verify_producer(args, run, CANDIDATE)
            for change in [dict(workflow='Other'), dict(job='unit')]:
                producer.write_text(json.dumps(dict(value, **change)))
                with self.subTest(change=change), self.assertRaisesRegex(ValueError, 'acceptance job'): tool.verify_producer(args, run, CANDIDATE)
            for change in [dict(sourceSHA='b' * 40), dict(runID='124'), dict(runAttempt='4'), dict(name='ci-build-' + CANDIDATE)]:
                producer.write_text(json.dumps(dict(value, **change)))
                with self.subTest(change=change), self.assertRaises(tool.evidence_transport.TransportError): tool.verify_producer(args, run, CANDIDATE)
            producer.write_text(json.dumps(value)); identity.write_text('{"tampered":true}')
            with self.assertRaises(tool.evidence_transport.TransportError): tool.verify_producer(args, run, CANDIDATE)
            identity.write_text('{}'); (payload / 'unexpected').write_text('unlisted')
            with self.assertRaises(tool.evidence_transport.TransportError): tool.verify_producer(args, run, CANDIDATE)

    def test_build_index_outside_producer_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); payload = root / 'payload'; payload.mkdir()
            entry = dict(path='a', size=0, sha256=hashlib.sha256(b'').hexdigest())
            producer = root / 'transport.json'
            producer.write_text(json.dumps(dict(schemaVersion=1, name='ci-acceptance-' + CANDIDATE, sourceSHA=CANDIDATE, runID=123,
                runAttempt=1, workflow='CI', job='acceptance', entries=[entry], bundleSHA256=tool.evidence_transport.bundle_hash([entry]))))
            args = SimpleNamespace(producer=producer, producer_root=payload, ci_root=payload, build_identity=root / 'external.json')
            with self.assertRaisesRegex(ValueError, 'inside'): tool.verify_producer(args, dict(id=123, run_attempt=1), CANDIDATE)

    def test_cli_reports_blocked_without_creating_directory_when_rpc_is_missing(self):
        with tempfile.TemporaryDirectory() as temporary:
            destination = Path(temporary) / 'never-created'
            command = [sys.executable, str(Path(tool.__file__)), '--candidate', CANDIDATE, '--controller', '0x' + '1' * 40,
                '--recipient', '0x' + '2' * 40, '--repository', 'example/streamgate', '--ci-run', '123', '--producer', temporary + '/absent', '--producer-root', temporary,
                '--ci-root', temporary, '--build-identity', temporary + '/absent', '--directory', str(destination)]
            result = subprocess.run(command, env={'PATH': '/usr/bin:/bin'}, capture_output=True, text=True)
            self.assertEqual(2, result.returncode, result.stderr)
            self.assertIn('BLOCKED', result.stdout); self.assertFalse(destination.exists())


if __name__ == '__main__':
    unittest.main()
