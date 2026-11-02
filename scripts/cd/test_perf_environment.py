import copy
import hashlib
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
from perf_environment import capture
from perf_protocol import compare_environments


class EnvironmentTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(); self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.binary = self.root / "loader.test"; self.binary.write_bytes(b"test loader")
        self.image = "sha256:" + "d" * 64
        self.binary.with_suffix(".build.json").write_text(json.dumps({"harnessRevision": "b" * 40, "imageID": self.image, "binarySHA256": hashlib.sha256(self.binary.read_bytes()).hexdigest()}))
        self.protocol = json.loads(Path("config/performance.acceptance.json").read_text())
        self.services = {}
        budgets = dict(self.protocol["infrastructureResources"], monolith=self.protocol["resources"]["api"], transcoder=self.protocol["resources"]["worker"])
        for name, limits in budgets.items():
            self.services[name] = {"Image": self.image, "Config": {"Image": "pinned-image", "Env": ["SECRET=must-not-escape"],
                "Labels": {"com.docker.compose.service": name}}, "HostConfig": {"NanoCpus": limits["nanoCPUs"], "Memory": limits["memoryBytes"]}}
        self.playback = {"commit": "a" * 40, "harness_revision": "b" * 40, "scenario": "warm_cache", "viewers": 50,
                         "fixture_hash": "c" * 64, "content_id": "content", "quality": "640x360", "media_hashes_sha256": "e" * 64}
        (self.root / "warm_cache-viewers-50.json").write_text(json.dumps(self.playback))
        self.resources = {"containers": {role: {"imageID": self.image, "run": "one-run"} for role in ("api", "worker", "loader")}}
        self.save_resources()
        self.environment = {"PERF_LOADER_BINARY": str(self.binary), "PERF_LOADER_IMAGE": self.image, "PERF_TARGET_SHA": "a" * 40}
        fixture = self.root / "media-hashes.json"; fixture.write_text('{"/segment.ts": "test"}\n')
        fixture.with_suffix(".provenance.json").write_text('{"source":"unit storage fixture"}\n')
        self.environment["PERF_MEDIA_HASHES_FILE"] = str(fixture)
        task = self.root / "transcoding-metadata.json"; task.write_text('{"source":"unit task metadata"}\n')
        self.environment["PERF_TRANSCODE_EVIDENCE_FILE"] = str(task)
        # A neighbour container that belongs to another compose project on the
        # same daemon: it is background load, never part of the benchmark.
        self.peer_id = "background"
        self.peer = {"Image": "sha256:" + "9" * 64, "Config": {"Image": "neighbour:1",
            "Labels": {"com.docker.compose.project": "neighbour-project", "com.docker.compose.service": "kafka"}},
            "HostConfig": {"NanoCpus": 1000000000, "Memory": 536870912}}

    def save_resources(self):
        (self.root / "resources-warm_cache-50.json").write_text(json.dumps(self.resources))

    def command(self, *args):
        if args[0] == "git": return "b" * 40
        if args[:2] == ("docker", "info"):
            return json.dumps({"ID": "same-runner", "OSType": "linux", "Architecture": "x86_64", "NCPU": 8, "MemTotal": 8000000000, "KernelVersion": "one-kernel", "ServerVersion": "29"})
        if args[:2] == ("docker", "ps"):
            if args[2] == "--format": return f"own|own-project\n{self.peer_id}|"
            self.assertEqual(args[-1], "label=com.docker.compose.project=own-project")
            return "\n".join(self.services)
        if args[:2] == ("docker", "inspect"):
            record = self.services.get(args[-1]) or (self.peer if args[-1] == self.peer_id else None)
            self.assertIsNotNone(record, f"unexpected inspect target {args[-1]}")
            return json.dumps([record])
        return "pinned-tool-version"

    def capture(self):
        with patch.dict(os.environ, self.environment), patch("perf_environment.command", side_effect=self.command):
            return capture(self.root, "own-project", "deploy/docker/compose.verify.yml")

    def test_actual_budgets_loader_and_no_secret_environment(self):
        manifest = self.capture()
        self.assertEqual(manifest["loader"]["kind"], "docker")
        self.assertEqual(manifest["loader"]["limits"], self.protocol["resources"]["loader"])
        self.assertEqual(manifest["applicationImages"]["api"], self.image)
        self.assertNotIn("must-not-escape", json.dumps(manifest))
        compare_environments(manifest, copy.deepcopy(manifest))

    def test_infrastructure_and_application_limits_are_read_back(self):
        for role in ("postgres", "monolith", "transcoder"):
            with self.subTest(role=role):
                previous = self.services[role]["HostConfig"]["NanoCpus"]
                self.services[role]["HostConfig"]["NanoCpus"] = 0
                with self.assertRaises(ValueError): self.capture()
                self.services[role]["HostConfig"]["NanoCpus"] = previous

    def test_missing_service_fails(self):
        del self.services["anvil-fallback-proxy"]
        with self.assertRaises(ValueError): self.capture()

    def test_missing_binary_and_wrong_loader_image_fail(self):
        self.binary.unlink()
        with self.assertRaises(ValueError): self.capture()
        self.binary.write_bytes(b"test loader")
        self.resources["containers"]["loader"]["imageID"] = "sha256:" + "f" * 64; self.save_resources()
        with self.assertRaises(ValueError): self.capture()

    def test_cross_runner_or_tool_or_protocol_comparison_fails(self):
        manifest = self.capture()
        for field in ("runner", "harness_revision", "protocolSHA256", "composeSHA256", "tools", "fixture_hash", "quality", "loader"):
            changed = copy.deepcopy(manifest); changed[field] = "different"
            with self.subTest(field=field), self.assertRaises(ValueError): compare_environments(manifest, changed)
        changed = copy.deepcopy(manifest); changed["services"]["postgres"]["imageID"] = "different"
        with self.assertRaises(ValueError): compare_environments(manifest, changed)

    def test_desktop_scope_platform_and_background_peers_are_bound_to_comparison(self):
        self.environment.update(VERIFY_ENVIRONMENT_PROFILE="desktop-local", VERIFY_PLATFORM="linux/amd64")
        manifest = self.capture()
        self.assertEqual(manifest["runner"]["backgroundContainers"], [{
            "service": "kafka",
            "imageID": "sha256:" + "9" * 64, "imageReference": "neighbour:1",
            "limits": {"nanoCPUs": 1000000000, "memoryBytes": 536870912}}])
        self.assertEqual(manifest["backgroundContainerIDs"], ["background"])
        self.assertIn("no production capacity claim", manifest["runner"]["measurementScope"])
        for field, value in (("executionProfile", "native-linux"), ("executionPlatform", "linux/arm64"),
                             ("backgroundContainers", ["changed"])):
            changed = copy.deepcopy(manifest)
            changed["runner"][field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                compare_environments(manifest, changed)

    def test_a_restarted_neighbour_is_the_same_background_load(self):
        # The neighbour restarts between the reference and candidate rounds: it
        # gets a new instance ID and a new compose project name, while the
        # services, images and limits drawing on the host stay the same.
        manifest = self.capture()
        self.peer_id = "background-restarted"
        self.peer["Config"]["Labels"]["com.docker.compose.project"] = "neighbour-project-2"
        restarted = self.capture()
        self.assertNotEqual(manifest["backgroundContainerIDs"], restarted["backgroundContainerIDs"])
        self.assertEqual(manifest["runner"]["backgroundContainers"], restarted["runner"]["backgroundContainers"])
        compare_environments(manifest, restarted)

    def test_background_composition_changes_are_refused(self):
        manifest = self.capture()
        mutations = {
            "service": lambda peer: peer.update(service="zookeeper"),
            "image": lambda peer: peer.update(imageID="sha256:" + "8" * 64),
            "cpu": lambda peer: peer["limits"].update(nanoCPUs=2000000000),
            "memory": lambda peer: peer["limits"].update(memoryBytes=1073741824),
        }
        for name, mutate in mutations.items():
            changed = copy.deepcopy(manifest)
            mutate(changed["runner"]["backgroundContainers"][0])
            with self.subTest(change=name), self.assertRaises(ValueError):
                compare_environments(manifest, changed)
        changed = copy.deepcopy(manifest)
        changed["runner"]["backgroundContainers"].append(dict(manifest["runner"]["backgroundContainers"][0], service="extra"))
        with self.subTest(change="extra-peer"), self.assertRaises(ValueError):
            compare_environments(manifest, changed)


if __name__ == "__main__": unittest.main()
