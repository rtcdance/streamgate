from pathlib import Path
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parent / "verify_environment.sh"
QUALIFIED = {
    "os_type": "linux", "architecture": "x86_64", "ncpu": 16,
    "mem_total_bytes": 64 * 1024 ** 3, "free_bytes": 400 * 1024 ** 3,
    "running_projects": [], "host_os": "Linux", "operating_system": "Ubuntu",
}


class VerifyEnvironmentTests(unittest.TestCase):
    def run_probe(self, probe, stage="all", project="own-run"):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / "bin"
            binary.mkdir()
            docker_root = root / "docker-data"
            docker_root.mkdir()
            data = dict(probe)
            data.setdefault("docker_root_dir", str(docker_root))
            data.setdefault("endpoint", "unix:///tmp/docker.sock")
            tool = binary / "probe-tool"
            tool.write_text("#!" + sys.executable + "\n" + '''
import json, os, sys
from pathlib import Path
p = json.loads(os.environ["FAKE_PROBE"])
name = Path(sys.argv[0]).name
if name == "docker":
    args = sys.argv[1:]
    if args[0] == "info":
        if p.get("info_error"): sys.exit(1)
        print("|".join(str(p.get(k, "")) for k in
            ("os_type", "architecture", "ncpu", "mem_total_bytes", "docker_root_dir", "operating_system")))
    elif args[:2] == ["context", "inspect"]:
        print(p["endpoint"])
    elif args[0] == "ps":
        if p.get("ps_error"): sys.exit(1)
        for i, owner in enumerate(p.get("running_projects", [])):
            print(str(i) + "|" + owner)
    else: sys.exit(2)
elif name == "uname": print(p["host_os"])
elif name == "df":
    if p.get("df_error"): sys.exit(1)
    if sys.argv[-1] != p["docker_root_dir"]: sys.exit(2)
    print("Filesystem 1024-blocks Used Available Capacity Mounted")
    print("disk 9999999999 0 " + str(p["free_bytes"] // 1024) + " 0% /")
''')
            tool.chmod(0o755)
            for name in ("docker", "uname", "df"):
                (binary / name).symlink_to(tool)
            (binary / "awk").symlink_to(shutil.which("awk"))
            env = dict(os.environ, PATH=str(binary), FAKE_PROBE=json.dumps(data))
            env.pop("DOCKER_HOST", None)
            self.assertIsNone(shutil.which("python3", path=env["PATH"]))
            return subprocess.run(["/bin/bash", str(SCRIPT), stage, project],
                                  env=env, capture_output=True, text=True, timeout=10)

    def test_qualified_runner_passes(self):
        result = self.run_probe(QUALIFIED)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("qualified runner", result.stdout)

    def test_targeted_stages_are_not_gated(self):
        tiny = dict(QUALIFIED, ncpu=2, mem_total_bytes=2 * 1024 ** 3,
                    free_bytes=1024, architecture="aarch64", info_error=True)
        for stage in ("unit", "media", "ops", "media,recovery"):
            with self.subTest(stage=stage):
                result = self.run_probe(tiny, stage=stage)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("skipping capacity gate", result.stdout)

    def test_stage_lists_containing_a_gated_stage_are_gated(self):
        for stage in ("media,perf", "release", "all"):
            with self.subTest(stage=stage):
                result = self.run_probe(dict(QUALIFIED, ncpu=2), stage=stage)
                self.assertEqual(result.returncode, 1)
                self.assertIn("env:FAIL", result.stderr)

    def test_each_shortfall_is_reported(self):
        cases = {
            "CPUs": dict(QUALIFIED, ncpu=4),
            "RAM": dict(QUALIFIED, mem_total_bytes=16 * 1024 ** 3),
            "Docker filesystem": dict(QUALIFIED, free_bytes=40 * 1024 ** 3),
            "architecture": dict(QUALIFIED, architecture="aarch64"),
            "competing container": dict(QUALIFIED, running_projects=["other-run"]),
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
        result = self.run_probe(dict(QUALIFIED, running_projects=["own-run"]))
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_unlabelled_business_containers_are_competing_load(self):
        result = self.run_probe(dict(QUALIFIED, running_projects=[""]))
        self.assertEqual(result.returncode, 1)
        self.assertIn("unlabelled", result.stderr)

    def test_unavailable_docker_root_never_falls_back_to_host_root(self):
        result = self.run_probe(dict(QUALIFIED, docker_root_dir="/missing-streamgate-docker-root"))
        self.assertEqual(result.returncode, 1)
        self.assertIn("disk capacity is unverified", result.stderr)

    def test_desktop_and_remote_daemons_are_not_native_local_runners(self):
        for changes in ({"host_os": "Darwin"}, {"operating_system": "Docker Desktop"},
                        {"endpoint": "tcp://runner:2375"}, {"os_type": ""}):
            with self.subTest(changes=changes):
                result = self.run_probe(dict(QUALIFIED, **changes))
                self.assertEqual(result.returncode, 1, result.stdout)

    def test_missing_or_malformed_capacity_and_probe_errors_fail_closed(self):
        for changes in ({"ncpu": ""}, {"ncpu": "8x"}, {"mem_total_bytes": "bad"},
                        {"info_error": True}, {"ps_error": True}, {"df_error": True}):
            with self.subTest(changes=changes):
                result = self.run_probe(dict(QUALIFIED, **changes))
                self.assertEqual(result.returncode, 1)
                self.assertIn("env:FAIL", result.stderr)


if __name__ == "__main__":
    unittest.main()
