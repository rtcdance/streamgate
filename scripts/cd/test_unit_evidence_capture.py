"""Synthetic gate controls only; these fixtures are never application evidence."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
REQUIRED = json.loads((ROOT / 'scripts/cd/required-tests.json').read_text())['suites']['wallet-unit-tests']


def events(fault=''):
    values = []
    for package in sorted({row['package'] for row in REQUIRED}):
        values.append(dict(Action='start', Package=package))
        for row in [row for row in REQUIRED if row['package'] == package]:
            if fault == 'missing' and row == REQUIRED[0]: continue
            values.append(dict(Action='run', Package=package, Test=row['test']))
            if fault == 'skip-child' and row == REQUIRED[0]:
                for action in ('run', 'skip'): values.append(dict(Action=action, Package=package, Test=row['test'] + '/required_negative'))
            values.append(dict(Action='pass', Package=package, Test=row['test']))
        values.append(dict(Action='pass', Package=package))
    return ''.join(json.dumps(value) + '\n' for value in values)


class UnitEvidenceCapture(unittest.TestCase):
    def run_capture(self, directory, original_exit=0, fault='', no_test_packages=()):
        directory = Path(directory); binary = directory / 'bin'; binary.mkdir()
        fake = binary / 'go'
        fake.write_text('\n'.join([
            '#!/bin/sh',
            'if [ "$1" = list ]; then',
            '  if [ "$2" = "-f" ]; then cat "$FIXTURE_TEST_PACKAGES"; else cat "$FIXTURE_PACKAGES"; fi',
            '  exit 0',
            'fi',
            'echo "$*" >> "$FIXTURE_TRACE"',
            'for pkg in "$@"; do',
            '  case "$pkg" in github.com/rtcdance/streamgate/*)',
            '    if grep -qxF "$pkg" "$FIXTURE_TEST_PACKAGES"; then grep -F "\\"Package\\": \\"$pkg\\"" "$FIXTURE_EVENTS"',
            '    else printf \'{"Action": "start", "Package": "%s"}\\n\' "$pkg"',
            '      printf \'{"Action": "output", "Package": "%s", "Output": "no test files"}\\n\' "$pkg"',
            '      printf \'{"Action": "skip", "Package": "%s"}\\n\' "$pkg"',
            '    fi ;;',
            '  esac',
            'done',
            'echo original-stderr >&2',
            'exit "$FIXTURE_EXIT"', '']) )
        fake.chmod(0o755)
        stream = directory / 'go.json'; stream.write_text(events(fault))
        names = sorted({row['package'] for row in REQUIRED})
        packages = directory / 'packages'; packages.write_text('\n'.join(names + sorted(no_test_packages)) + '\n')
        test_packages = directory / 'test_packages'; test_packages.write_text('\n'.join(names) + '\n')
        output = directory / 'output'; trace = directory / 'trace'
        env = dict(os.environ, PATH=str(binary) + os.pathsep + os.environ['PATH'], FIXTURE_PACKAGES=str(packages),
                   FIXTURE_TEST_PACKAGES=str(test_packages), FIXTURE_TRACE=str(trace), FIXTURE_EVENTS=str(stream),
                   FIXTURE_EXIT=str(original_exit))
        result = subprocess.run(['bash', 'scripts/cd/run_unit_tests.sh', str(output)], cwd=ROOT, env=env, capture_output=True, text=True)
        self.assertEqual(str(original_exit), (output / 'unit-tests.exit').read_text().strip())
        self.assertEqual(stream.read_bytes(), (output / 'unit-tests.json').read_bytes())
        self.assertEqual('original-stderr\n', (output / 'unit-tests.stderr').read_text())
        commands = trace.read_text().splitlines(); self.assertEqual(1, len(commands), 'must not execute the coverage suite twice')
        for flag in ('-race', '-count=1', '-json', '-covermode=atomic', '-coverprofile='):
            self.assertIn(flag, commands[0])
        for pkg in no_test_packages:
            self.assertNotIn(pkg, commands[0], 'package without test files must not be streamed to the required gate')
            self.assertNotIn(pkg, (output / 'unit-tests.json').read_text(), 'no-test package must not enter the evidence stream')
        return result

    def test_original_success_requires_every_required_case(self):
        with tempfile.TemporaryDirectory() as directory:
            result = self.run_capture(directory)
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)

    def test_original_failure_cannot_be_hidden_by_passing_events(self):
        with tempfile.TemporaryDirectory() as directory:
            result = self.run_capture(directory, original_exit=37)
            self.assertNotEqual(0, result.returncode)
            self.assertIn('go test exited 37', result.stderr)

    def test_missing_required_test_cannot_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            result = self.run_capture(directory, fault='missing')
            self.assertNotEqual(0, result.returncode)
            self.assertIn('required test did not run and pass', result.stderr)

    def test_skipped_required_subcase_cannot_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            result = self.run_capture(directory, fault='skip-child')
            self.assertNotEqual(0, result.returncode)
            self.assertIn('required test/subtest skipped', result.stderr)

    def test_no_test_files_package_is_streamed_out_of_the_required_gate(self):
        # go test -json ends [no test files] packages with a package-level
        # "skip", which the required gate rightly rejects; the runner must not
        # stream them at all instead of weakening the gate.
        with tempfile.TemporaryDirectory() as directory:
            result = self.run_capture(directory, no_test_packages=('github.com/rtcdance/streamgate/docs',))
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()
