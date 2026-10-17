import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


GATE = Path(__file__).with_name("assert_required_tests.py")
PKG = "example.test/service"


def event(action, test=None, package=PKG):
    value = {"Action": action, "Package": package}
    if test:
        value["Test"] = test
    return value


def passing(test="TestRequired"):
    return [event("start"), event("run", test), event("pass", test), event("pass")]


class RequiredTestsGate(unittest.TestCase):
    def invoke(self, events, *, code=0, required=None, suffix=""):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = {"suites": {"test": required if required is not None else [{"package": PKG, "test": "TestRequired"}]}}
            (root / "manifest.json").write_text(json.dumps(manifest))
            (root / "events.json").write_text("".join(json.dumps(e) + "\n" for e in events) + suffix)
            return subprocess.run([
                sys.executable, str(GATE), "--manifest", str(root / "manifest.json"),
                "--suite", "test", "--exit-code", str(code), str(root / "events.json"),
            ], text=True, capture_output=True)

    def test_complete_run_passes(self):
        result = self.invoke(passing())
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_original_exit_code_is_preserved(self):
        self.assertNotEqual(self.invoke(passing(), code=1).returncode, 0)

    def test_missing_one_required_test_fails(self):
        required = [{"package": PKG, "test": name} for name in ("TestRequired", "TestMissing")]
        self.assertNotEqual(self.invoke(passing(), required=required).returncode, 0)

    def test_wrong_package_cannot_satisfy_requirement(self):
        events = [dict(e, Package="other.test/service") for e in passing()]
        self.assertNotEqual(self.invoke(events).returncode, 0)

    def test_package_build_failure_fails(self):
        self.assertNotEqual(self.invoke(passing() + [event("fail", package="example.test/broken")]).returncode, 0)

    def test_required_subtest_skip_fails(self):
        events = passing()[:2] + [event("run", "TestRequired/case"), event("skip", "TestRequired/case")] + passing()[2:]
        self.assertNotEqual(self.invoke(events).returncode, 0)

    def test_required_subtest_failure_fails(self):
        events = passing()[:2] + [event("fail", "TestRequired/case")] + passing()[2:]
        self.assertNotEqual(self.invoke(events).returncode, 0)

    def test_missing_package_completion_fails(self):
        self.assertNotEqual(self.invoke(passing()[:-1]).returncode, 0)

    def test_incomplete_additional_package_fails(self):
        self.assertNotEqual(self.invoke(passing() + [event("start", package="example.test/unfinished")]).returncode, 0)

    def test_malformed_json_fails(self):
        self.assertNotEqual(self.invoke(passing(), suffix="not-json\n").returncode, 0)

    def test_truncated_json_fails(self):
        self.assertNotEqual(self.invoke(passing(), suffix='{"Action":').returncode, 0)

    def test_empty_log_fails(self):
        self.assertNotEqual(self.invoke([]).returncode, 0)

    def test_pass_without_execution_fails(self):
        self.assertNotEqual(self.invoke([event("start"), event("pass", "TestRequired"), event("pass")]).returncode, 0)

    def test_unexpected_test_failure_fails(self):
        self.assertNotEqual(self.invoke(passing() + [event("fail", "TestOther")]).returncode, 0)

    def test_empty_suite_fails(self):
        self.assertNotEqual(self.invoke(passing(), required=[]).returncode, 0)

    def test_duplicate_required_identity_fails(self):
        required = [{"package": PKG, "test": "TestRequired"}] * 2
        self.assertNotEqual(self.invoke(passing(), required=required).returncode, 0)

    def test_unfinished_required_subtest_fails(self):
        events = passing()[:2] + [event("run", "TestRequired/unfinished")] + passing()[2:]
        self.assertNotEqual(self.invoke(events).returncode, 0)

    def test_package_completion_without_start_fails(self):
        self.assertNotEqual(self.invoke(passing() + [event("pass", package="other.test/service")]).returncode, 0)

    def test_unknown_event_action_fails(self):
        self.assertNotEqual(self.invoke(passing() + [event("unknown")]).returncode, 0)

    def test_event_after_package_completion_fails(self):
        self.assertNotEqual(self.invoke(passing() + [event("run", "TestTooLate")]).returncode, 0)


if __name__ == "__main__":
    unittest.main()
