from pathlib import Path
import os
import subprocess
import tempfile
import unittest


SOURCE = Path(__file__).resolve().parent / "run-fuzz-target.sh"


class RunFuzzTargetTests(unittest.TestCase):
    """The retry is bounded and only for a deadline with no failing input."""

    def run_target(self, behaviour):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            state = root / "state"
            state.mkdir()
            (state / "count").write_text("0")
            bin_dir = root / "bin"
            bin_dir.mkdir()
            go = bin_dir / "go"
            go.write_text("\n".join([
                "#!/usr/bin/env bash",
                'state="$FAKE_STATE"',
                'n=$(( $(cat "$state/count" 2>/dev/null || echo 0) + 1 ))',
                'printf "%s\\n" "$n" > "$state/count"',
                'printf "%s\\n" "$*" >> "$state/args"',
                f'behaviour="{behaviour}"',
                'if [ "$behaviour" = crash ]; then',
                '  echo "--- FAIL: FuzzX (1.00s)"; echo "Failing input written to testdata/fuzz/FuzzX/abc"; exit 1',
                'fi',
                'if [ "$behaviour" = deadline-once ] && [ "$n" -gt 1 ]; then exit 0; fi',
                'if [ "$behaviour" = deadline-once ] || [ "$behaviour" = deadline-always ]; then',
                '  echo "--- FAIL: FuzzX (31.00s)"; echo "    context deadline exceeded"; exit 1',
                'fi',
                'echo "--- PASS: FuzzX (30.06s)"; exit 0',
            ]))
            go.chmod(0o755)
            env = {**os.environ, "PATH": f"{bin_dir}{os.pathsep}{os.environ['PATH']}", "FAKE_STATE": str(state)}
            result = subprocess.run(["bash", str(SOURCE), "pkg/web3", "FuzzX", "30"],
                                    capture_output=True, text=True, env=env)
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

    def test_rejects_a_non_positive_budget(self):
        for value in ("0", "x", ""):
            result = subprocess.run(["bash", str(SOURCE), "pkg/web3", "FuzzX", value],
                                    capture_output=True, text=True)
            with self.subTest(value=value):
                self.assertEqual(result.returncode, 2, result.stderr)


if __name__ == "__main__":
    unittest.main()
