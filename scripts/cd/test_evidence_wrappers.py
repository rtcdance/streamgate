"""Static and behavioural checks for the evidence transport wrappers.

The wrappers are the only place where the Actions path list and the restore
bindings are assembled, so a mistake there is invisible to the helper tests.
These checks run the wrapper's own shell with a fixture environment; they never
contact a registry, Docker, or the Actions artifact service.
"""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
UPLOAD = ROOT / ".github/actions/evidence-upload/action.yml"
DOWNLOAD = ROOT / ".github/actions/evidence-download/action.yml"


def step_run(path, step_id):
    lines = path.read_text().splitlines()
    for index, line in enumerate(lines):
        if line.strip() == "id: " + step_id:
            for offset in range(index, len(lines)):
                if lines[offset].strip() != "run: |":
                    continue
                indent = len(lines[offset]) - len(lines[offset].lstrip())
                body = []
                for following in lines[offset + 1:]:
                    if following.strip() and (len(following) - len(following.lstrip())) <= indent:
                        break
                    body.append(following[indent + 2:] if len(following) > indent + 2 else "")
                return "\n".join(body)
    raise AssertionError(f"no run block for step {step_id}")


class WrapperTests(unittest.TestCase):
    def actions_path(self, paths, excludes):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "output"
            output.write_text("")
            environment = dict(
                os.environ,
                EVIDENCE_PATHS=paths,
                EVIDENCE_EXCLUDES=excludes,
                GITHUB_OUTPUT=str(output),
                RUNNER_TEMP=directory,
            )
            result = subprocess.run(
                ["bash", "-c", step_run(UPLOAD, "prepare")],
                env=environment, capture_output=True, text=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            text = output.read_text()
            marker = "actions_path<<EVIDENCE_EOF\n"
            body = text.split(marker, 1)[1].split("EVIDENCE_EOF", 1)[0]
            return [line for line in body.splitlines() if line]

    def test_negations_carry_the_artifact_root(self):
        # The original workflow wrote these negations workspace-relative; a bare
        # 'build-inputs/**' would be resolved against the workspace and never
        # match the nested directory, so include-hidden-files=true would leave
        # .private exposed.
        self.assertEqual(
            self.actions_path(".verify-artifacts/ci-all/", "build-inputs/**\n.private/**"),
            [
                ".verify-artifacts/ci-all/",
                "!.verify-artifacts/ci-all/build-inputs/**",
                "!.verify-artifacts/ci-all/.private/**",
            ],
        )

    def test_negations_stay_bare_at_the_workspace_root(self):
        self.assertEqual(
            self.actions_path("candidate-monolith.tar\nmeta/monolith.json", "meta/private/**"),
            ["candidate-monolith.tar", "meta/monolith.json", "!meta/private/**"],
        )

    def test_single_glob_path_keeps_its_directory(self):
        self.assertEqual(
            self.actions_path("fuzz-logs/", "tmp/**"),
            ["fuzz-logs/", "!fuzz-logs/tmp/**"],
        )

    def test_download_binds_the_current_run(self):
        body = step_run(DOWNLOAD, "transport")
        self.assertIn('--expect-run "$GITHUB_RUN_ID"', body)
        self.assertIn("--require-digest-refs", body)
        self.assertIn('--expect-names "$required_names"', body)


if __name__ == "__main__":
    unittest.main()
