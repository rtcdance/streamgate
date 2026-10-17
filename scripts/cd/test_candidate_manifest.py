import copy
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("candidate", Path(__file__).with_name("candidate_manifest.py"))
candidate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(candidate)


class CandidateManifestTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.archive = Path(self.directory.name) / "images.tar"
        self.archive.write_bytes(b"candidate archive")
        self.info = {
            "Id": "sha256:image", "Os": "linux", "Architecture": "amd64",
            "Config": {"Labels": {"org.opencontainers.image.revision": "revision", "com.streamgate.build.tags": "nats_integration"}},
        }
        self.document = {
            "archive": str(self.archive), "archiveSHA256": candidate.file_hash(self.archive), "revision": "revision",
            "images": {"monolith": {"tag": "candidate-monolith:latest", "imageID": "sha256:image", "buildTags": ["nats_integration"], "platform": "linux/amd64"}},
        }
        self.document["images"]["api-gateway"] = copy.deepcopy(self.document["images"]["monolith"])
        self.document["images"]["transcoder"] = copy.deepcopy(self.document["images"]["monolith"])

    def test_exact_identity_passes(self):
        with patch.object(candidate, "docker_json", return_value=[self.info]):
            candidate.verify(self.document)

    def test_modified_archive_fails(self):
        self.archive.write_bytes(b"changed archive")
        with self.assertRaisesRegex(ValueError, "archive"):
            candidate.verify(self.document)

    def test_replaced_tag_fails(self):
        info = copy.deepcopy(self.info)
        info["Id"] = "sha256:replacement"
        with patch.object(candidate, "docker_json", return_value=[info]):
            with self.assertRaisesRegex(ValueError, "replaced"):
                candidate.verify(self.document)

    def test_wrong_revision_fails(self):
        info = copy.deepcopy(self.info)
        info["Config"]["Labels"]["org.opencontainers.image.revision"] = "wrong"
        with patch.object(candidate, "docker_json", return_value=[info]):
            with self.assertRaisesRegex(ValueError, "revision"):
                candidate.verify(self.document)

    def test_wrong_build_tags_fail(self):
        info = copy.deepcopy(self.info)
        info["Config"]["Labels"]["com.streamgate.build.tags"] = ""
        with patch.object(candidate, "docker_json", return_value=[info]):
            with self.assertRaisesRegex(ValueError, "tags"):
                candidate.verify(self.document)

    def test_wrong_platform_fails(self):
        info = dict(self.info, Architecture="arm64")
        with patch.object(candidate, "docker_json", return_value=[info]):
            with self.assertRaisesRegex(ValueError, "platform"):
                candidate.verify(self.document)

    def test_running_container_mismatch_fails(self):
        with patch.object(candidate.subprocess, "check_output", return_value="container-id\n"):
            with patch.object(candidate, "docker_json", side_effect=[[self.info], [{"Image": "sha256:wrong"}]]):
                with self.assertRaisesRegex(ValueError, "running stage"):
                    candidate.verify(self.document, "own-project")

    def test_missing_required_image_fails(self):
        del self.document["images"]["transcoder"]
        with self.assertRaisesRegex(ValueError, "missing"):
            candidate.verify(self.document)

    def test_requested_platform_mismatch_fails(self):
        with patch.object(candidate, "docker_json", return_value=[self.info]):
            with self.assertRaisesRegex(ValueError, "requested platform"):
                candidate.verify(self.document, platform="linux/arm64")

    def test_empty_archive_fails(self):
        self.archive.write_bytes(b"")
        self.document["archiveSHA256"] = candidate.file_hash(self.archive)
        with patch.object(candidate, "docker_json", return_value=[self.info]):
            with self.assertRaisesRegex(ValueError, "empty"):
                candidate.verify(self.document)

    def test_matching_but_empty_build_tags_fail(self):
        info = copy.deepcopy(self.info)
        info["Config"]["Labels"]["com.streamgate.build.tags"] = ""
        for entry in self.document["images"].values():
            entry["buildTags"] = [""]
        with patch.object(candidate, "docker_json", return_value=[info]):
            with self.assertRaisesRegex(ValueError, "tags"):
                candidate.verify(self.document)


if __name__ == "__main__":
    unittest.main()
