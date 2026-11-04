import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("evidence_transport", Path(__file__).with_name("evidence_transport.py"))
transport = importlib.util.module_from_spec(spec)
spec.loader.exec_module(transport)


class EvidenceTransportTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)

    def make_tree(self, files):
        for name, content in files.items():
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
        return self.root

    def test_collect_single_directory_uses_contents(self):
        self.make_tree({"fuzz-logs/a.log": b"a", "fuzz-logs/sub/b.log": b"b"})
        _, entries = transport.collect_sources([str(self.root / "fuzz-logs")])
        self.assertEqual([entry["path"] for entry in entries], ["a.log", "sub/b.log"])

    def test_collect_multiple_paths_preserve_relative_layout(self):
        self.make_tree({"candidate.tar": b"tar", "meta/monolith.json": b"{}"})
        _, entries = transport.collect_sources([
            str(self.root / "candidate.tar"), str(self.root / "meta" / "*.json"),
        ])
        self.assertEqual([entry["path"] for entry in entries], ["candidate.tar", "meta/monolith.json"])

    def test_collect_single_file(self):
        self.make_tree({"coverage.out": b"mode: set"})
        _, entries = transport.collect_sources([str(self.root / "coverage.out")])
        self.assertEqual([entry["path"] for entry in entries], ["coverage.out"])

    def test_collect_rejects_symlink(self):
        self.make_tree({"a.txt": b"a"})
        os.symlink(self.root / "a.txt", self.root / "link.txt")
        with self.assertRaises(transport.TransportError):
            transport.collect_sources([str(self.root)])

    def test_validate_entry_path_rejects_escape(self):
        for value in ("../x", "/x", "a/../b", "a//b", "a\\b", "", "."):
            with self.assertRaises(transport.TransportError, msg=value):
                transport.validate_entry_path(value)

    def test_bundle_hash_is_order_independent(self):
        entries = [
            {"path": "a", "mode": 420, "size": 1, "sha256": "1" * 64},
            {"path": "b", "mode": 420, "size": 2, "sha256": "2" * 64},
        ]
        self.assertEqual(transport.bundle_hash(entries), transport.bundle_hash(list(reversed(entries))))

    def manifest(self):
        entries = [
            {"path": "candidate.tar", "mode": 420, "size": 3, "sha256": transport.hashlib.sha256(b"tar").hexdigest()},
        ]
        return {
            "schemaVersion": 1, "name": "candidate-monolith", "sourceSHA": "a" * 40,
            "runID": "40120832170", "runAttempt": "1", "entries": entries,
            "bundleSHA256": transport.bundle_hash(entries),
        }

    def test_registry_header_timeout_retries_completed_daemon_error_with_existing_bounds(self):
        message = 'docker push failed: failed commit on ref layer-sha256: net/http: timeout awaiting response headers'
        with patch.object(transport, 'run_docker', side_effect=[transport.TransportError(message), 'ok']) as command, \
                patch.object(transport.time, 'sleep') as sleep:
            self.assertEqual(transport.run_registry('push', 'ghcr.io/x/y:t'), 'ok')
        self.assertEqual(command.call_count, 2)
        self.assertTrue(all(call.args == ('push', 'ghcr.io/x/y:t') for call in command.call_args_list))
        self.assertEqual([call.args[0] for call in sleep.call_args_list], [5])
        with patch.object(transport, 'run_docker', side_effect=transport.TransportError(message)) as command, \
                patch.object(transport.time, 'sleep') as sleep:
            with self.assertRaises(transport.TransportError): transport.run_registry('push', 'ghcr.io/x/y:t')
        self.assertEqual(command.call_count, 3)
        self.assertEqual([call.args[0] for call in sleep.call_args_list], [5, 10])
        for permanent in ('unauthorized', 'x509', 'digest mismatch'):
            with self.subTest(permanent=permanent), patch.object(transport, 'run_docker', side_effect=transport.TransportError(permanent + ': ' + message)) as command, \
                    patch.object(transport.time, 'sleep') as sleep:
                with self.assertRaises(transport.TransportError): transport.run_registry('push', 'ghcr.io/x/y:t')
            self.assertEqual(command.call_count, 1)
            sleep.assert_not_called()

    def test_registry_push_retries_a_dropped_request(self):
        calls = []

        def fake_docker(*args, capture=True, timeout=None):
            calls.append(args)
            if args[0] == "push" and calls.count(("push", "ghcr.io/x/y:t")) == 1:
                raise transport.TransportError("docker push failed: EOF")
            if args[:2] == ("image", "inspect"):
                return json.dumps(["ghcr.io/x/y@sha256:" + "a" * 64])
            return ""

        with patch.object(transport, "run_docker", side_effect=fake_docker), \
                patch.object(transport.time, "sleep", return_value=None):
            _, digest = transport.push_image(
                self.root, "ghcr.io/x/y", "t", "https://example.invalid", "a" * 40, "n", "b" * 64)
        self.assertEqual([call[0] for call in calls].count("push"), 2)
        self.assertEqual(digest, "sha256:" + "a" * 64)

    def test_registry_push_surfaces_a_persistent_failure(self):
        with patch.object(transport, "run_docker",
                          side_effect=transport.TransportError("docker push failed: EOF")), \
                patch.object(transport.time, "sleep", return_value=None):
            with self.assertRaises(transport.TransportError):
                transport.push_image(
                    self.root, "ghcr.io/x/y", "t", "https://example.invalid", "a" * 40, "n", "b" * 64)

    def test_registry_operation_has_a_finite_timeout(self):
        with patch.dict(os.environ, {"STREAMGATE_EVIDENCE_REGISTRY_TIMEOUT_SECONDS": "17"}), \
                patch.object(transport, "run_docker", return_value="ok") as command:
            self.assertEqual(transport.run_registry("push", "ghcr.io/x/y:t"), "ok")
        command.assert_called_once_with("push", "ghcr.io/x/y:t", timeout=17)

    def test_registry_rejects_invalid_timeout_before_starting(self):
        for value in ("0", "-1", "forever", "1.5"):
            with self.subTest(value=value), \
                    patch.dict(os.environ, {"STREAMGATE_EVIDENCE_REGISTRY_TIMEOUT_SECONDS": value}), \
                    patch.object(transport, "run_docker") as command:
                with self.assertRaises(transport.TransportError):
                    transport.run_registry("pull", "ghcr.io/x/y:t")
                command.assert_not_called()

    def test_registry_authentication_failure_does_not_retry(self):
        with patch.object(transport, "run_docker", side_effect=transport.TransportError(
                "docker push failed: unauthorized: authentication required")) as command, \
                patch.object(transport.time, "sleep") as sleep:
            with self.assertRaises(transport.TransportError):
                transport.run_registry("push", "ghcr.io/x/y:t")
        self.assertEqual(command.call_count, 1)
        sleep.assert_not_called()

    def test_registry_transient_failure_exhausts_three_attempts(self):
        with patch.object(transport, "run_docker", side_effect=transport.TransportError(
                "docker pull failed: connection reset by peer")) as command, \
                patch.object(transport.time, "sleep") as sleep:
            with self.assertRaises(transport.TransportError):
                transport.run_registry("pull", "ghcr.io/x/y:t")
        self.assertEqual(command.call_count, 3)
        self.assertEqual([c.args[0] for c in sleep.call_args_list], [5, 10])

    def test_registry_timeout_does_not_stack_background_transfers(self):
        with patch.object(transport.subprocess, "run", side_effect=transport.subprocess.TimeoutExpired(
                ["docker", "push", "ghcr.io/x/y:t"], 17)) as command, \
                patch.object(transport.time, "sleep") as sleep:
            with self.assertRaisesRegex(transport.TransportError, "timed out.*daemon"):
                transport.run_registry("push", "ghcr.io/x/y:t")
        self.assertEqual(command.call_count, 1)
        sleep.assert_not_called()

    def test_real_stalled_docker_client_is_terminated(self):
        self.make_tree({"docker": b"#!/bin/sh\nexec sleep 30\n"})
        (self.root / "docker").chmod(0o755)
        with patch.dict(os.environ, {"PATH": str(self.root) + os.pathsep + os.environ["PATH"]}):
            before = transport.time.monotonic()
            with self.assertRaisesRegex(transport.TransportError, "timed out"):
                transport.run_docker("push", "ghcr.io/x/y:t", timeout=0.1)
            self.assertLess(transport.time.monotonic() - before, 5)

    def test_verify_manifest_accepts_matching_bindings(self):
        manifest = self.manifest()
        transport.verify_manifest(manifest, "candidate-monolith", "a" * 40, "40120832170", "1")

    def test_verify_manifest_rejects_wrong_bindings(self):
        manifest = self.manifest()
        cases = [
            ("candidate-other", "a" * 40, "40120832170", "1"),
            ("candidate-monolith", "b" * 40, "40120832170", "1"),
            ("candidate-monolith", "a" * 40, "40120832171", "1"),
            ("candidate-monolith", "a" * 40, "40120832170", "2"),
        ]
        for case in cases:
            with self.assertRaises(transport.TransportError, msg=str(case)):
                transport.verify_manifest(manifest, *case)

    def test_verify_manifest_rejects_tampered_bundle(self):
        manifest = self.manifest()
        manifest["entries"][0]["size"] = 4
        with self.assertRaises(transport.TransportError):
            transport.verify_manifest(manifest, None, None, None, None)

    def materialize(self, entries):
        target = Path(tempfile.mkdtemp(dir=self.directory.name))
        for entry in entries:
            path = target / entry["path"]
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b"tar")
        return target

    def test_verify_tree_accepts_exact_bytes(self):
        manifest = self.manifest()
        transport.verify_tree(self.materialize(manifest["entries"]), manifest["entries"])

    def test_verify_tree_rejects_tampered_byte(self):
        manifest = self.manifest()
        target = self.materialize(manifest["entries"])
        (target / "candidate.tar").write_bytes(b"TAR")
        with self.assertRaises(transport.TransportError):
            transport.verify_tree(target, manifest["entries"])

    def test_verify_tree_rejects_extra_file(self):
        manifest = self.manifest()
        target = self.materialize(manifest["entries"])
        (target / "extra.txt").write_bytes(b"x")
        with self.assertRaises(transport.TransportError):
            transport.verify_tree(target, manifest["entries"])

    def test_verify_tree_rejects_missing_file(self):
        manifest = self.manifest()
        target = self.materialize(manifest["entries"])
        (target / "candidate.tar").unlink()
        with self.assertRaises(transport.TransportError):
            transport.verify_tree(target, manifest["entries"])

    def test_verify_tree_rejects_symlink(self):
        manifest = self.manifest()
        target = self.materialize(manifest["entries"])
        real = target / "real"
        real.write_bytes(b"tar")
        (target / "candidate.tar").unlink()
        os.symlink(real, target / "candidate.tar")
        with self.assertRaises(transport.TransportError):
            transport.verify_tree(target, manifest["entries"])

    def test_prepare_only_writes_context_and_manifest(self):
        self.make_tree({"candidate.tar": b"tar"})
        context = self.root / "context"
        argv = [
            "put", "--name", "candidate-monolith", "--path", str(self.root / "candidate.tar"),
            "--image", "ghcr.io/rtcdance/streamgate/evidence-candidate-monolith",
            "--tag", "r40120832170-a1", "--source-sha", "a" * 40,
            "--run-id", "40120832170", "--attempt", "1", "--context", str(context),
            "--prepare-only",
        ]
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            self.assertEqual(transport.main(argv), 0)
        result = json.loads(output.getvalue())
        self.assertEqual(result["status"], "prepared")
        manifest = json.loads((context / "transport.json").read_text())
        self.assertEqual(manifest["bundleSHA256"], result["bundleSHA256"])
        self.assertEqual((context / "payload" / "candidate.tar").read_bytes(), b"tar")
        self.assertIn("COPY payload/ /payload/", (context / "Dockerfile").read_text())

    def test_put_skips_missing_files_when_ignored(self):
        argv = [
            "put", "--name", "empty", "--path", str(self.root / "absent-*"),
            "--image", "ghcr.io/rtcdance/streamgate/evidence-empty", "--tag", "r1-a1",
            "--source-sha", "a" * 40, "--run-id", "1", "--attempt", "1",
            "--if-no-files-found", "ignore",
        ]
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            self.assertEqual(transport.main(argv), 0)
        self.assertEqual(json.loads(output.getvalue())["status"], "skipped")

    def test_put_errors_on_missing_files(self):
        argv = [
            "put", "--name", "empty", "--path", str(self.root / "absent-*"),
            "--image", "ghcr.io/rtcdance/streamgate/evidence-empty", "--tag", "r1-a1",
            "--source-sha", "a" * 40, "--run-id", "1", "--attempt", "1",
        ]
        with contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(transport.main(argv), 2)

    def test_put_rejects_unsafe_name(self):
        self.make_tree({"a.txt": b"a"})
        argv = [
            "put", "--name", "../escape", "--path", str(self.root / "a.txt"),
            "--image", "ghcr.io/rtcdance/streamgate/evidence", "--tag", "r1-a1",
            "--source-sha", "a" * 40, "--run-id", "1", "--attempt", "1", "--prepare-only",
        ]
        with contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(transport.main(argv), 2)

    def test_collect_sources_excludes_unselected_siblings(self):
        inputs = self.root / "inputs"
        inputs.mkdir()
        (inputs / "candidate.tar").write_bytes(b"tar")
        (inputs / ".private").mkdir()
        (inputs / ".private" / "secret.txt").write_bytes(b"secret")
        (inputs / "unrelated.txt").write_bytes(b"x")
        _, entries = transport.collect_sources([str(inputs / "candidate.tar")])
        self.assertEqual([entry["path"] for entry in entries], ["candidate.tar"])

    def test_put_prepare_only_excludes_unselected_siblings(self):
        inputs = self.root / "inputs"
        inputs.mkdir()
        (inputs / "candidate.tar").write_bytes(b"tar")
        (inputs / ".private").mkdir()
        (inputs / ".private" / "secret.txt").write_bytes(b"secret")
        context = self.root / "context"
        argv = [
            "put", "--name", "candidate-monolith", "--path", str(inputs / "candidate.tar"),
            "--image", "ghcr.io/example/evidence", "--tag", "probe",
            "--source-sha", "a" * 40, "--run-id", "1", "--attempt", "1",
            "--prepare-only", "--context", str(context),
        ]
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(transport.main(argv), 0)
        manifest = json.loads((context / "transport.json").read_text())
        self.assertEqual([entry["path"] for entry in manifest["entries"]], ["candidate.tar"])

    def test_copy_payload_rejects_destination_ancestor_symlink(self):
        source = self.root / "payload"
        (source / "meta").mkdir(parents=True)
        (source / "meta" / "receipt.bin").write_bytes(b"receipt")
        base = self.root / "base"
        destination = base / "dest"
        destination.mkdir(parents=True)
        outside = base / "outside"
        outside.mkdir()
        os.symlink(outside, destination / "meta")
        with self.assertRaises(transport.TransportError):
            transport.copy_payload(source, destination)
        self.assertFalse((outside / "receipt.bin").exists())

    def test_copy_payload_rejects_symlinked_destination_root(self):
        source = self.root / "payload"
        source.mkdir()
        (source / "a.txt").write_bytes(b"a")
        outside = self.root / "outside"
        outside.mkdir()
        destination = self.root / "dest-link"
        os.symlink(outside, destination)
        with self.assertRaises(transport.TransportError):
            transport.copy_payload(source, destination)
        self.assertFalse((outside / "a.txt").exists())

    def test_copy_payload_rejects_dangling_leaf_symlink(self):
        source = self.root / "payload"
        source.mkdir()
        (source / "a.txt").write_bytes(b"a")
        destination = self.root / "dest"
        destination.mkdir()
        os.symlink(self.root / "missing", destination / "a.txt")
        with self.assertRaises(transport.TransportError):
            transport.copy_payload(source, destination)
        self.assertFalse((self.root / "missing").exists())

    def test_verify_manifest_rejects_unsafe_name(self):
        manifest = self.manifest()
        for name in ("../escaped", "/absolute", ".", "..", "a/b"):
            manifest["name"] = name
            with self.assertRaises(transport.TransportError, msg=name):
                transport.verify_manifest(manifest, None, None, None, None)

    def test_get_multi_image_rejects_unsafe_manifest_name(self):
        manifest = self.manifest()
        manifest["name"] = "../escaped"
        manifest["bundleSHA256"] = transport.bundle_hash(manifest["entries"])
        payload = self.materialize(manifest["entries"])
        work = Path(tempfile.mkdtemp(dir=self.directory.name))

        def fake_extract(reference, local):
            return manifest, payload, work

        destination = self.root / "dest"
        argv = [
            "get", "--image", "a@sha256:" + "1" * 64, "--image", "b@sha256:" + "2" * 64,
            "--dest", str(destination), "--expect-sha", "a" * 40,
            "--expect-run", "40120832170", "--expect-attempt", "1",
        ]
        with patch.object(transport, "extract", side_effect=fake_extract), \
                patch.object(transport, "resolve_digest", return_value="sha256:" + "1" * 64), \
                contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(transport.main(argv), 2)
        self.assertFalse((self.root / "escaped").exists())

    def test_get_multi_image_rejects_symlinked_destination_root(self):
        outside = self.root / "outside"
        outside.mkdir()
        destination = self.root / "dest-link"
        os.symlink(outside, destination)
        payload = self.root / "payload"
        payload.mkdir()
        (payload / "data.bin").write_bytes(b"x")
        entries = [{"path": "data.bin", "mode": 420, "size": 1, "sha256": transport.hashlib.sha256(b"x").hexdigest()}]
        manifests = []
        for name in ("artifact-a", "artifact-b"):
            manifests.append({
                "schemaVersion": 1, "name": name, "sourceSHA": "a" * 40,
                "runID": "40120832170", "runAttempt": "1", "entries": entries,
                "bundleSHA256": transport.bundle_hash(entries),
            })
        work = Path(tempfile.mkdtemp(dir=self.directory.name))

        def fake_extract(reference, local):
            return manifests.pop(0), payload, work

        argv = [
            "get", "--image", "a@sha256:" + "1" * 64, "--image", "b@sha256:" + "2" * 64,
            "--dest", str(destination), "--expect-sha", "a" * 40,
            "--expect-run", "40120832170", "--expect-attempt", "1",
        ]
        with patch.object(transport, "extract", side_effect=fake_extract), \
                patch.object(transport, "resolve_digest", return_value="sha256:" + "1" * 64), \
                contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(transport.main(argv), 2)
        self.assertFalse((outside / "artifact-a" / "data.bin").exists())
        self.assertFalse((outside / "artifact-b" / "data.bin").exists())

    def test_collect_selection_applies_exclusions_before_hashing(self):
        self.make_tree({
            "ci-all/build-inputs/candidate-monolith.tar": b"tar",
            "ci-all/run-1/evidence.json": b"{}",
        })
        hashed = []
        real_hash = transport.file_hash

        def tracking_hash(path):
            hashed.append(Path(path).name)
            return real_hash(path)

        with patch.object(transport, "file_hash", side_effect=tracking_hash):
            _, entries, report = transport.collect_selection(
                [str(self.root / "ci-all")], excludes=["build-inputs/**"]
            )
        self.assertEqual([entry["path"] for entry in entries], ["run-1/evidence.json"])
        self.assertEqual(report["excluded"], 1)
        self.assertNotIn("candidate-monolith.tar", hashed)

    def test_collect_selection_excludes_hidden_by_default(self):
        self.make_tree({"ci-all/visible.json": b"{}", "ci-all/.private/secret.txt": b"s"})
        _, entries, report = transport.collect_selection([str(self.root / "ci-all")])
        self.assertEqual([entry["path"] for entry in entries], ["visible.json"])
        self.assertEqual(report["hiddenExcluded"], 1)
        _, entries, report = transport.collect_selection(
            [str(self.root / "ci-all")], excludes=[".private/**"], include_hidden=True
        )
        self.assertEqual([entry["path"] for entry in entries], ["visible.json"])
        self.assertEqual(report["excluded"], 1)

    def test_collect_selection_tolerates_unmatched_spec(self):
        self.make_tree({"ci-all/ci-job-results.json": b"{}"})
        _, entries, report = transport.collect_selection([
            str(self.root / "ci-all" / "ci-job-results.json"),
            str(self.root / "ci-all" / "ci-evidence.json"),
        ])
        self.assertEqual([entry["path"] for entry in entries], ["ci-job-results.json"])
        self.assertEqual([item["matches"] for item in report["specs"]], [1, 0])

    def test_ci_summary_with_receipts_preserves_the_summary_layout(self):
        self.make_tree({"ci-all/ci-job-results.json": b"{}", "ci-all/ci-evidence.json": b"{}",
                        "ci-all/publication-revalidation/monolith.json": b"receipt"})
        _, entries, _ = transport.collect_selection([
            str(self.root / "ci-all/ci-job-results.json"), str(self.root / "ci-all/ci-evidence.json"),
            str(self.root / "ci-all/publication-revalidation"),
        ])
        self.assertEqual([entry["path"] for entry in entries], [
            "ci-evidence.json", "ci-job-results.json", "publication-revalidation/monolith.json"])

    def test_put_warn_uploads_remaining_files_when_one_spec_is_missing(self):
        self.make_tree({"ci-all/ci-job-results.json": b"{}"})
        context = self.root / "context"
        argv = [
            "put", "--name", "ci-evidence", "--path", str(self.root / "ci-all" / "ci-job-results.json"),
            "--path", str(self.root / "ci-all" / "ci-evidence.json"),
            "--image", "ghcr.io/example/evidence", "--tag", "probe",
            "--source-sha", "a" * 40, "--run-id", "1", "--attempt", "1",
            "--if-no-files-found", "warn", "--prepare-only", "--context", str(context),
        ]
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(transport.main(argv), 0)
        manifest = json.loads((context / "transport.json").read_text())
        self.assertEqual([entry["path"] for entry in manifest["entries"]], ["ci-job-results.json"])

    def test_put_missing_policy_applies_to_total_selection(self):
        self.make_tree({"ci-all/build-inputs/big.tar": b"tar"})
        base = [
            "put", "--name", "ci-acceptance", "--path", str(self.root / "ci-all"),
            "--exclude", "build-inputs/**", "--image", "ghcr.io/example/evidence",
            "--tag", "probe", "--source-sha", "a" * 40, "--run-id", "1", "--attempt", "1",
            "--prepare-only", "--context", str(self.root / "context"),
        ]
        with contextlib.redirect_stderr(io.StringIO()), contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(transport.main(base), 2)
        with contextlib.redirect_stderr(io.StringIO()), contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(transport.main(base + ["--if-no-files-found", "warn"]), 0)

    def test_put_writes_github_output(self):
        self.make_tree({"coverage.out": b"mode: set"})
        output_file = self.root / "github-output"
        argv = [
            "put", "--name", "coverage-unit", "--path", str(self.root / "coverage.out"),
            "--image", "ghcr.io/example/evidence", "--tag", "r1-a1",
            "--source-sha", "a" * 40, "--run-id", "1", "--attempt", "1",
            "--prepare-only", "--context", str(self.root / "context"),
            "--github-output", str(output_file),
        ]
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(transport.main(argv), 0)
        values = dict(line.split("=", 1) for line in output_file.read_text().splitlines())
        self.assertEqual(values["status"], "prepared")
        self.assertEqual(values["files"], "1")

    def staged_images(self, names):
        payloads = []
        for name in names:
            payload = Path(tempfile.mkdtemp(dir=self.directory.name))
            (payload / f"{name}.bin").write_bytes(name.encode())
            entries = [{
                "path": f"{name}.bin", "mode": 420, "size": len(name),
                "sha256": transport.hashlib.sha256(name.encode()).hexdigest(),
            }]
            manifest = {
                "schemaVersion": 1, "name": name, "sourceSHA": "a" * 40,
                "runID": "40120832170", "runAttempt": "1", "entries": entries,
                "bundleSHA256": transport.bundle_hash(entries),
            }
            payloads.append((manifest, payload, Path(tempfile.mkdtemp(dir=self.directory.name))))
        return payloads

    def test_get_rejects_artifact_outside_declared_set(self):
        staged = self.staged_images(["candidate-monolith", "candidate-manifest-" + "a" * 40])

        def fake_extract(reference, local):
            return staged.pop(0)

        argv = [
            "get", "--image", "r@sha256:" + "1" * 64, "--image", "r@sha256:" + "2" * 64,
            "--dest", str(self.root / "dest"), "--merge", "--require-digest-refs",
            "--expect-sha", "a" * 40, "--expect-run", "40120832170",
            "--expect-names", "candidate-monolith",
        ]
        with patch.object(transport, "extract", side_effect=fake_extract), \
                patch.object(transport, "resolve_digest", return_value="sha256:" + "1" * 64), \
                contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(transport.main(argv), 2)
        self.assertFalse((self.root / "dest" / "candidate-monolith.bin").exists())

    def test_get_allows_declared_extra_and_bounds_attempt(self):
        staged = self.staged_images(["candidate-monolith", "candidate-manifest-" + "a" * 40])

        def fake_extract(reference, local):
            return staged.pop(0)

        argv = [
            "get", "--image", "r@sha256:" + "1" * 64, "--image", "r@sha256:" + "2" * 64,
            "--dest", str(self.root / "dest"), "--merge", "--require-digest-refs",
            "--expect-sha", "a" * 40, "--expect-run", "40120832170",
            "--expect-names", "candidate-monolith",
            "--allow-extra-names", "candidate-manifest-" + "a" * 40,
            "--max-attempt", "2",
        ]
        with patch.object(transport, "extract", side_effect=fake_extract), \
                patch.object(transport, "resolve_digest", return_value="sha256:" + "1" * 64), \
                contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(transport.main(argv), 0)
        self.assertTrue((self.root / "dest" / "candidate-monolith.bin").is_file())
        self.assertTrue((self.root / "dest" / ("candidate-manifest-" + "a" * 40 + ".bin")).is_file())

    def test_get_rejects_attempt_above_bound(self):
        staged = self.staged_images(["candidate-monolith"])

        def fake_extract(reference, local):
            return staged.pop(0)

        argv = [
            "get", "--image", "r@sha256:" + "1" * 64, "--dest", str(self.root / "dest"),
            "--expect-sha", "a" * 40, "--expect-run", "40120832170", "--max-attempt", "0",
        ]
        with patch.object(transport, "extract", side_effect=fake_extract), \
                patch.object(transport, "resolve_digest", return_value="sha256:" + "1" * 64), \
                contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(transport.main(argv), 2)

    def test_get_requires_digest_pinned_references(self):
        argv = [
            "get", "--image", "ghcr.io/example/evidence:r1-a1", "--dest", str(self.root / "dest"),
            "--require-digest-refs",
        ]
        with contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(transport.main(argv), 2)

    def test_get_verifies_every_artifact_before_writing(self):
        staged = self.staged_images(["candidate-monolith", "candidate-transcoder"])
        staged[1][0]["entries"][0]["sha256"] = "0" * 64
        staged[1][0]["bundleSHA256"] = transport.bundle_hash(staged[1][0]["entries"])

        def fake_extract(reference, local):
            return staged.pop(0)

        argv = [
            "get", "--image", "r@sha256:" + "1" * 64, "--image", "r@sha256:" + "2" * 64,
            "--dest", str(self.root / "dest"), "--merge",
        ]
        with patch.object(transport, "extract", side_effect=fake_extract), \
                patch.object(transport, "resolve_digest", return_value="sha256:" + "1" * 64), \
                contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(transport.main(argv), 2)
        self.assertFalse((self.root / "dest" / "candidate-monolith.bin").exists())

    def test_copy_payload_rejects_collision(self):
        first = Path(tempfile.mkdtemp(dir=self.directory.name))
        second = Path(tempfile.mkdtemp(dir=self.directory.name))
        (first / "a.txt").write_bytes(b"1")
        (second / "a.txt").write_bytes(b"2")
        destination = self.root / "merged"
        transport.copy_payload(first, destination)
        with self.assertRaises(transport.TransportError):
            transport.copy_payload(second, destination)


if __name__ == "__main__":
    unittest.main()
