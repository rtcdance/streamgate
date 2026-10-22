import copy
import json
from pathlib import Path
import unittest

import test_generate_candidate_gates as fixtures
from summarize_ci_evidence import REQUIRED_JOBS, REQUIRED_SERVICES, summarize


class CISummaryTests(unittest.TestCase):
    def setUp(self):
        fixture = fixtures.CandidateGateGenerationTests()
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        self.fixture = fixture
        self.root = fixture.root
        self.revision = fixture.revision
        self.manifest_path = self.root / "ci-manifest.json"
        candidate = json.loads((self.root / "candidate.json").read_text())
        self.images = {}
        for service in REQUIRED_SERVICES:
            image = candidate["images"].get(service, {"imageID": "sha256:" + "5" * 64,
                      "revision": self.revision, "platform": "linux/amd64", "buildTags": ["nats_integration"]}).copy()
            image.update({"archiveSHA256": "6" * 64, "registryDigest": "sha256:" + "7" * 64})
            self.images[service] = image
        originals = copy.deepcopy(self.images)
        for image in originals.values():
            image["registryDigest"] = None
        candidate["ciBuildInputs"] = {"images": originals}
        for image in candidate["images"].values():
            image["buildArchiveSHA256"] = "6" * 64
        (self.root / "candidate.json").write_text(json.dumps(candidate))
        fixture.generate()
        (self.root / "command.exit").write_text("0\n")
        self.manifest = {"schemaVersion": 2, "candidateSHA": self.revision, "revision": self.revision,
                         "platform": "linux/amd64", "images": self.images,
                         "registryPublication": {"status": "RECORDED", "ciRun": "123", "ciAttempt": "1"}}
        self.needs = {job: {"result": "success"} for job in REQUIRED_JOBS}

    def invoke(self, event="push"):
        self.manifest_path.write_text(json.dumps(self.manifest))
        return summarize(self.root, self.manifest_path, self.needs, self.revision, "123", "1", event, self.root)

    def test_full_candidate_and_nine_raw_gates_bind_to_this_ci_run(self):
        result = self.invoke()
        self.assertEqual(result["status"], "REQUIRED_GATES_PASS")
        self.assertEqual(set(result["gates"]), {f"M{i}" for i in range(9)})
        self.assertEqual(result["ciRun"], "123")
        self.assertEqual(set(result["requirements"]), {f"R{i:02d}" for i in range(1, 13)})
        for row in result["requirements"].values():
            self.assertEqual(row["candidateSHA"], self.revision)
            self.assertTrue(row["scenarios"])
            self.assertTrue(row["artifacts"])
            self.assertTrue(all(len(code["sha256"]) == 64 for code in row["code"]))

    def test_missing_skipped_cancelled_failed_or_extra_job_fails(self):
        for status in ("skipped", "cancelled", "failure", "pending"):
            self.needs["acceptance"]["result"] = status
            with self.subTest(status=status), self.assertRaisesRegex(ValueError, "required CI job"):
                self.invoke()
        del self.needs["acceptance"]
        with self.assertRaises(ValueError):
            self.invoke()

    def test_requirement_code_hashes_come_from_the_candidate_tree(self):
        first = self.invoke()["requirements"]["R01"]["code"]
        (self.root / "pkg/gateway/routes.go").write_text("uncommitted replacement\n")
        self.assertEqual(self.invoke()["requirements"]["R01"]["code"], first)

    def test_outer_exit_cannot_be_hidden_by_a_precleanup_index(self):
        for status in ("1", "124", "137", ""):
            (self.root / "command.exit").write_text(status)
            with self.subTest(status=status), self.assertRaisesRegex(ValueError, "raw exit"):
                self.invoke()

    def test_tampered_or_missing_raw_evidence_is_rejected(self):
        (self.root / "logs/media.log").write_text("changed after acceptance\n")
        with self.assertRaisesRegex(ValueError, "hash mismatch"):
            self.invoke()

    def test_another_sha_or_build_cannot_inherit_acceptance(self):
        for field, value in (("imageID", "sha256:" + "f" * 64), ("archiveSHA256", "f" * 64),
                             ("revision", "f" * 40), ("platform", "linux/arm64"), ("buildTags", ["wrong"])):
            original = self.images["monolith"][field]
            self.images["monolith"][field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.invoke()
            self.images["monolith"][field] = original

    def test_missing_material_gate_is_not_silently_optional(self):
        index = json.loads((self.root / "candidate-gates.json").read_text())
        index["gates"] = [gate for gate in index["gates"] if gate["id"] != "M6"]
        (self.root / "candidate-gates.json").write_text(json.dumps(index))
        with self.assertRaisesRegex(ValueError, "M0-M8"):
            self.invoke()

    def test_non_pr_requires_this_runs_publication_receipts(self):
        for status in ({"status": "NOT_RECORDED"}, {"status": "RECORDED", "ciRun": "122", "ciAttempt": "1"}):
            self.manifest["registryPublication"] = status
            with self.subTest(status=status), self.assertRaisesRegex(ValueError, "publication"):
                self.invoke()

    def test_pr_records_no_registry_push(self):
        self.manifest["registryPublication"] = {"status": "NOT_RECORDED"}
        for image in self.images.values():
            image["registryDigest"] = None
        self.assertEqual(self.invoke("pull_request")["registryPublication"]["status"], "NOT_RECORDED")
        self.images["monolith"]["registryDigest"] = "sha256:" + "7" * 64
        with self.assertRaisesRegex(ValueError, "PR image"):
            self.invoke("pull_request")


if __name__ == "__main__":
    unittest.main()
