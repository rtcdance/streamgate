"""Launcher safety controls with synthetic Docker responses, not live acceptance."""
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
SCRIPT = Path(__file__).resolve().parents[1] / 'wallet-testnet/launch_stack.py'
spec = importlib.util.spec_from_file_location('wallet_stack_launcher', SCRIPT)
tool = importlib.util.module_from_spec(spec); spec.loader.exec_module(tool)
CANDIDATE = 'a' * 40


class PublicStackLaunchTests(unittest.TestCase):
    def fixture(self, root):
        args = SimpleNamespace(candidate=CANDIDATE, controller='0x' + '1' * 40, output=root / 'launch.json')
        prepared = dict(project='sg-w3-aaaaaaaa-test0001', environment={'PATH': '/usr/bin:/bin'}, directory=root, capsule=root / 'ci',
            source=lambda _: b'', h5Paths=[], originalCI={}, variables=dict(SEPOLIA_RPC_URL='https://primary.example/secret', SEPOLIA_RPC_FALLBACK_URL='https://fallback.example/secret'),
            config=dict(services={'monolith': dict(image='sha256:' + '1' * 64), 'redis': dict(image='redis@sha256:' + '2' * 64)}))
        report = dict(commands=[], status='BLOCKED', completeAcceptance=False)
        return args, prepared, report

    def start_fixture(self, existing=False, residual=False, preflight_exit=0, startup_exit=0, missing=None):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); args, prepared, report = self.fixture(root)
            commands = []
            def read(command):
                if command[:2] == ['docker', 'ps']: return b'owned-container' if existing else b''
                if command[:3] in (['docker', 'network', 'ls'], ['docker', 'volume', 'ls']): return b'residual' if residual else b''
                raise AssertionError('unexpected read command')
            def execute(command, **kwargs):
                commands.append(command)
                if command[0] == sys.executable: code = preflight_exit
                elif command[:3] == ['docker', 'image', 'inspect']: code = int(command[-1] == missing)
                elif command[:2] == ['docker', 'pull']: code = 0
                elif command[:2] == ['docker', 'compose']: code = startup_exit
                else: raise AssertionError('unexpected or destructive Docker command')
                return SimpleNamespace(returncode=code, stdout=b'private-provider-marker', stderr=b'private-provider-marker')
            with mock.patch.object(tool.wallet_runtime, 'command', side_effect=read), \
                    mock.patch.object(tool.wallet_runtime, 'inspect_stack', return_value={'monolith': {'imageID': 'unit-only'}}), \
                    mock.patch.object(tool.subprocess, 'run', side_effect=execute):
                try: tool.start(args, prepared, report); error = None
                except (ValueError, OSError) as caught: error = caught
            require_exit = args.output.with_suffix('.network.exit')
            return report, commands, error, require_exit.read_text().strip() if require_exit.exists() else None

    def test_new_project_uses_preflight_then_existing_images_and_one_bounded_no_build_start(self):
        report, commands, error, exit_value = self.start_fixture()
        self.assertIsNone(error); self.assertEqual('STARTED_HEALTHY', report['status']); self.assertEqual('0', exit_value)
        self.assertEqual(1, sum(command[:2] == ['docker', 'compose'] for command in commands))
        self.assertEqual(['up', '-d', '--no-build', '--pull', 'never', '--wait', '--wait-timeout', '180'], commands[-1][-8:])
        self.assertNotIn('private-provider-marker', json.dumps(report))
        self.assertFalse(report['completeAcceptance'])

    def test_healthy_existing_project_is_reused_without_preflight_pull_or_restart(self):
        report, commands, error, exit_value = self.start_fixture(existing=True)
        self.assertIsNone(error); self.assertEqual('REUSED_HEALTHY', report['status']); self.assertEqual([], commands); self.assertIsNone(exit_value)

    def test_residual_resources_and_failed_preflight_never_start_containers(self):
        for options, expected in [(dict(residual=True), None), (dict(preflight_exit=2), '2')]:
            report, commands, error, exit_value = self.start_fixture(**options)
            self.assertIsInstance(error, tool.prepare.deployment.BlockedError); self.assertEqual(expected, exit_value)
            self.assertFalse(any(command[:2] == ['docker', 'compose'] for command in commands))

    def test_missing_infrastructure_is_pulled_only_at_its_pinned_amd64_digest(self):
        image = 'redis@sha256:' + '2' * 64
        report, commands, error, _ = self.start_fixture(missing=image)
        self.assertIsNone(error)
        self.assertIn(['docker', 'pull', '--platform', 'linux/amd64', image], commands)
        report, commands, error, _ = self.start_fixture(missing='sha256:' + '1' * 64)
        self.assertIsInstance(error, tool.prepare.deployment.BlockedError)
        self.assertFalse(any(command[:2] == ['docker', 'pull'] for command in commands))

    def test_unhealthy_start_preserves_project_and_original_nonzero_exit(self):
        report, commands, error, _ = self.start_fixture(startup_exit=17)
        self.assertIsInstance(error, tool.prepare.deployment.BlockedError)
        self.assertEqual(17, report['commands'][-1]['exit'])
        self.assertFalse(any(any(word in command for word in ('down', 'rm', 'restart', 'build')) for command in commands))

    def test_kernel_lock_prevents_duplicate_launch_and_releases_after_exception(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with self.assertRaisesRegex(RuntimeError, 'controlled abort'):
                with tool.project_lock(root):
                    with self.assertRaises(tool.prepare.deployment.BlockedError):
                        with tool.project_lock(root): pass
                    raise RuntimeError('controlled abort')
            with tool.project_lock(root): pass

    def test_unfrozen_cli_blocks_before_docker_and_existing_output_is_preserved(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); output = root / 'launch.json'
            command = [sys.executable, str(SCRIPT), '--candidate', CANDIDATE, '--controller', '0x' + '1' * 40, '--recipient', '0x' + '2' * 40, '--repository', 'example/streamgate', '--ci-run', '123',
                '--producer', str(root / 'missing'), '--producer-root', str(root / 'missing'), '--ci-root', str(root / 'missing'),
                '--build-identity', str(root / 'missing'), '--directory', str(root / 'missing'), '--output', str(output)]
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(2, result.returncode, result.stderr)
            report = json.loads(output.read_text()); self.assertEqual('BLOCKED', report['status']); self.assertEqual([], report['commands'])
            before = output.read_bytes(); result = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(1, result.returncode); self.assertEqual(before, output.read_bytes())


if __name__ == '__main__':
    unittest.main()
