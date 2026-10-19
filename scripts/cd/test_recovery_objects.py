import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest

from recovery_objects import backup, restore, inventory, load_backup


class MemoryStore:
    def __init__(self, objects):
        self.data = dict(objects)
        self.uploaded = []

    def objects(self):
        return [{"key": key, "bytes": len(body)} for key, body in sorted(self.data.items())]

    def download(self, key, path):
        path.write_bytes(self.data[key])

    def prepare_empty(self):
        if self.data:
            raise ValueError("target bucket is not empty")

    def upload(self, key, path):
        self.uploaded.append(key)
        self.data[key] = path.read_bytes()


class RecoveryObjectsTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "objects"
        self.source = MemoryStore({"uploads/a.mp4": b"\x00original", "streams/id/g1/360p/segment.ts": b"media", "empty": b""})

    def test_logical_roundtrip_and_private_permissions(self):
        expected = backup(self.source, self.root)
        target = MemoryStore({})
        restore(target, self.root)
        self.assertEqual(target.data, self.source.data)
        self.assertEqual(inventory(target), expected)
        self.assertEqual(self.root.stat().st_mode & 0o777, 0o700)
        self.assertTrue(all(p.stat().st_mode & 0o777 == 0o600 for p in self.root.iterdir()))
        self.assertTrue(all(set(row) == {"key", "bytes", "sha256"} for row in expected))

    def test_empty_snapshot_is_not_proof(self):
        with self.assertRaises(ValueError): backup(MemoryStore({}), self.root)

    def test_invalid_object_paths_are_rejected(self):
        for key in ("../escape", "/absolute", "a/../b", "a//b", "a\\b", "a\nb", ".minio.sys/internal"):
            with self.subTest(key=key), self.assertRaises(ValueError):
                inventory(MemoryStore({key: b"body"}))

    def test_duplicate_inventory_is_rejected(self):
        self.source.objects = lambda: [{"key": "a", "bytes": 1}] * 2
        with self.assertRaises(ValueError): backup(self.source, self.root)

    def test_download_size_must_match_listing(self):
        self.source.objects = lambda: [{"key": "empty", "bytes": 1}]
        with self.assertRaises(ValueError): backup(self.source, self.root)

    def test_inventory_must_not_change_during_snapshot(self):
        original = self.source.download
        def change(key, path):
            original(key, path)
            self.source.data["new"] = b"write"
        self.source.download = change
        with self.assertRaises(ValueError): backup(self.source, self.root)

    def test_corrupt_missing_or_extra_backup_fails_before_any_upload(self):
        backup(self.source, self.root)
        body = self.root / "000000.bin"
        original = body.read_bytes()
        for mutation in (lambda: body.write_bytes(b"corrupt"), body.unlink,
                         lambda: (self.root / "extra.bin").write_bytes(b"unlisted")):
            with self.subTest(mutation=mutation):
                body.write_bytes(original)
                mutation()
                target = MemoryStore({})
                with self.assertRaises(ValueError): restore(target, self.root)
                self.assertEqual(target.uploaded, [])
                (self.root / "extra.bin").unlink(missing_ok=True)

    def test_symlink_backup_is_rejected(self):
        backup(self.source, self.root)
        path = self.root / "000000.bin"
        outside = Path(self.temp.name) / "elsewhere"
        outside.write_bytes(path.read_bytes())
        path.unlink(); path.symlink_to(outside)
        with self.assertRaises(ValueError): load_backup(self.root)

    def test_manifest_cannot_traverse_or_duplicate_files(self):
        backup(self.source, self.root)
        path = self.root / "manifest.json"
        original = json.loads(path.read_text())
        for value in ("../outside", "/outside", "000001.bin"):
            changed = copy.deepcopy(original); changed["objects"][0]["file"] = value
            path.write_text(json.dumps(changed))
            with self.subTest(value=value), self.assertRaises(ValueError): load_backup(self.root)

    def test_restore_requires_an_empty_logical_bucket(self):
        backup(self.source, self.root)
        with self.assertRaises(ValueError): restore(MemoryStore({"existing": b"data"}), self.root)

    def test_restored_bytes_are_independently_rehashed(self):
        expected = backup(self.source, self.root)
        target = MemoryStore({})
        restore(target, self.root)
        target.data["uploads/a.mp4"] = b"tampered"
        self.assertNotEqual(inventory(target), expected)
        row = next(row for row in expected if row["key"] == "uploads/a.mp4")
        self.assertEqual(row["sha256"], hashlib.sha256(self.source.data[row["key"]]).hexdigest())


if __name__ == "__main__":
    unittest.main()
