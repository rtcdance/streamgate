import copy
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import import_ci_candidate as importer
from write_candidate_manifest import REQUIRED_SERVICES


class ImportCandidateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.inputs = self.root / "build-inputs"
        (self.inputs / "meta").mkdir(parents=True)
        self.output = self.root / "acceptance/candidate.json"
        self.revision = "a" * 40
        self.infos = {}
        for service in REQUIRED_SERVICES:
            archive = self.inputs / f"candidate-{service}.tar"
            archive.write_bytes(service.encode())
            image_id = "sha256:" + hashlib.sha256(service.encode()).hexdigest()
            data = {"imageID": image_id, "archiveSHA256": importer.file_hash(archive),
                    "registryDigest": None, "revision": self.revision,
                    "platform": "linux/amd64", "buildTags": ["nats_integration"]}
            (self.inputs / f"meta/{service}.json").write_text(json.dumps(data))
            self.infos[service] = {"Id": image_id, "Os": "linux", "Architecture": "amd64",
                                   "Config": {"Labels": {"org.opencontainers.image.revision": self.revision,
                                                         "com.streamgate.build.tags": "nats_integration"}}}
        self.commands = []

    def docker(self, command, **kwargs):
        self.commands.append(command)
        if command[1] == "save":
            Path(command[3]).write_bytes(b"combined fixture archive")
        return subprocess.CompletedProcess(command, 0)

    def inspect(self, *args):
        for service in importer.RUNTIME_SERVICES:
            if args[-1] in (f"streamgate-{service}:candidate", f"sg-fixture-{service}:latest"):
                return [copy.deepcopy(self.infos[service])]
        raise AssertionError(args)

    def invoke(self):
        with patch.object(importer.subprocess, "run", side_effect=self.docker), \
                patch.object(importer, "docker_json", side_effect=self.inspect), \
                patch("candidate_manifest.docker_json", side_effect=self.inspect):
            return importer.import_candidate(self.inputs, "sg-fixture", self.revision, "linux/amd64", self.output)

    def test_import_binds_runtime_images_to_all_original_build_archives(self):
        value = self.invoke()
        self.assertEqual(set(value["ciBuildInputs"]["images"]), REQUIRED_SERVICES)
        for service, entry in value["images"].items():
            self.assertEqual(entry["imageID"], self.infos[service]["Id"])
            self.assertEqual(entry["buildArchiveSHA256"], importer.file_hash(self.inputs / f"candidate-{service}.tar"))
        self.assertFalse(any("build" in command for command in self.commands))
        self.assertEqual(len([command for command in self.commands if command[1] == "load"]), 3)

    def test_manifest_paths_are_relative_to_the_artifact_root(self):
        value = self.invoke()
        self.assertEqual(value["archive"], "candidate-images.tar")
        self.assertEqual(value["ciBuildInputs"]["path"], "build-inputs")
        stored = json.loads(self.output.read_text())
        self.assertEqual(stored["archive"], "candidate-images.tar")
        self.assertFalse(Path(stored["archive"]).is_absolute())

    def test_substituted_archive_is_rejected_before_any_docker_operation(self):
        (self.inputs / "candidate-monolith.tar").write_bytes(b"substituted")
        with self.assertRaisesRegex(ValueError, "archive"):
            self.invoke()
        self.assertEqual(self.commands, [])
        self.assertFalse(self.output.exists())

    def test_missing_nonruntime_build_input_fails(self):
        (self.inputs / "meta/auth.json").unlink()
        with self.assertRaisesRegex(ValueError, "missing"):
            self.invoke()
        self.assertEqual(self.commands, [])

    def test_substituted_loaded_image_never_writes_a_manifest(self):
        self.infos["monolith"]["Id"] = "sha256:" + "f" * 64
        with self.assertRaisesRegex(ValueError, "imageID"):
            self.invoke()
        self.assertFalse(self.output.exists())
        self.assertFalse(any(command[1] in ("save", "tag") for command in self.commands))

    def test_load_failure_is_not_swallowed(self):
        with patch.object(importer.subprocess, "run", side_effect=subprocess.CalledProcessError(42, "docker load")):
            with self.assertRaises(subprocess.CalledProcessError) as error:
                importer.import_candidate(self.inputs, "sg-fixture", self.revision, "linux/amd64", self.output)
        self.assertEqual(error.exception.returncode, 42)
        self.assertFalse(self.output.exists())

    def test_cannot_replace_a_prior_run(self):
        self.invoke()
        previous = self.output.read_bytes()
        with self.assertRaisesRegex(ValueError, "already exists"):
            self.invoke()
        self.assertEqual(self.output.read_bytes(), previous)

    def test_mixed_build_facts_fail_before_docker(self):
        path = self.inputs / "meta/worker.json"
        original = json.loads(path.read_text())
        for field, value in (("revision", "b" * 40), ("platform", "linux/arm64"), ("buildTags", ["wrong"])):
            path.write_text(json.dumps({**original, field: value}))
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, "metadata"):
                self.invoke()
            self.assertEqual(self.commands, [])


if __name__ == "__main__":
    unittest.main()
