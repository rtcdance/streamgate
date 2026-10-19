from pathlib import Path
import re
import shlex
import signal
import json
import subprocess
import tempfile
import unittest


SOURCE = Path(__file__).resolve().parent.parent / "backup-restore-drill.sh"


def function(name):
    match = re.search(r"^" + name + r"\(\) \{[^\n]*\}$|^" + name + r"\(\) \{\n.*?^\}", SOURCE.read_text(), re.M | re.S)
    if match is None: raise AssertionError(f"missing recovery function: {name}")
    return match[0]


class RecoveryDrillTests(unittest.TestCase):
    def test_private_backup_uses_controller_root_and_standalone_fallback(self):
        for controller in (True, False):
            with tempfile.TemporaryDirectory() as directory, self.subTest(controller=controller):
                root = Path(directory)
                artifacts = root / "recovery"
                commands = ["set -euo pipefail", "ARTIFACTS=" + shlex.quote(str(artifacts)),
                            "unset VERIFY_ARTIFACTS"]
                if controller:
                    commands.append("VERIFY_ARTIFACTS=" + shlex.quote(str(root)))
                commands += [function("prepare_private_backup_directory"),
                             "prepare_private_backup_directory", 'printf "%s" "$PRIVATE_DIR"']
                result = subprocess.run(["bash", "-c", "\n".join(commands)], capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                private = Path(result.stdout)
                self.assertEqual(private.parent, (root if controller else artifacts) / ".private")
                self.assertEqual(private.stat().st_mode & 0o777, 0o700)
                self.assertEqual(private.parent.stat().st_mode & 0o777, 0o700)
                if controller:
                    self.assertFalse((artifacts / ".private").exists())

    def test_cast_json_fields_support_the_pinned_tools_envelope(self):
        for document in ({"hash": "0x123"}, {"schema_version": 1, "success": True, "data": {"hash": "0x123"}, "errors": [], "warnings": []}):
            body = '\n'.join(["set -euo pipefail", function("json_get"),
                               "json_get " + shlex.quote(json.dumps(document)) + " hash"])
            with self.subTest(document=document):
                result = subprocess.run(["bash", "-c", body], capture_output=True, text=True)
                self.assertEqual(result.returncode, 0)
                self.assertEqual(result.stdout.strip(), "0x123")

    def test_failed_or_unknown_cast_envelopes_do_not_become_empty_success(self):
        for changes in ({"success": False}, {"schema_version": 2}, {"errors": ["failure"]}, {"data": []}):
            document = {"schema_version": 1, "success": True, "data": {"hash": "0x123"}, "errors": []}
            document.update(changes)
            body = '\n'.join(["set -euo pipefail", function("json_get"),
                               "json_get " + shlex.quote(json.dumps(document)) + " hash"])
            with self.subTest(changes=changes):
                self.assertNotEqual(subprocess.run(["bash", "-c", body], capture_output=True).returncode, 0)

    def test_real_term_cleans_private_backups_and_keeps_143(self):
        with tempfile.TemporaryDirectory() as directory:
            private = Path(directory) / "private"; private.mkdir()
            (private / "redis.rdb").write_text("secret")
            script = '\n'.join(["set -euo pipefail", "PRIVATE_DIR=" + shlex.quote(str(private)),
                                function("cleanup_private"), "trap cleanup_private EXIT",
                                "trap 'exit 143' TERM", 'echo ready', 'while :; do sleep .1; done'])
            process = subprocess.Popen(["bash", "-c", script], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            try:
                self.assertEqual(process.stdout.readline().strip(), "ready")
                process.send_signal(signal.SIGTERM)
                process.communicate(timeout=5)
                self.assertEqual(process.returncode, 143)
                self.assertFalse(private.exists())
            finally:
                if process.poll() is None: process.kill(); process.communicate()

    def test_private_backups_removed_on_success_failure_and_cancel(self):
        for status in (0, 37, 130, 143):
            with tempfile.TemporaryDirectory() as directory, self.subTest(status=status):
                private = Path(directory) / "private"; private.mkdir()
                (private / "pg-dump.sql").write_text("secret")
                public = Path(directory) / "hashes.json"; public.write_text("public")
                script = '\n'.join(["set -euo pipefail", "PRIVATE_DIR=" + shlex.quote(str(private)),
                                    function("cleanup_private"), "trap cleanup_private EXIT", f"exit {status}"])
                result = subprocess.run(["bash", "-c", script], capture_output=True)
                self.assertEqual(result.returncode, status)
                self.assertFalse(private.exists())
                self.assertTrue(public.exists())

    def test_drain_precedes_stopping_the_publisher_and_worker(self):
        body = '\n'.join(["set -euo pipefail", 'CHAIN_ID=31337',
                         'TRACE_FILE=$(mktemp); trap \'rm -f "$TRACE_FILE"\' EXIT',
                         'die() { echo fatal; exit 1; }', 'p() { :; }',
                         'src_compose() { echo "compose $*" >> "$TRACE_FILE"; }',
                         'wait_rows() { echo "drain $2" >> "$TRACE_FILE"; }',
                         function("quiesce_source"), 'quiesce_source; cat "$TRACE_FILE"'])
        result = subprocess.run(["bash", "-c", body], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        lines = result.stdout.splitlines()
        self.assertEqual(lines[0], "compose stop api-gateway")
        self.assertIn("transcoding_tasks", lines[1])
        self.assertIn("outbox", lines[2])
        self.assertIn("content_registry_intents", lines[3])
        self.assertEqual(lines[4], "compose stop monolith transcoder")

    def test_failed_drain_never_continues_to_snapshot(self):
        body = '\n'.join(["set -euo pipefail", 'CHAIN_ID=31337',
                         'die() { exit 23; }', 'p() { :; }', 'src_compose() { :; }',
                         'wait_rows() { return 1; }', function("quiesce_source"),
                         'quiesce_source', 'echo snapshot'])
        result = subprocess.run(["bash", "-c", body], capture_output=True, text=True)
        self.assertEqual(result.returncode, 23)
        self.assertNotIn("snapshot", result.stdout)

    def test_sql_failure_is_not_a_successful_empty_result(self):
        for name in ("src_psql", "dst_psql"):
            body = '\n'.join(["set -euo pipefail", 'SRC_PG=src; DST_PG=dst',
                             'timeout() { shift; "$@"; }', 'docker() { return 31; }',
                             function(name), name + " 'SELECT 1'"])
            with self.subTest(name=name):
                self.assertEqual(subprocess.run(["bash", "-c", body], capture_output=True).returncode, 31)


if __name__ == "__main__": unittest.main()
