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
