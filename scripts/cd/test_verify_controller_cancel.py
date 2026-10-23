import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest


class ControllerCancellationTests(unittest.TestCase):
    def test_existing_foreign_controller_name_refuses_before_mutation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "scripts").mkdir()
            source = Path(__file__).resolve().parent.parent / "verify-controller.sh"
            (root / "scripts/verify-controller.sh").write_bytes(source.read_bytes())
            # The controller runs the environment gate before it builds anything,
            # so the fake checkout must carry the gate itself.
            (root / "scripts/cd").mkdir()
            environment = Path(__file__).resolve().parent / "verify_environment.py"
            (root / "scripts/cd/verify_environment.py").write_bytes(environment.read_bytes())
            (root / "scripts/verify-docker.sh").write_text("exit 0\n")
            binary = root / "bin"
            binary.mkdir()
            fake = binary / "docker"
            fake.write_text("#!" + sys.executable + "\n" + '''
import json, os, sys
args = sys.argv[1:]
with open(os.environ["FAKE_TRACE"], "a") as log:
    log.write(json.dumps(args) + "\\n")
if args[:2] == ["ps", "-aq"] and any(arg.startswith("name=") for arg in args):
    if not any(arg.startswith("label=") for arg in args):
        print("foreign-controller-id")
elif args[:2] == ["context", "inspect"]:
    print("unix:///tmp/fake.sock")
elif args[:2] == ["image", "inspect"]:
    print("sha256:" + "1" * 64)
elif args[0] == "inspect":
    print("exited")
''')
            fake.chmod(0o755)
            trace = root / "trace.jsonl"
            env = dict(os.environ, PATH=str(binary) + os.pathsep + os.environ["PATH"],
                       FAKE_TRACE=str(trace), VERIFY_STAGE="media", VERIFY_PROJECT="own-cancel-test",
                       VERIFY_ARTIFACTS=str(root / "artifacts"))
            result = subprocess.run(["bash", str(root / "scripts/verify-controller.sh")], env=env,
                                    capture_output=True, text=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            calls = [json.loads(line) for line in trace.read_text().splitlines()]
            self.assertFalse(any(call[0] in ("run", "rm", "build") for call in calls), calls)

    def test_environment_gate_failure_releases_the_daemon_lock(self):
        # EFF-00 + EFF-04: the gate refuses before the build, and the early
        # refusal must not leave the fixed-name lock behind for the next run.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "scripts/cd").mkdir(parents=True)
            source = Path(__file__).resolve().parent.parent / "verify-controller.sh"
            (root / "scripts/verify-controller.sh").write_bytes(source.read_bytes())
            (root / "scripts/cd/verify_environment.py").write_bytes(
                (Path(__file__).resolve().parent / "verify_environment.py").read_bytes())
            (root / "scripts/verify-docker.sh").write_text("exit 0\n")
            binary = root / "bin"
            binary.mkdir()
            fake = binary / "docker"
            fake.write_text("#!" + sys.executable + "\n" + '''
import json, os, sys
args = sys.argv[1:]
with open(os.environ["FAKE_TRACE"], "a") as log:
    log.write(json.dumps(args) + "\\n")
''')
            fake.chmod(0o755)
            trace = root / "trace.jsonl"
            env = dict(os.environ, PATH=str(binary) + os.pathsep + os.environ["PATH"],
                       FAKE_TRACE=str(trace), VERIFY_STAGE="perf", VERIFY_PROJECT="own-gate-test",
                       VERIFY_ARTIFACTS=str(root / "artifacts"))
            result = subprocess.run(["bash", str(root / "scripts/verify-controller.sh")], env=env,
                                    capture_output=True, text=True, timeout=30)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("env:FAIL", result.stderr)
            calls = [json.loads(line) for line in trace.read_text().splitlines()]
            creates = [i for i, c in enumerate(calls) if c[:2] == ["network", "create"]]
            removals = [i for i, c in enumerate(calls) if c[:2] == ["network", "rm"]]
            self.assertTrue(creates, calls)
            self.assertTrue(removals, calls)
            self.assertLess(creates[0], removals[0], "the lock must be released after it was taken")
            self.assertFalse(any(c[0] == "build" for c in calls), "the gate must refuse before the build")

    def test_term_while_client_is_running_preserves_status_and_cleans(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "scripts").mkdir()
            source = Path(__file__).resolve().parent.parent / "verify-controller.sh"
            (root / "scripts/verify-controller.sh").write_bytes(source.read_bytes())
            # The controller runs the environment gate before it builds anything,
            # so the fake checkout must carry the gate itself.
            (root / "scripts/cd").mkdir()
            environment = Path(__file__).resolve().parent / "verify_environment.py"
            (root / "scripts/cd/verify_environment.py").write_bytes(environment.read_bytes())
            (root / "scripts/verify-docker.sh").write_text("exit 0\n")
            binary = root / "bin"
            binary.mkdir()
            fake = binary / "docker"
            fake.write_text("#!" + sys.executable + "\n" + '''
import json, os, signal, sys, time
from pathlib import Path
args = sys.argv[1:]
with open(os.environ["FAKE_TRACE"], "a") as log:
    log.write(json.dumps(args) + "\\n")
ready = Path(os.environ["FAKE_READY"])
if args[0] == "run":
    artifacts = Path(os.environ["VERIFY_ARTIFACTS"])
    private = artifacts / ".private" / "recovery-backup"
    private.mkdir(parents=True)
    (private / "redis.rdb").write_text("private session snapshot")
    (artifacts / "perf-loader.test").write_text("generated loader")
    (artifacts / "public-result.json").write_text("{}")
    outputs = Path(os.environ["VERIFY_SOURCE_ROOT"]) / "deploy/docker"
    outputs.mkdir(parents=True)
    owned = outputs / "monolith-linux"
    owned.write_text("owned generated application")
    (outputs / "transcoder-linux").write_text("preexisting unowned application")
    (artifacts / ".private/owned-binaries").write_text(str(owned) + "\\n")
    temporary = ready.with_suffix(".tmp")
    temporary.write_text(str(os.getpid()))
    temporary.replace(ready)
    while True:
        time.sleep(0.05)
elif args[0] == "rm":
    if ready.exists():
        try:
            os.kill(int(ready.read_text()), signal.SIGTERM)
        except ProcessLookupError:
            pass
elif args[:2] == ["image", "inspect"]:
    print("sha256:" + "1" * 64)
elif args[:2] == ["context", "inspect"]:
    print("unix:///tmp/fake-verification.sock")
''')
            fake.chmod(0o755)
            ready, trace = root / "ready", root / "trace.jsonl"
            env = dict(os.environ, PATH=str(binary) + os.pathsep + os.environ["PATH"],
                       FAKE_READY=str(ready), FAKE_TRACE=str(trace),
                       VERIFY_STAGE="media", VERIFY_PROJECT="own-cancel-test", VERIFY_IMAGE="fake:local",
                       VERIFY_ARTIFACTS=str(root / "artifacts"))
            process = subprocess.Popen(["bash", str(root / "scripts/verify-controller.sh")],
                                       env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                       text=True, start_new_session=True)
            try:
                deadline = time.monotonic() + 10
                while not ready.exists() and process.poll() is None and time.monotonic() < deadline:
                    time.sleep(0.01)
                self.assertTrue(ready.exists(), "fake controller did not reach its observable running state")
                process.send_signal(signal.SIGTERM)
                # The TERM chain forks several interpreter startups (cleanup's
                # docker probes plus the fake client kill); on a busy container
                # filesystem a single fake call alone can take hundreds of ms.
                # The bound only has to exclude a hang, not measure latency —
                # the killpg in finally is the real backstop.
                self.assertEqual(process.wait(timeout=10), 143)
                self.assertFalse((root / "artifacts/.private").exists(), "outer cancellation must remove private snapshots even when the child cannot run its trap")
                self.assertFalse((root / "artifacts/perf-loader.test").exists())
                self.assertFalse((root / "deploy/docker/monolith-linux").exists())
                self.assertEqual((root / "deploy/docker/transcoder-linux").read_text(), "preexisting unowned application")
                self.assertTrue((root / "artifacts/public-result.json").exists())
                calls = [json.loads(line) for line in trace.read_text().splitlines()]
                self.assertTrue(any(call[:2] == ["rm", "-f"] for call in calls))
                self.assertTrue(any(call[:2] == ["volume", "ls"] for call in calls))
            finally:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.communicate(timeout=3)


if __name__ == "__main__":
    unittest.main()
