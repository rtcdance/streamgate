from pathlib import Path
import os
import subprocess
import sys
import tempfile
import unittest


SOURCE = Path(__file__).resolve().parent / "run-fuzz-target.sh"


class RunFuzzTargetTests(unittest.TestCase):
    """The retry is bounded and only for a deadline with no failing input."""

    def run_target(self, behaviour, target="FuzzX"):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            state = root / "state"
            state.mkdir()
            (state / "count").write_text("0")
            bin_dir = root / "bin"
            bin_dir.mkdir()
            go = bin_dir / "go"
            go.write_text("#!" + sys.executable + "\n" + r'''
from pathlib import Path
import os, sys
state = Path(os.environ["FAKE_STATE"])
n = int((state / "count").read_text()) + 1
(state / "count").write_text(str(n))
with (state / "args").open("a") as stream: stream.write(" ".join(sys.argv[1:]) + "\n")
behaviour, target = os.environ["FAKE_BEHAVIOUR"], os.environ["FAKE_TARGET"]
if behaviour == "success":
    print("PASS"); sys.exit(0)
if n > 1 and behaviour != "deadline-always": sys.exit(0)
# Shape captured from Go 1.26 coordinator shutdown in the all2 ops log.
lines = ["=== RUN   " + target,
         "fuzz: elapsed: 0s, gathering baseline coverage: 0/3 completed",
         "fuzz: elapsed: 0s, gathering baseline coverage: 3/3 completed, now fuzzing with 4 workers",
         "fuzz: elapsed: 30s, execs: 401456 (13949/sec), new interesting: 4 (total: 18)",
         "--- FAIL: " + target + " (31.00s)", "    context deadline exceeded",
         "=== NAME  ", "FAIL", "exit status 1", "FAIL\tgithub.com/rtcdance/streamgate/pkg/web3\t31.273s"]
if behaviour == "no-progress": lines = [s for s in lines if not s.startswith("fuzz:")]
if behaviour == "zero-progress": lines[3] = "fuzz: elapsed: 30s, execs: 0 (0/sec)"
if behaviour == "mixed-error": lines.insert(6, "ERROR: unrelated database handshake failed")
if behaviour == "mixed-assertion": lines.insert(6, "    parser_test.go:42: unexpected parser result")
if behaviour == "annotated-deadline": lines[5] = "    request failed: context deadline exceeded"
if behaviour == "wrong-target": lines[4] = "--- FAIL: FuzzOther (31.00s)"
if behaviour == "multiple-failures": lines.insert(6, "--- FAIL: FuzzOther (0.00s)")
if behaviour == "duplicate-deadline": lines.insert(6, "    context deadline exceeded")
if behaviour == "missing-footer": lines = lines[:6]
if behaviour == "compile-output": lines = ["fmt", "github.com/rtcdance/streamgate/pkg/web3"] + lines
if behaviour == "crash": lines.insert(6, "Failing input written to testdata/fuzz/FuzzX/abc")
if behaviour == "hung": lines.insert(6, "hung or terminated unexpectedly")
if behaviour == "seed-error": lines.insert(6, "failure while testing seed corpus entry")
if behaviour == "panic": lines.insert(6, "panic: runtime error")
if behaviour == "coordinator-error": lines.insert(6, "communicating with fuzzing coordinator")
print("\n".join(lines))
sys.exit(124 if behaviour == "non-test-exit" else 1)
''')
            go.chmod(0o755)
            env = {**os.environ, "PATH": f"{bin_dir}{os.pathsep}{os.environ['PATH']}",
                   "FAKE_STATE": str(state), "FAKE_BEHAVIOUR": behaviour, "FAKE_TARGET": target}
            result = subprocess.run(["bash", str(SOURCE), "pkg/web3", target, "30"],
                                    capture_output=True, text=True, env=env, timeout=10)
            calls = int((state / "count").read_text())
            args = (state / "args").read_text().splitlines() if (state / "args").exists() else []
            return result, calls, args

    def test_success_runs_once(self):
        result, calls, args = self.run_target("success")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(calls, 1)
        self.assertNotIn("-parallel=1", args[0])

    def test_crash_is_final_and_never_retried(self):
        result, calls, _ = self.run_target("crash")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertEqual(calls, 1)
        self.assertIn("Failing input written to", result.stdout)

    def test_deadline_without_a_failing_input_retries_once_on_one_worker(self):
        result, calls, args = self.run_target("deadline-once")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(calls, 2)
        self.assertIn("-parallel=1", args[1])
        self.assertIn("RETRY", result.stderr)

    def test_repeated_deadline_fails_closed(self):
        result, calls, _ = self.run_target("deadline-always")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, 2)
        self.assertIn("context deadline exceeded", result.stderr + result.stdout)

    def test_a_hung_worker_is_final_even_with_a_deadline(self):
        result, calls, _ = self.run_target("hung")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, 1)
        self.assertIn("hung or terminated unexpectedly", result.stdout)

    def test_a_deadline_without_progress_is_not_retried(self):
        result, calls, _ = self.run_target("no-progress")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, 1)

    def test_unknown_or_partial_failures_never_become_retry_success(self):
        for behaviour in ("mixed-error", "mixed-assertion", "annotated-deadline", "wrong-target",
                          "multiple-failures", "duplicate-deadline", "missing-footer", "zero-progress",
                          "seed-error", "panic", "coordinator-error"):
            with self.subTest(behaviour=behaviour):
                result, calls, _ = self.run_target(behaviour)
                self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                self.assertEqual(calls, 1)
                self.assertNotIn("RETRY:", result.stderr)

    def test_a_non_test_exit_is_preserved_and_never_retried(self):
        result, calls, _ = self.run_target("non-test-exit")
        self.assertEqual(result.returncode, 124, result.stdout + result.stderr)
        self.assertEqual(calls, 1)

    def test_compile_preamble_and_real_target_name_keep_the_known_retry(self):
        result, calls, args = self.run_target("compile-output", target="FuzzParseSIWEMessage")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(calls, 2)
        self.assertIn("-fuzz ^FuzzParseSIWEMessage$", args[0])

    def test_rejects_a_non_positive_budget(self):
        for value in ("0", "x", ""):
            result = subprocess.run(["bash", str(SOURCE), "pkg/web3", "FuzzX", value],
                                    capture_output=True, text=True)
            with self.subTest(value=value):
                self.assertEqual(result.returncode, 2, result.stderr)


if __name__ == "__main__":
    unittest.main()
