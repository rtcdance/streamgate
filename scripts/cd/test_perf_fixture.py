import hashlib
import json
from pathlib import Path
import subprocess
import sys
import unittest

sys.path.insert(0, str(Path(__file__).parent))
from perf_fixture import capture

CONTENT = "11111111-1111-4111-8111-111111111111"
PREFIX = f"streams/{CONTENT}/g2/640x360/"


class FixtureTests(unittest.TestCase):
    def runner(self, objects, listing=None):
        def run(args, **kwargs):
            self.assertIn("timeout", kwargs)
            if "ls" in args:
                rows = listing if listing is not None else [{"status": "success", "key": name} for name in objects]
                return subprocess.CompletedProcess(args, 0, b"\n".join(json.dumps(x).encode() for x in rows), b"")
            if "cat" in args:
                name = args[-1].removeprefix("local/streamgate/" + PREFIX)
                if name not in objects:
                    raise subprocess.CalledProcessError(1, args)
                return subprocess.CompletedProcess(args, 0, objects[name], b"")
            return subprocess.CompletedProcess(args, 0, b"", b"")
        return run

    def objects(self):
        return {"index.m3u8": b"#EXTM3U\n#EXTINF:2,\nsegment_000.ts\n", "segment_000.ts": b"trusted fixture"}

    def test_hashes_storage_bytes_and_protected_paths(self):
        hashes, evidence = capture("minio", CONTENT, 2, "640x360", self.runner(self.objects()))
        self.assertEqual(hashes, {f"/api/v1/streaming/{CONTENT}/segment/segment_000.ts": hashlib.sha256(b"trusted fixture").hexdigest()})
        self.assertEqual(evidence[0]["objectKey"], PREFIX + "segment_000.ts")
        self.assertEqual(evidence[0]["bytes"], 15)

    def test_full_prefix_keys(self):
        hashes, _ = capture("minio", CONTENT, 2, "640x360", self.runner(self.objects(), [{"key": PREFIX + k} for k in self.objects()]))
        self.assertEqual(len(hashes), 1)

    def test_missing_segment_fails(self):
        objects = self.objects(); del objects["segment_000.ts"]
        with self.assertRaises(ValueError): capture("minio", CONTENT, 2, "640x360", self.runner(objects))

    def test_empty_listing_fails(self):
        with self.assertRaises(ValueError): capture("minio", CONTENT, 2, "640x360", self.runner({}, []))

    def test_bad_identity_fails_before_docker(self):
        for content, generation, quality in [("../other", 2, "640x360"), (CONTENT, 0, "640x360"), (CONTENT, 2, "../other")]:
            with self.assertRaises(ValueError): capture("minio", content, generation, quality, lambda *a, **k: self.fail("must not invoke Docker"))

    def test_unsafe_playlist_fails(self):
        for uri in ["../segment.ts", "https://example.invalid/segment.ts", "segment.ts?playback_token=secret"]:
            objects = self.objects(); objects["index.m3u8"] = f"#EXTM3U\n#EXTINF:2,\n{uri}\n".encode()
            with self.assertRaises(ValueError): capture("minio", CONTENT, 2, "640x360", self.runner(objects))

    def test_listing_error_fails(self):
        with self.assertRaises(ValueError): capture("minio", CONTENT, 2, "640x360", self.runner({}, [{"status": "error"}]))


if __name__ == "__main__": unittest.main()
