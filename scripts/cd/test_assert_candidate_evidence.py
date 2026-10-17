import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


spec = importlib.util.spec_from_file_location("evidence", Path(__file__).with_name("assert_candidate_evidence.py"))
evidence = importlib.util.module_from_spec(spec)
spec.loader.exec_module(evidence)


class OverallEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        (self.root / "candidate.json").write_text(json.dumps({"revision": "candidate", "images": {"monolith": {"imageID": "sha256:image"}}}))
        (self.root / "raw.json").write_text('{"status":"PASS"}\n')
        digest = hashlib.sha256((self.root / "raw.json").read_bytes()).hexdigest()
        self.index = {"candidateSHA": "candidate", "gates": [
            {"id": f"M{i}", "status": "PASS", "candidateSHA": "candidate", "imageID": "sha256:image",
             "command": "actual-command", "scenarios": [f"scenario-{i}"], "artifacts": [{"path": "raw.json", "sha256": digest}]}
            for i in range(9)
        ]}

    def write(self, index=None):
        (self.root / "candidate-gates.json").write_text(json.dumps(index or self.index))

    def test_complete_identity_index_passes(self):
        self.write()
        evidence.validate(self.root)

    def test_missing_index_fails(self):
        with self.assertRaises(FileNotFoundError):
            evidence.validate(self.root)

    def test_missing_gate_fails(self):
        self.index["gates"].pop()
        self.write()
        with self.assertRaisesRegex(ValueError, "M0-M8"):
            evidence.validate(self.root)

    def test_duplicate_gate_fails(self):
        self.index["gates"][-1] = copy.deepcopy(self.index["gates"][0])
        self.write()
        with self.assertRaisesRegex(ValueError, "M0-M8"):
            evidence.validate(self.root)

    def test_skipped_gate_fails(self):
        self.index["gates"][0]["status"] = "SKIP"
        self.write()
        with self.assertRaisesRegex(ValueError, "incomplete"):
            evidence.validate(self.root)

    def test_wrong_candidate_fails(self):
        self.index["gates"][0]["candidateSHA"] = "old-candidate"
        self.write()
        with self.assertRaisesRegex(ValueError, "candidate"):
            evidence.validate(self.root)

    def test_wrong_image_fails(self):
        self.index["gates"][0]["imageID"] = "sha256:other"
        self.write()
        with self.assertRaisesRegex(ValueError, "image"):
            evidence.validate(self.root)

    def test_changed_evidence_fails(self):
        self.write()
        (self.root / "raw.json").write_text("tampered")
        with self.assertRaisesRegex(ValueError, "hash"):
            evidence.validate(self.root)

    def test_evidence_escape_fails(self):
        self.index["gates"][0]["artifacts"][0]["path"] = "../outside.json"
        self.write()
        with self.assertRaisesRegex(ValueError, "escapes"):
            evidence.validate(self.root)


if __name__ == "__main__":
    unittest.main()
