from pathlib import Path
import re
import subprocess
import tempfile
import unittest


SOURCE = Path(__file__).resolve().parent.parent / "verify-docker.sh"


def function(name, source=SOURCE):
    return re.search(r"^" + name + r"\(\) \{\n.*?^\}", source.read_text(), re.M | re.S)[0]


class VerifyShellTests(unittest.TestCase):
    def run_shell(self, body):
        with tempfile.TemporaryDirectory() as directory:
            script = '\n'.join([
                'set -euo pipefail', 'log() { :; }', 'fail() { exit 1; }',
                f'LOG_DIR="{directory}"', function("run_logged"), body,
            ])
            return subprocess.run(["bash", "-c", script], capture_output=True, text=True)

    def test_logged_failure_preserves_exit(self):
        self.assertEqual(self.run_shell("run_logged case bash -c 'exit 37'").returncode, 37)

    def test_function_failure_cannot_be_hidden_by_following_success(self):
        result = self.run_shell("task() { bash -c 'exit 29'; echo incorrectly-continued; }; run_logged case task")
        self.assertEqual(result.returncode, 29, result.stdout)
        self.assertNotIn("incorrectly-continued", result.stdout)

    def test_success_passes(self):
        self.assertEqual(self.run_shell("run_logged case bash -c 'exit 0'").returncode, 0)

    def test_logged_subshell_does_not_clean_parent_resources(self):
        result = self.run_shell("trap 'echo outer-cleanup' EXIT; run_logged case true")
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout.count("outer-cleanup"), 1, result.stdout)

    def run_image_preparation(self, docker_body, image="pinned-image@sha256:" + "1" * 64):
        script = '\n'.join([
            'set -euo pipefail', 'VERIFY_PLATFORM=linux/amd64',
            'log() { :; }', 'fail() { exit 1; }',
            'timeout() { shift; "$@"; }', docker_body,
            function("platform_variant_available"), function("ensure_runtime_image"),
            'ensure_runtime_image ' + image,
        ])
        return subprocess.run(["bash", "-c", script], capture_output=True, text=True)

    # The stubs model the platform probe the code really uses. `docker image
    # inspect --platform` is deliberately NOT trusted as an availability answer:
    # on Docker 29 desktop-linux it exits 0 with an empty Os/Arch for an
    # index-only reference stored as a foreign architecture, and the old code
    # returned early on that success — the H5 proxy then died with "image with
    # reference nginx:alpine was found but does not provide the specified
    # platform (linux/amd64)". `docker create` is what resolves the variant the
    # same way `docker run` will, so it is the availability answer.
    def test_cached_image_never_contacts_registry(self):
        result = self.run_image_preparation('docker() { case "$1" in create) echo cid; return 0 ;; rm) return 0 ;; esac; echo unexpected-pull >&2; return 1; }')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("unexpected-pull", result.stderr)

    def test_empty_platform_inspect_is_not_trusted_as_availability(self):
        # The regression: an engine answering `image inspect --platform` with
        # success (empty Os/Arch) while only a foreign-architecture image is
        # stored. The run must still prepare the requested variant instead of
        # returning early and failing later inside `docker run`.
        result = self.run_image_preparation(
            'docker() { case "$1" in '
            'image) echo ""; return 0 ;; '
            'create) if [ -n "$pulled" ]; then echo cid; return 0; fi; return 1 ;; '
            'pull) echo pull-attempt; pulled=1; return 0 ;; '
            'rm) return 0 ;; '
            'esac; return 1; }')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.count("pull-attempt"), 1, result.stdout)

    def test_native_platform_variant_avoids_pull(self):
        result = self.run_image_preparation('docker() { case "$1" in create) echo cid; return 0 ;; rm) return 0 ;; pull) echo unexpected-pull >&2 ;; esac; return 1; }')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("unexpected-pull", result.stderr)

    def test_image_becoming_usable_only_after_pull_pulls_exactly_once(self):
        result = self.run_image_preparation(
            'docker() { case "$1" in '
            'create) if [ -n "$pulled" ]; then echo cid; return 0; fi; return 1 ;; '
            'pull) echo pull-attempt; pulled=1; return 0 ;; '
            'rm) return 0 ;; '
            'esac; return 1; }')
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertEqual(result.stdout.count("pull-attempt"), 1, result.stdout)

    def test_failed_image_pull_stops_after_three_attempts(self):
        result = self.run_image_preparation('docker() { if [ "$1" = pull ]; then echo pull-attempt; fi; return 1; }')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout.count("pull-attempt"), 3)

    def test_mutable_image_reference_is_rejected(self):
        result = self.run_image_preparation('docker() { echo incorrectly-contacted-registry; return 0; }', image="mutable:latest")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("incorrectly-contacted-registry", result.stdout)

    def test_cleanup_keeps_original_failure(self):
        script = '\n'.join([
            'set -euo pipefail', 'docker() { return 43; }', 'PROXY_PIDS=""',
            'PROJECT_NAME=own-run', 'COMPOSE_FILE=own-file', 'CREATED_PROJECTS="own-project"',
            'VERIFY_CONTROLLER=own-controller', 'VERIFY_COMPOSE_FILE=own-file',
            function("cleanup"), 'trap cleanup EXIT', 'exit 39',
        ])
        self.assertEqual(subprocess.run(["bash", "-c", script], capture_output=True).returncode, 39)

    def test_cleanup_failure_cannot_turn_success_green(self):
        script = '\n'.join([
            'set -euo pipefail', 'docker() { return 43; }', 'PROXY_PIDS=""',
            'PROJECT_NAME=own-run', 'COMPOSE_FILE=own-file', 'CREATED_PROJECTS=""',
            function("cleanup"), 'trap cleanup EXIT', 'exit 0',
        ])
        self.assertNotEqual(subprocess.run(["bash", "-c", script], capture_output=True).returncode, 0)

    def test_signal_exit_is_preserved(self):
        script = '\n'.join([
            'set -euo pipefail', 'docker() { return 0; }', 'PROXY_PIDS=""',
            'PROJECT_NAME=own-run', 'COMPOSE_FILE=own-file', 'CREATED_PROJECTS=""',
            function("cleanup"), 'trap cleanup EXIT', "trap 'exit 143' TERM", 'kill -TERM $$',
        ])
        self.assertEqual(subprocess.run(["bash", "-c", script], capture_output=True).returncode, 143)

    def test_outer_cleanup_keeps_original_failure(self):
        script = '\n'.join([
            'set -euo pipefail', 'docker() { return 43; }',
            'VERIFY_CONTROLLER=own-controller', 'VERIFY_PROJECT=own-project',
            function("cleanup", SOURCE.with_name("verify-controller.sh")), 'trap cleanup EXIT', 'exit 39',
        ])
        self.assertEqual(subprocess.run(["bash", "-c", script], capture_output=True).returncode, 39)

    def test_outer_cleanup_failure_cannot_turn_success_green(self):
        script = '\n'.join([
            'set -euo pipefail', 'docker() { return 43; }',
            'VERIFY_CONTROLLER=own-controller', 'VERIFY_PROJECT=own-project',
            function("cleanup", SOURCE.with_name("verify-controller.sh")), 'trap cleanup EXIT', 'exit 0',
        ])
        self.assertNotEqual(subprocess.run(["bash", "-c", script], capture_output=True).returncode, 0)


if __name__ == "__main__":
    unittest.main()
