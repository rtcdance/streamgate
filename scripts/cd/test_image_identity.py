from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).parent))
from image_identity import identity


class ImageIdentityTests(unittest.TestCase):
    def setUp(self):
        self.info = {
            "Id": "sha256:image-config", "RepoDigests": ["registry/image@sha256:registry-manifest"],
            "Os": "linux", "Architecture": "amd64",
            "Config": {"Labels": {"org.opencontainers.image.revision": "revision", "com.streamgate.build.tags": "nats_integration"}},
        }

    def test_digest_types_stay_distinct(self):
        result = identity(self.info, "revision", "archive-hash")
        self.assertEqual(result["imageID"], "sha256:image-config")
        self.assertEqual(result["archiveSHA256"], "archive-hash")
        self.assertIsNone(result["registryDigest"])
        self.assertEqual(result["dockerRepoDigests"], ["registry/image@sha256:registry-manifest"])
        self.assertEqual(result["platform"], "linux/amd64")
        self.assertEqual(result["buildTags"], ["nats_integration"])

    def test_wrong_revision_fails(self):
        with self.assertRaisesRegex(ValueError, "revision"):
            identity(self.info, "wrong", "archive-hash")

    def test_missing_build_tags_fail(self):
        del self.info["Config"]["Labels"]["com.streamgate.build.tags"]
        with self.assertRaisesRegex(ValueError, "build tags"):
            identity(self.info, "revision", "archive-hash")

    def test_repo_digest_is_not_proof_of_a_registry_publication(self):
        self.info["RepoDigests"] = ["local-only@sha256:local-manifest"]
        result = identity(self.info, "revision", "archive-hash")
        self.assertIsNone(result["registryDigest"])


if __name__ == "__main__":
    unittest.main()
