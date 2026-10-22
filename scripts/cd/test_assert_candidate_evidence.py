"""Negative contracts for the final same-candidate evidence index."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
from test_generate_candidate_gates import write_schema_fixture


HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("candidate_gates", HERE / "generate_candidate_gates.py")
generator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generator)
spec2 = importlib.util.spec_from_file_location("evidence", HERE / "assert_candidate_evidence.py")
evidence = importlib.util.module_from_spec(spec2)
spec2.loader.exec_module(evidence)


class OverallEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.revision = "a" * 40
        (self.root / "config").mkdir()
        self.protocol = self.root / "config/performance.acceptance.json"
        self.protocol.write_text(json.dumps({"baseline": "test", "safetyBaselineRevision": "c" * 40}) + "\n")
        (self.root / "candidate-images.tar").write_bytes(b"candidate archive")
        images = {
            service: {"imageID": "sha256:" + hashlib.sha256(service.encode()).hexdigest(),
                      "registryDigest": None, "buildTags": ["nats_integration"],
                      "platform": "linux/amd64", "revision": self.revision}
            for service in ("monolith", "api-gateway", "transcoder")
        }
        (self.root / "candidate.json").write_text(json.dumps({
            "revision": self.revision,
            "archive": str(self.root / "candidate-images.tar"),
            "archiveSHA256": hashlib.sha256((self.root / "candidate-images.tar").read_bytes()).hexdigest(),
            "images": images,
        }))
        (self.root / "TRANSITION_PLAN.md").write_text(
            "<!-- streamgate-acceptance-status: incomplete -->\n\nCurrent acceptance is incomplete.\n")
        (self.root / "docs/resume").mkdir(parents=True)
        for relative in generator.DOCS:
            path = self.root / relative
            path.write_text("Evidence is traced to TRANSITION_PLAN.md; 人工讲述由本人完成。\n")
        subprocess.run(["git", "init", "-q", str(self.root)], check=True)
        subprocess.run(["git", "-C", str(self.root), "config", "user.email", "test@example.invalid"], check=True)
        subprocess.run(["git", "-C", str(self.root), "config", "user.name", "evidence test"], check=True)
        subprocess.run(["git", "-C", str(self.root), "add", "TRANSITION_PLAN.md", "docs/resume"], check=True)
        subprocess.run(["git", "-C", str(self.root), "commit", "-qm", "candidate material fixture"], check=True)
        self.revision = subprocess.check_output(
            ["git", "-C", str(self.root), "rev-parse", "HEAD"], text=True,
        ).strip()
        candidate = json.loads((self.root / "candidate.json").read_text())
        candidate["revision"] = self.revision
        for image in candidate["images"].values():
            image["revision"] = self.revision
        (self.root / "candidate.json").write_text(json.dumps(candidate))

        for stage, relative in generator.STAGE_LOGS.items():
            path = self.root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(f"PASS: {stage} fixture\nstage={stage} PASS\n")
        write_schema_fixture(self.root, self.revision)
        m7_events = [{
            "Action": "pass",
            "Package": "github.com/rtcdance/streamgate/pkg/service/gating",
            "Test": "TestAcceptance_GatingRuleSnapshotRepeatableRead",
        }]
        for test_id, marker in generator.M7_TESTS:
            m7_events.extend((
                {"Action": "output", "Package": generator.M7_PACKAGE,
                 "Test": test_id, "Output": marker + "\n"},
                {"Action": "pass", "Package": generator.M7_PACKAGE, "Test": test_id},
            ))
        (self.root / "durability-tests.json").write_text(
            "\n".join(json.dumps(event) for event in m7_events) + "\n")
        with (self.root / generator.STAGE_LOGS["media"]).open("a") as output:
            output.write("\n".join(generator.M3_MARKERS) + "\n")
        with (self.root / generator.STAGE_LOGS["release-drills"]).open("a") as output:
            output.write("\n".join(generator.M5_DRILL_MARKERS) + "\n")
        smoke = self.root / "release-drills/broken-smoke.log"
        smoke.parent.mkdir(parents=True, exist_ok=True)
        smoke.write_text("PASS: Anvil chainId=31337\nFAIL: dev-mint failed: HTTP 500\n")
        (smoke.parent / "broken-start.log").write_text("auth.jwt_secret is only 6 bytes\n")
        for directory in ("perf", "perf-reference", "perf-compare"):
            (self.root / directory).mkdir()
        fixture_hash = "b" * 64
        (self.root / "perf/environment.json").write_text(json.dumps({
            "commit": self.revision, "harness_revision": self.revision,
            "fixture_hash": fixture_hash,
            "protocolSHA256": hashlib.sha256(self.protocol.read_bytes()).hexdigest(),
        }) + "\n")
        (self.root / "perf/round-1.json").write_text('{"result":"candidate"}\n')
        (self.root / "perf-reference/round-1.json").write_text('{"result":"reference"}\n')
        (self.root / "perf-compare/comparison.json").write_text(json.dumps({
            "ratio": 1.1, "candidateMedianP95MS": 110, "referenceMedianP95MS": 100,
        }) + "\n")
        with patch("pathlib.Path.cwd", return_value=self.root):
            generator.generate(self.root, "test-project", self.revision, "linux/amd64")
        self.index = json.loads((self.root / "candidate-gates.json").read_text())

    def write(self, index=None):
        (self.root / "candidate-gates.json").write_text(json.dumps(index or self.index))

    def validate(self):
        evidence.validate(self.root, self.root)

    def test_complete_identity_index_passes(self):
        self.write()
        self.validate()

    def test_missing_index_fails(self):
        (self.root / "candidate-gates.json").unlink()
        with self.assertRaises(FileNotFoundError):
            self.validate()

    def test_missing_gate_fails(self):
        self.index["gates"].pop()
        self.write()
        with self.assertRaisesRegex(ValueError, "M0-M8"):
            self.validate()

    def test_duplicate_gate_fails(self):
        self.index["gates"][-1] = copy.deepcopy(self.index["gates"][0])
        self.write()
        with self.assertRaisesRegex(ValueError, "M0-M8"):
            self.validate()

    def test_skipped_gate_fails(self):
        self.index["gates"][0]["status"] = "SKIP"
        self.write()
        with self.assertRaisesRegex(ValueError, "incomplete"):
            self.validate()

    def test_wrong_candidate_fails(self):
        self.index["gates"][0]["candidateSHA"] = "old-candidate"
        self.write()
        with self.assertRaisesRegex(ValueError, "candidate"):
            self.validate()

    def test_wrong_image_fails(self):
        self.index["gates"][0]["imageIDs"] = ["sha256:other"]
        self.write()
        with self.assertRaisesRegex(ValueError, "image"):
            self.validate()

    def test_changed_evidence_fails(self):
        self.write()
        (self.root / "logs/unit.log").write_text("tampered")
        with self.assertRaisesRegex(ValueError, "hash"):
            self.validate()

    def test_evidence_escape_fails(self):
        self.index["gates"][0]["artifacts"][0]["path"] = "../outside.json"
        self.write()
        with self.assertRaisesRegex(ValueError, "escapes"):
            self.validate()

    def test_remote_ci_cannot_be_claimed_by_local_index(self):
        self.index["sameShaRemoteCI"]["status"] = "PASS"
        self.write()
        with self.assertRaisesRegex(ValueError, "pending"):
            self.validate()

    def test_archive_identity_change_fails(self):
        (self.root / "candidate-images.tar").write_bytes(b"tampered archive")
        self.write()
        with self.assertRaisesRegex(ValueError, "archive identity"):
            self.validate()

    def test_config_hash_change_fails(self):
        (self.root / "config/performance.acceptance.json").write_text("changed\n")
        self.write()
        with self.assertRaisesRegex(ValueError, "config hash"):
            self.validate()


if __name__ == "__main__":
    unittest.main()
