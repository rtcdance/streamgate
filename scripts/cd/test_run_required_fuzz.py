import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


RUNNER = Path(__file__).with_name("run-required-fuzz.sh")
MODULE = "github.com/rtcdance/streamgate/"
PRESENT = MODULE + "pkg/fake"

# The exact shape the Go fuzz coordinator prints when its budget deadline races
# its own shutdown: the deadline is reported as the failure, yet real mutations
# ran and no failing input was recorded.
BENIGN_DEADLINE = (
    "fuzz: elapsed: 0s, gathering baseline coverage: 1/1 completed, now fuzzing with 4 workers\n"
    "fuzz: elapsed: 3s, execs: 45359 (15118/sec), new interesting: 1 (total: 2)\n"
    "fuzz: elapsed: 5s, execs: 74134 (13504/sec), new interesting: 1 (total: 2)\n"
    "--- FAIL: FuzzPresent (5.14s)\n"
    "    context deadline exceeded\n"
    "FAIL\n"
    "exit status 1\n"
)


def entry(package, fuzz):
    return {"package": package, "fuzz": fuzz}


class RequiredFuzzRunner(unittest.TestCase):
    """Fail-closed properties of the required-fuzz contract runner."""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = Path(self._tmp.name)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        package = self.root / "pkg" / "fake"
        package.mkdir(parents=True)
        (package / "fuzz_test.go").write_text(
            'package fake\n\nimport "testing"\n\nfunc FuzzPresent(f *testing.F) {}\n'
        )
        self.record = self.root / "go-invocations.txt"
        self.output = self.root / "go-output.txt"

    def _install_go(self, exit_code, output=""):
        """Replace `go` with a stub that records its arguments, prints and exits."""
        self.record.write_text("")
        self.output.write_text(output)
        body = (
            "#!/usr/bin/env bash\n"
            f"printf '%s\\n' \"$*\" >> {self.record}\n"
            f"cat {self.output}\n"
            f"exit {exit_code}\n"
        )
        stub = self.bin / "go"
        stub.write_text(body)
        stub.chmod(0o755)

    def invoke(self, required, *, seconds="5", go_exit=0, go_output=""):
        self._install_go(go_exit, go_output)
        manifest = self.root / "manifest.json"
        manifest.write_text(json.dumps({"version": 1, "required": required}))
        env = dict(os.environ)
        env["PATH"] = f"{self.bin}{os.pathsep}{env['PATH']}"
        env["FUZZ_LOG_DIR"] = str(self.root / "logs")
        env["REQUIRED_FUZZ_MANIFEST"] = str(manifest)
        return subprocess.run(
            ["bash", str(RUNNER), seconds],
            cwd=str(self.root), text=True, capture_output=True, env=env,
        )

    def invocations(self):
        return [line for line in self.record.read_text().splitlines() if line]

    def test_present_target_passes_and_is_fuzzed(self):
        result = self.invoke([entry(PRESENT, "FuzzPresent")])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(self.invocations()), 1)
        self.assertIn("./pkg/fake", self.invocations()[0])
        self.assertIn("-fuzz ^FuzzPresent$", self.invocations()[0])
        self.assertIn("-fuzztime 5s", self.invocations()[0])
        identities = (self.root / "logs" / "required-fuzz.identities").read_text()
        self.assertEqual(identities, "pkg/fake FuzzPresent\n")

    def test_missing_target_fails_without_running_go(self):
        result = self.invoke([entry(MODULE + "pkg/absent", "FuzzAbsent")])
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("GATE FAILURE", result.stderr)
        self.assertEqual(self.invocations(), [])

    def test_target_required_from_the_wrong_package_fails(self):
        other = self.root / "pkg" / "fake2"
        other.mkdir(parents=True)
        (other / "fuzz_test.go").write_text(
            'package fake2\n\nimport "testing"\n\nfunc FuzzOther(f *testing.F) {}\n'
        )
        result = self.invoke([entry(MODULE + "pkg/fake2", "FuzzPresent")])
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.invocations(), [])

    def test_nonzero_fuzz_exit_fails(self):
        result = self.invoke([entry(PRESENT, "FuzzPresent")], go_exit=1)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("GATE FAILURE", result.stderr)

    def test_only_the_engine_budget_deadline_is_accepted_once(self):
        """A racing shutdown is repeated, then accepted only because no input failed."""
        result = self.invoke(
            [entry(PRESENT, "FuzzPresent")], go_exit=1, go_output=BENIGN_DEADLINE
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(self.invocations()), 2, "a racing run must be repeated once")
        self.assertIn("NOTE", result.stdout)

    def test_a_deadline_that_also_recorded_a_crasher_fails(self):
        output = BENIGN_DEADLINE + "Failing input written to testdata/fuzz/FuzzPresent/deadbeef\n"
        result = self.invoke([entry(PRESENT, "FuzzPresent")], go_exit=1, go_output=output)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("GATE FAILURE", result.stderr)
        self.assertEqual(len(self.invocations()), 1, "a crasher must not be retried away")

    def test_a_deadline_from_a_seed_failure_fails(self):
        output = "failure while testing seed corpus entry: testdata/fuzz/FuzzPresent/seed\n" + BENIGN_DEADLINE
        result = self.invoke([entry(PRESENT, "FuzzPresent")], go_exit=1, go_output=output)
        self.assertNotEqual(result.returncode, 0)

    def test_a_timeout_panic_fails(self):
        output = "panic: test timed out after 3m5s\n" + BENIGN_DEADLINE
        result = self.invoke([entry(PRESENT, "FuzzPresent")], go_exit=1, go_output=output)
        self.assertNotEqual(result.returncode, 0)

    def test_a_deadline_without_any_execution_fails(self):
        output = "--- FAIL: FuzzPresent (0.01s)\n    context deadline exceeded\nFAIL\n"
        result = self.invoke([entry(PRESENT, "FuzzPresent")], go_exit=1, go_output=output)
        self.assertNotEqual(result.returncode, 0)

    def test_empty_required_set_fails(self):
        result = self.invoke([])
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.invocations(), [])

    def test_duplicate_identity_fails(self):
        result = self.invoke([entry(PRESENT, "FuzzPresent"), entry(PRESENT, "FuzzPresent")])
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.invocations(), [])

    def test_non_fuzz_name_fails(self):
        result = self.invoke([entry(PRESENT, "TestSomething")])
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.invocations(), [])

    def test_non_numeric_seconds_fails(self):
        self.assertNotEqual(self.invoke([entry(PRESENT, "FuzzPresent")], seconds="abc").returncode, 0)

    def test_zero_seconds_fails(self):
        self.assertNotEqual(self.invoke([entry(PRESENT, "FuzzPresent")], seconds="0").returncode, 0)


if __name__ == "__main__":
    unittest.main()
