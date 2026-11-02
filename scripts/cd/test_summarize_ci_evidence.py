import copy
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
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

    def invoke(self, event="push", attempt="1", publications=None):
        self.manifest_path.write_text(json.dumps(self.manifest))
        extra = {} if publications is None else {"registry_dir": publications,
                                               "registry_repository": "ghcr.io/example/streamgate"}
        return summarize(self.root, self.manifest_path, self.needs, self.revision, "123", attempt,
                         event, self.root, **extra)

    def publications(self):
        root = self.root / "publications"
        root.mkdir(exist_ok=True)
        for service, image in self.images.items():
            log = root / f"{service}.push.log"
            log.write_text(f"latest: digest: {image['registryDigest']} size: 123\n")
            log.with_suffix(".exit").write_text("0\n")
            image["registryReference"] = f"ghcr.io/example/streamgate/{service}@{image['registryDigest']}"
            image["registryPublicationAttempt"] = "1"
            receipt = {**image, "schemaVersion": 1, "service": service, "candidateSHA": self.revision,
                       "ciRun": "123", "ciAttempt": "1", "pushExitCode": 0, "pushLog": log.name,
                       "pushLogSHA256": hashlib.sha256(log.read_bytes()).hexdigest()}
            (root / f"{service}.json").write_text(json.dumps(receipt))
        return root

    def test_inherited_manifest_requires_actual_receipt_revalidation(self):
        with self.assertRaisesRegex(ValueError, "publication"):
            self.invoke(attempt="2")

    def test_revalidation_preserves_original_manifest_and_producer_identity(self):
        publications = self.publications()
        result = self.invoke(attempt="2", publications=publications)
        self.assertEqual(result["registryPublication"]["ciAttempt"], "2")
        self.assertEqual(json.loads(self.manifest_path.read_text())["registryPublication"]["ciAttempt"], "1")
        self.assertEqual(result["images"]["monolith"]["registryPublicationAttempt"], "1")
        proof = result["registryPublicationEvidence"]
        self.assertEqual(proof["sourceVerification"]["ciAttempt"], "1")
        self.assertEqual(len(proof["receipts"]), len(REQUIRED_SERVICES))
        self.assertEqual(proof["sourceManifestSHA256"], hashlib.sha256(self.manifest_path.read_bytes()).hexdigest())

    def test_revalidation_rejects_wrong_publication_bindings(self):
        publications = self.publications()
        path = publications / "monolith.json"
        original = json.loads(path.read_text())
        for key, value in (("ciRun", "122"), ("ciAttempt", "2"), ("ciAttempt", "3"), ("candidateSHA", "f" * 40),
                           ("archiveSHA256", "f" * 64), ("imageID", "sha256:" + "f" * 64),
                           ("platform", "linux/arm64"), ("buildTags", ["wrong"]),
                           ("pushExitCode", 1), ("service", "auth")):
            with self.subTest(key=key):
                path.write_text(json.dumps({**original, key: value}))
                with self.assertRaises(ValueError):
                    self.invoke(attempt="2", publications=publications)

    def test_revalidation_rejects_tampered_push_log(self):
        publications = self.publications()
        (publications / "monolith.push.log").write_text("changed\n")
        with self.assertRaisesRegex(ValueError, "raw push"):
            self.invoke(attempt="2", publications=publications)

    def test_revalidation_cannot_change_the_accepted_registry_digest(self):
        publications = self.publications()
        self.images["monolith"]["registryDigest"] = "sha256:" + "f" * 64
        with self.assertRaisesRegex(ValueError, "registry identity"):
            self.invoke(attempt="2", publications=publications)

    def test_revalidation_rejects_a_coherently_substituted_publication(self):
        publications = self.publications()
        path = publications / "monolith.json"
        receipt = json.loads(path.read_text())
        digest = "sha256:" + "f" * 64
        log = publications / "monolith.push.log"
        log.write_text(f"latest: digest: {digest} size: 123\n")
        receipt.update(registryDigest=digest, registryReference=f"ghcr.io/example/streamgate/monolith@{digest}",
                       pushLogSHA256=hashlib.sha256(log.read_bytes()).hexdigest())
        path.write_text(json.dumps(receipt))
        with self.assertRaisesRegex(ValueError, "registry identity"):
            self.invoke(attempt="2", publications=publications)

    def test_revalidation_rejects_a_future_manifest_or_wrong_run(self):
        publications = self.publications()
        for value in ({"status": "RECORDED", "ciRun": "123", "ciAttempt": "3"},
                      {"status": "RECORDED", "ciRun": "122", "ciAttempt": "1"}):
            self.manifest["registryPublication"] = value
            with self.subTest(value=value), self.assertRaisesRegex(ValueError, "publication"):
                self.invoke(attempt="2", publications=publications)

    def test_revalidation_cannot_reuse_a_failed_all(self):
        publications = self.publications()
        (self.root / "command.exit").write_text("1\n")
        with self.assertRaisesRegex(ValueError, "raw exit"):
            self.invoke(attempt="2", publications=publications)

    def test_revalidation_cannot_hide_a_skipped_job(self):
        publications = self.publications()
        self.needs["acceptance"]["result"] = "skipped"
        with self.assertRaisesRegex(ValueError, "required CI job"):
            self.invoke(attempt="2", publications=publications)

    def test_revalidation_rejects_missing_receipts(self):
        publications = self.publications()
        (publications / "auth.json").unlink()
        with self.assertRaisesRegex(ValueError, "publication receipts"):
            self.invoke(attempt="2", publications=publications)

    def test_revalidation_rejects_symlinked_receipts(self):
        publications = self.publications()
        path = publications / "auth.json"
        saved = self.root / "outside-receipt.json"
        path.rename(saved)
        path.symlink_to(saved)
        with self.assertRaisesRegex(ValueError, "escapes"):
            self.invoke(attempt="2", publications=publications)

    def test_cli_writes_revalidation_proof_without_changing_the_manifest(self):
        publications = self.publications()
        self.manifest_path.write_text(json.dumps(self.manifest))
        before = self.manifest_path.read_bytes()
        needs = self.root / "needs.json"
        needs.write_text(json.dumps(self.needs))
        output = self.root / "ci-evidence.json"
        result = subprocess.run([
            sys.executable, str(Path(__file__).with_name("summarize_ci_evidence.py")), str(self.root),
            "--manifest", str(self.manifest_path), "--needs", str(needs), "--revision", self.revision,
            "--ci-run", "123", "--ci-attempt", "2", "--event", "push", "--registry-dir", str(publications),
            "--registry-repository", "ghcr.io/example/streamgate", "--out", str(output),
        ], cwd=self.root, capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(output.read_text())["registryPublication"]["ciAttempt"], "2")
        self.assertEqual(self.manifest_path.read_bytes(), before)

    def relocate(self):
        """Build at A, move the artifact root to B, then delete A."""
        candidate = json.loads((self.root / "candidate.json").read_text())
        candidate["archive"] = "candidate-images.tar"
        (self.root / "candidate.json").write_text(json.dumps(candidate))
        self.fixture.generate()
        self.manifest_path.write_text(json.dumps(self.manifest))
        moved = self.root.parent / (self.root.name + "-relocated")
        shutil.copytree(self.root, moved)
        self.addCleanup(shutil.rmtree, moved, True)
        shutil.rmtree(self.root)
        return moved

    def test_relocated_candidate_binds_through_the_real_summarize(self):
        moved = self.relocate()
        result = summarize(moved, moved / "ci-manifest.json", self.needs, self.revision,
                           "123", "1", "push", moved)
        self.assertEqual(result["status"], "REQUIRED_GATES_PASS")
        index = json.loads((moved / "candidate-gates.json").read_text())
        paths = {item["path"] for gate in index["gates"] for item in gate["artifacts"]}
        self.assertTrue(paths)
        self.assertTrue(all(not Path(path).is_absolute() for path in paths), sorted(paths))
        self.assertTrue(all((moved / path).is_file() for path in paths))

    def test_relocated_candidate_rejects_an_artifact_changed_after_the_move(self):
        moved = self.relocate()
        (moved / "logs/media.log").write_text("changed after relocation\n")
        with self.assertRaisesRegex(ValueError, "hash mismatch"):
            summarize(moved, moved / "ci-manifest.json", self.needs, self.revision,
                      "123", "1", "push", moved)

    def test_relocated_run_may_not_repair_paths_by_rewriting_candidate_json(self):
        moved = self.relocate()
        candidate = json.loads((moved / "candidate.json").read_text())
        candidate["archive"] = str(self.root / "candidate-images.tar")
        (moved / "candidate.json").write_text(json.dumps(candidate))
        with self.assertRaisesRegex(ValueError, "escapes"):
            summarize(moved, moved / "ci-manifest.json", self.needs, self.revision,
                      "123", "1", "push", moved)

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
