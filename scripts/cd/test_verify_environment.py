from pathlib import Path
import json
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parent / "verify_environment.py"

QUALIFIED = {
    "os_type": "linux",
    "architecture": "x86_64",
    "ncpu": 16,
    "mem_total_bytes": 64 * 1024 ** 3,
    "docker_root_dir": "/var/lib/docker",
    "free_bytes": 400 * 1024 ** 3,
    "running_projects": [],
}


class VerifyEnvironmentTests(unittest.TestCase):
    def run_probe(self, probe, stage="all"):
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as handle:
            json.dump(probe, handle)
        try:
            return subprocess.run(
                [sys.executable, str(SCRIPT), "--stage", stage, "--probe", handle.name],
                capture_output=True, text=True,
            )
        finally:
            Path(handle.name).unlink()

    def test_qualified_runner_passes(self):
        result = self.run_probe(QUALIFIED)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("qualified runner", result.stdout)

    def test_targeted_stages_are_not_gated(self):
        tiny = dict(QUALIFIED, ncpu=2, mem_total_bytes=2 * 1024 ** 3,
                    free_bytes=1 * 1024 ** 3, architecture="aarch64")
        for stage in ("unit", "media", "ops", "media,recovery"):
            with self.subTest(stage=stage):
                result = self.run_probe(tiny, stage=stage)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("skipping capacity gate", result.stdout)

    def test_stage_lists_containing_a_gated_stage_are_gated(self):
        tiny = dict(QUALIFIED, ncpu=2, mem_total_bytes=2 * 1024 ** 3)
        for stage in ("media,perf", "release", "all"):
            with self.subTest(stage=stage):
                result = self.run_probe(tiny, stage=stage)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("[env:FAIL]", result.stderr)

    def test_each_shortfall_is_reported(self):
        cases = {
            "CPUs": dict(QUALIFIED, ncpu=4),
            "RAM": dict(QUALIFIED, mem_total_bytes=16 * 1024 ** 3),
            "Docker filesystem": dict(QUALIFIED, free_bytes=40 * 1024 ** 3),
            "architecture": dict(QUALIFIED, architecture="aarch64"),
            "another acceptance": dict(QUALIFIED, running_projects=["sg-all-local6"]),
        }
        for needle, probe in cases.items():
            with self.subTest(needle=needle):
                result = self.run_probe(probe)
                self.assertEqual(result.returncode, 1)
                self.assertIn(needle, result.stderr)
                self.assertIn("BLOCKED", result.stderr)

    def test_emulated_daemon_is_rejected_even_when_large(self):
        result = self.run_probe(dict(QUALIFIED, architecture="aarch64", ncpu=64,
                                     mem_total_bytes=128 * 1024 ** 3))
        self.assertEqual(result.returncode, 1)
        self.assertIn("no emulation", result.stderr)

    def test_the_same_run_is_not_counted_as_a_competing_acceptance(self):
        # collect() filters the caller's own project out; the probe contract is
        # "other projects only", so an empty list must stay qualified.
        result = self.run_probe(dict(QUALIFIED, running_projects=[]))
        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
