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
    def test_term_while_client_is_running_preserves_status_and_cleans(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "scripts").mkdir()
            source = Path(__file__).resolve().parent.parent / "verify-controller.sh"
            (root / "scripts/verify-controller.sh").write_bytes(source.read_bytes())
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
                self.assertEqual(process.wait(timeout=2), 143)
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
