#!/usr/bin/env python3
"""Contract tests for deriving M0-M8 from same-candidate raw artifacts."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch


HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("candidate_gates", HERE / "generate_candidate_gates.py")
generator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generator)
spec2 = importlib.util.spec_from_file_location("candidate_evidence", HERE / "assert_candidate_evidence.py")
evidence = importlib.util.module_from_spec(spec2)
spec2.loader.exec_module(evidence)


def write_schema_fixture(root, revision):
    directory = root / "release-compat"
    directory.mkdir()
    for name in ("schema-before.txt", "schema-after.txt"):
        (directory / name).write_text("public|contents|id|uuid|NO|\n")
    digest = hashlib.sha256((directory / "schema-before.txt").read_bytes()).hexdigest()
    baseline_id = "sha256:" + "8" * 64
    (root / "reference.json").write_text(json.dumps({
        "revision": "c" * 40, "images": {"monolith": {"imageID": baseline_id}},
    }))
    candidate_id = json.loads((root / "candidate.json").read_text())["images"]["monolith"]["imageID"]
    (directory / "compatibility.json").write_text(json.dumps({
        "status": "PASS", "candidateSHA": revision, "baselineSHA": "c" * 40,
        "baselineImageID": baseline_id, "restoredImageID": candidate_id,
        "schemaBeforeSHA256": digest, "schemaAfterSHA256": digest,
    }))
    with (root / generator.STAGE_LOGS["release-compat"]).open("a") as output:
        output.write("RELEASE_SCHEMA_BASELINE_STARTED\nRELEASE_SCHEMA_BUSINESS_PASS\nRELEASE_SCHEMA_A_RECOVERED\n")


class CandidateGateGenerationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "config").mkdir()
        self.protocol = self.root / "config/performance.acceptance.json"
        self.protocol.write_text(json.dumps({"baseline": "test", "safetyBaselineRevision": "c" * 40}) + "\n")
        subprocess.run(["git", "init", "-q", str(self.root)], check=True)
        # A developer's global excludes file must not decide which fixture paths exist.
        subprocess.run(["git", "-C", str(self.root), "config", "core.excludesFile", os.devnull], check=True)
        subprocess.run(["git", "-C", str(self.root), "config", "user.email", "test@example.invalid"], check=True)
        subprocess.run(["git", "-C", str(self.root), "config", "user.name", "candidate gate test"], check=True)
        (self.root / "TRANSITION_PLAN.md").write_text(
            "<!-- streamgate-acceptance-status: incomplete -->\n\nCurrent acceptance is incomplete.\n")
        (self.root / "docs/resume").mkdir(parents=True)
        for relative in generator.DOCS:
            path = self.root / relative
            path.write_text("Evidence is traced to TRANSITION_PLAN.md; 人工讲述由本人完成。\n")
        from summarize_ci_evidence import REQUIREMENTS
        code_paths = {path for _, paths in REQUIREMENTS.values() for path in paths}
        for relative in code_paths:
            path = self.root / relative
            if path.exists():
                continue
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("synthetic source fixture for requirement trace\n")
        subprocess.run(["git", "-C", str(self.root), "add", "TRANSITION_PLAN.md", "docs/resume", *sorted(code_paths)], check=True)
        subprocess.run(["git", "-C", str(self.root), "commit", "-qm", "candidate material fixture"], check=True)
        self.revision = subprocess.check_output(
            ["git", "-C", str(self.root), "rev-parse", "HEAD"], text=True,
        ).strip()

        archive = self.root / "candidate-images.tar"
        archive.write_bytes(b"candidate image archive")
        images = {
            service: {"revision": self.revision, "platform": "linux/amd64",
                      "imageID": "sha256:" + hashlib.sha256(service.encode()).hexdigest(),
                      "buildTags": ["nats_integration"], "registryDigest": None}
            for service in ("monolith", "api-gateway", "transcoder")
        }
        (self.root / "candidate.json").write_text(json.dumps({
            "revision": self.revision,
            "archive": str(archive),
            "archiveSHA256": hashlib.sha256(archive.read_bytes()).hexdigest(),
            "images": images,
        }))

        for stage, relative in generator.STAGE_LOGS.items():
            path = self.root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(f"PASS: {stage} fixture\nstage={stage} PASS\n")
        write_schema_fixture(self.root, self.revision)
        (self.root / "durability-tests.json").write_text(json.dumps({
            "Action": "pass",
            "Package": "github.com/rtcdance/streamgate/pkg/service/gating",
            "Test": "TestAcceptance_GatingRuleSnapshotRepeatableRead",
        }) + "\n")
        m7_events = []
        for test_id, marker in generator.M7_TESTS:
            m7_events.extend((
                {"Action": "output", "Package": generator.M7_PACKAGE,
                 "Test": test_id, "Output": marker + "\n"},
                {"Action": "pass", "Package": generator.M7_PACKAGE, "Test": test_id},
            ))
        with (self.root / "durability-tests.json").open("a") as output:
            output.write("\n".join(json.dumps(event) for event in m7_events) + "\n")
        media = self.root / generator.STAGE_LOGS["media"]
        with media.open("a") as output:
            output.write("\n".join(generator.M3_MARKERS) + "\n")
        drills = self.root / generator.STAGE_LOGS["release-drills"]
        with drills.open("a") as output:
            output.write("\n".join(generator.M5_DRILL_MARKERS) + "\n")
        smoke = self.root / "release-drills/broken-smoke.log"
        smoke.parent.mkdir(parents=True)
        smoke.write_text("PASS: Anvil chainId=31337\nFAIL: dev-mint failed: HTTP 500\n")
        (smoke.parent / "broken-start.log").write_text("auth.jwt_secret is only 6 bytes\n")
        for directory in ("perf", "perf-reference", "perf-compare"):
            (self.root / directory).mkdir()
        (self.root / "perf/environment.json").write_text(json.dumps({
            "commit": self.revision,
            "harness_revision": self.revision,
            "fixture_hash": "b" * 64,
            "protocolSHA256": hashlib.sha256(self.protocol.read_bytes()).hexdigest(),
        }) + "\n")
        (self.root / "perf/round-1.json").write_text('{"result":"candidate"}\n')
        (self.root / "perf-reference/round-1.json").write_text('{"result":"reference"}\n')
        (self.root / "perf-compare/comparison.json").write_text(json.dumps({
            "ratio": 1.1, "candidateMedianP95MS": 110, "referenceMedianP95MS": 100,
        }) + "\n")

    def generate(self):
        with patch("pathlib.Path.cwd", return_value=self.root):
            return generator.generate(self.root, "test-project", self.revision, "linux/amd64")

    def commit_material_change(self, relative, content):
        (self.root / relative).write_text(content)
        subprocess.run(["git", "-C", str(self.root), "add", relative], check=True)
        subprocess.run(["git", "-C", str(self.root), "commit", "--amend", "--no-edit", "-q"], check=True)
        self.revision = subprocess.check_output(
            ["git", "-C", str(self.root), "rev-parse", "HEAD"], text=True,
        ).strip()
        candidate = json.loads((self.root / "candidate.json").read_text())
        candidate["revision"] = self.revision
        for image in candidate["images"].values():
            image["revision"] = self.revision
        (self.root / "candidate.json").write_text(json.dumps(candidate))
        perf_environment = json.loads((self.root / "perf/environment.json").read_text())
        perf_environment["commit"] = self.revision
        perf_environment["harness_revision"] = self.revision
        (self.root / "perf/environment.json").write_text(json.dumps(perf_environment) + "\n")
        compatibility = self.root / "release-compat/compatibility.json"
        value = json.loads(compatibility.read_text())
        value["candidateSHA"] = self.revision
        compatibility.write_text(json.dumps(value))

    def write_m7_events(self, output_events, terminal_events):
        events = [{
            "Action": "pass",
            "Package": "github.com/rtcdance/streamgate/pkg/service/gating",
            "Test": "TestAcceptance_GatingRuleSnapshotRepeatableRead",
        }]
        events.extend(output_events)
        events.extend(terminal_events)
        (self.root / "durability-tests.json").write_text(
            "\n".join(json.dumps(event) for event in events) + "\n")

    def test_complete_local_evidence_generates_all_nine_gates(self):
        self.generate()
        evidence.validate(self.root, self.root)
        index = json.loads((self.root / "candidate-gates.json").read_text())
        self.assertEqual({gate["id"] for gate in index["gates"]}, {f"M{i}" for i in range(9)})
        self.assertEqual(index["sameShaRemoteCI"], {"required": True, "status": "PENDING"})
        self.assertEqual(index["rawExitCode"], 0)
        self.assertEqual(index["configHash"], hashlib.sha256(self.protocol.read_bytes()).hexdigest())
        self.assertEqual(index["fixtureHash"], "b" * 64)
        m4 = next(gate for gate in index["gates"] if gate["id"] == "M4")
        paths = [artifact["path"] for artifact in m4["artifacts"]]
        self.assertEqual(paths.count("perf-compare/comparison.json"), 1)

    def test_changed_protocol_is_not_misreported_as_candidate_evidence(self):
        (self.root / "config/performance.acceptance.json").write_text('{"baseline":"changed"}\n')
        with self.assertRaisesRegex(ValueError, "config hash differs"):
            self.generate()

    def test_final_all_rejects_a_reused_reference_result(self):
        (self.root / "perf-reference/reference-reuse.json").write_text('{"reused":true}\n')
        with self.assertRaisesRegex(ValueError, "remeasure its reference"):
            self.generate()

    def test_missing_release_drill_marker_fails_generation(self):
        path = self.root / generator.STAGE_LOGS["release-drills"]
        path.write_text("PASS: release drills fixture\nstage=release-drills PASS\n")
        with self.assertRaisesRegex(ValueError, "M5 local release drills are incomplete"):
            self.generate()

    def test_missing_stage_completion_marker_fails_generation(self):
        path = self.root / generator.STAGE_LOGS["release-smoke"]
        path.write_text("PASS: one business assertion\n")
        with self.assertRaisesRegex(ValueError, "release-smoke has no successful raw stage completion marker"):
            self.generate()

    def test_changed_archive_bytes_do_not_match_candidate_manifest(self):
        archive = self.root / "candidate-images.tar"
        archive.write_bytes(b"changed")
        with self.assertRaisesRegex(ValueError, "candidate archive SHA256 mismatch"):
            generator.generate(self.root, "test-project", self.revision, "linux/amd64")

    def test_m5_indexes_the_real_broken_smoke_output(self):
        self.generate()
        index = json.loads((self.root / "candidate-gates.json").read_text())
        m5 = next(gate for gate in index["gates"] if gate["id"] == "M5")
        self.assertIn("release-drills/broken-smoke.log", {item["path"] for item in m5["artifacts"]})
        self.assertIn("release-drills/broken-start.log", {item["path"] for item in m5["artifacts"]})

    def test_m5_requires_exact_baseline_business_compatibility_and_schema(self):
        path = self.root / "release-compat/compatibility.json"
        original = json.loads(path.read_text())
        for field, value in (("status", "SKIP"), ("candidateSHA", "f" * 40), ("baselineSHA", "f" * 40),
                             ("baselineImageID", "sha256:" + "f" * 64), ("restoredImageID", "sha256:" + "f" * 64),
                             ("schemaAfterSHA256", "f" * 64)):
            path.write_text(json.dumps({**original, field: value}))
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, "business compatibility"):
                self.generate()
        path.write_text(json.dumps(original))
        (self.root / "release-compat/schema-after.txt").write_text("different schema\n")
        with self.assertRaisesRegex(ValueError, "business compatibility"):
            self.generate()

    def test_m5_refuses_unrelated_failure_diagnostics(self):
        for filename, diagnostic in (("broken-start.log", "database unavailable"),
                                     ("broken-smoke.log", "FAIL: missing browser")):
            path = self.root / "release-drills" / filename
            original = path.read_text()
            path.write_text(diagnostic)
            with self.subTest(filename=filename), self.assertRaisesRegex(ValueError, "raw diagnostic"):
                self.generate()
            path.write_text(original)

    def test_m3_requires_the_real_postgres_snapshot_test_event(self):
        (self.root / "durability-tests.json").write_text('{"Action":"skip","Test":"TestAcceptance_GatingRuleSnapshotRepeatableRead"}\n')
        with self.assertRaisesRegex(ValueError, "policy snapshot test did not PASS"):
            self.generate()

    def test_m7_requires_exact_passing_tests_and_their_own_output_markers(self):
        self.generate()
        index = json.loads((self.root / "candidate-gates.json").read_text())
        m7 = next(gate for gate in index["gates"] if gate["id"] == "M7")
        self.assertIn("durability-tests.json", {item["path"] for item in m7["artifacts"]})
        self.assertEqual(sum("M7_" in scenario for scenario in m7["scenarios"]), 2)

    def test_m7_skip_or_failure_does_not_count_as_pass(self):
        output_events = []
        terminal_events = []
        for test_id, marker in generator.M7_TESTS:
            output_events.append(
                {"Action": "output", "Package": generator.M7_PACKAGE,
                 "Test": test_id, "Output": marker + "\n"})
            terminal_events.append(
                {"Action": "skip" if test_id == generator.M7_TESTS[0][0] else "pass",
                 "Package": generator.M7_PACKAGE, "Test": test_id})
        self.write_m7_events(output_events, terminal_events)
        with self.assertRaisesRegex(ValueError, "passing empty-anchor or rollback"):
            self.generate()

    def test_m7_marker_from_another_test_is_not_accepted(self):
        output_events = []
        terminal_events = []
        for test_id, marker in generator.M7_TESTS:
            output_events.append(
                {"Action": "output", "Package": generator.M7_PACKAGE,
                 "Test": "UnrelatedTest", "Output": marker + "\n"})
            terminal_events.append(
                {"Action": "pass", "Package": generator.M7_PACKAGE, "Test": test_id})
        self.write_m7_events(output_events, terminal_events)
        with self.assertRaisesRegex(ValueError, "passing empty-anchor or rollback"):
            self.generate()

    def test_m6_snapshots_candidate_tree_docs_into_artifacts(self):
        artifacts_root = self.root / ".verify-artifacts" / "separate-run"
        artifacts_root.mkdir(parents=True)
        with patch("pathlib.Path.cwd", return_value=self.root):
            artifacts, scenarios = generator._doc_scenarios(artifacts_root, self.root, self.revision)
        self.assertEqual({item["path"] for item in artifacts},
                         {generator.PLAN, *generator.DOCS})
        for relative in (generator.PLAN, *generator.DOCS):
            self.assertEqual((artifacts_root / relative).read_bytes(),
                             (self.root / relative).read_bytes())
        self.assertIn("candidate plan status snapshot: incomplete", scenarios)

    def test_m6_rejects_current_completion_claims_while_plan_is_incomplete(self):
        self.commit_material_change(
            generator.DOCS[0],
            "证据状态:已完成并有实测。AI 辅助；本人讲述待确认。证据见 TRANSITION_PLAN.md。\n",
        )
        with self.assertRaisesRegex(ValueError, "stale completion claim"):
            self.generate()

    def test_m6_requires_a_single_near_top_plan_status_marker(self):
        self.commit_material_change(generator.PLAN, "No status marker.\n")
        with self.assertRaisesRegex(ValueError, "exactly one current acceptance status marker"):
            self.generate()


if __name__ == "__main__":
    unittest.main()
