from pathlib import Path
import re
import shlex
import subprocess
import tempfile
import unittest


SOURCE = Path(__file__).resolve().parent.parent / "verify-docker.sh"


def function(name, source=SOURCE):
    return re.search(r"^" + name + r"\(\) \{\n.*?^\}", source.read_text(), re.M | re.S)[0]


class VerifyShellTests(unittest.TestCase):
    def test_reorg_requires_two_nonempty_distinct_canonical_hashes(self):
        source = SOURCE.parent / "durable-fault-acceptance.sh"
        old = "0x" + "a" * 64
        for actual, previous, expected in [("", old, 1), ("null", old, 1), (old, old, 1),
                                           ("0x" + "b" * 64, "", 1), ("0x" + "b" * 64, old, 0)]:
            script = '\n'.join(['set -euo pipefail', 'block_hash() { printf "%s" "$ACTUAL"; }',
                                 function("changed_block_hash", source),
                                 'changed_block_hash 10 "$PREVIOUS"'])
            import os
            result = subprocess.run(["bash", "-c", script], env={**os.environ, "ACTUAL": actual, "PREVIOUS": previous}, capture_output=True, text=True)
            with self.subTest(actual=actual, previous=previous):
                self.assertEqual(result.returncode, expected)
                if expected == 0: self.assertEqual(result.stdout, actual)

    def test_recovery_seed_requires_full_business_acceptance(self):
        source = function("stage_recovery")
        self.assertNotIn("VERIFY_SKIP_BROWSER_INSTALL", source)
        self.assertNotIn("acceptance_failed_only_in_browser", source)
        self.assertIn('run_logged recovery-seed run_fullchain "$src"', source)
        result = self.run_shell("run_fullchain() { echo 'FAIL: Browser playback failed'; return 17; }; run_logged recovery-seed run_fullchain")
        self.assertEqual(result.returncode, 17)

    def test_source_freeze_rejects_dirty_tree_or_changed_head(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            def git(*args):
                return subprocess.check_output(["git", "-C", directory, *args], text=True).strip()
            git("init", "-q"); git("config", "user.name", "Acceptance Test"); git("config", "user.email", "test@example.invalid")
            source = root / "source.go"; source.write_text("original\n")
            (root / ".gitignore").write_text(".agents/\n__pycache__/\n")
            git("add", "source.go", ".gitignore"); git("commit", "-qm", "initial")
            revision = git("rev-parse", "HEAD")
            script = '\n'.join(['set -euo pipefail', 'fail() { exit 1; }', function("assert_clean_revision"),
                                 'assert_clean_revision ' + shlex.quote(directory) + ' ' + revision])
            self.assertEqual(subprocess.run(["bash", "-c", script], capture_output=True).returncode, 0)
            (root / ".agents").mkdir(); (root / ".agents/state.json").write_text("{}\n")
            self.assertEqual(subprocess.run(["bash", "-c", script], capture_output=True).returncode, 0)
            extra = root / "untracked.go"; extra.write_text("new source\n")
            self.assertNotEqual(subprocess.run(["bash", "-c", script], capture_output=True).returncode, 0)
            extra.unlink()
            source.write_text("edited\n")
            self.assertNotEqual(subprocess.run(["bash", "-c", script], capture_output=True).returncode, 0)
            git("add", "source.go"); git("commit", "-qm", "changed head")
            self.assertNotEqual(subprocess.run(["bash", "-c", script], capture_output=True).returncode, 0)

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

    def test_standalone_perf_creates_log_directory(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            script = "\n".join([
                "set -euo pipefail", "VERIFY_MODE=pr", "PROJECT_NAME=own-perf", "IMAGE_TAG=tool",
                "ARTIFACT_DIR=" + shlex.quote(directory), "LOG_DIR=" + shlex.quote(str(root / "logs")),
                "VERIFY_MONO_PORT=1", "VERIFY_ANVIL_PORT=2", "VERIFY_MINIO_PORT=3",
                "log() { :; }", 'fail() { echo "$*" >&2; exit 1; }',
                "require_controller_tool() { :; }", "bring_up_fullstack() { :; }",
                "wait_for_anvil_proxy() { :; }", "assert_two_chain_endpoints() { :; }",
                "run_fullchain() { echo seed-invoked; return 17; }",
                "acceptance_failed_only_in_browser() { return 1; }",
                function("perf_acceptance_config"), function("perf_acceptance_list"),
                function("stage_perf"), "stage_perf",
            ])
            result = subprocess.run(["bash", "-c", script], capture_output=True, text=True)
            self.assertIn("seed-invoked", result.stdout, result.stderr)
            self.assertNotEqual(result.returncode, 0, "the original seed failure must still fail")
            self.assertIn("seed-invoked", (root / "logs" / "perf.log").read_text())

    def test_reference_setup_smoke_uses_reference_revision(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); target = "a" * 40
            script = "\n".join([
                "set -euo pipefail", "LOG_DIR=" + shlex.quote(directory), "PERF_COMPARE_VIEWERS=50",
                "VERIFY_COMPOSE_FILE=unused", "VERIFY_MONO_PORT=1", "VERIFY_ANVIL_PORT=2", "VERIFY_MINIO_PORT=3",
                "log() { :; }", "fail() { exit 1; }", "run_fullchain() { :; }",
                "docker() { case \"$*\" in *'SELECT id FROM'*) echo 11111111-1111-4111-8111-111111111111 ;; *'SELECT file_hash'*) echo " + "e" * 64 + " ;; *) return 1 ;; esac; }",
                "perf_prepare_fixture() { PERF_WALLET_KEYS_FILE=keys; }",
                "bash() { printf '%s' \"${PERF_COMMIT:-missing}\" > " + shlex.quote(str(root / "target")) + "; }",
                function("perf_seed_and_pool"),
                "perf_seed_and_pool own-project " + shlex.quote(directory) + " 1 " + target,
            ])
            result = subprocess.run(["bash", "-c", script], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual((root / "target").read_text(), target)

    def test_candidate_matrix_stamps_the_candidate_revision(self):
        # The measured matrix must not let the harness fall back to the ambient
        # `git rev-parse HEAD`: a concurrent checkout in the worktree then
        # relabels the evidence and the resource-identity gate refuses rows
        # whose containers carry the candidate revision while the recorded
        # workload carries another commit.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); revision = "c" * 40
            seen = root / "stamped"
            script = "\n".join([
                "set -euo pipefail", "VERIFY_MODE=pr", "VERIFY_SOURCE_REVISION=" + revision,
                "PROJECT_NAME=own-perf", "IMAGE_TAG=tool", "VERIFY_COMPOSE_FILE=unused", "PERF_WALLET_KEYS_FILE=unused",
                "ARTIFACT_DIR=" + shlex.quote(directory), "LOG_DIR=" + shlex.quote(str(root / "logs")),
                "VERIFY_MONO_PORT=1", "VERIFY_ANVIL_PORT=2", "VERIFY_MINIO_PORT=3",
                "log() { :; }", 'fail() { echo "$*" >&2; exit 1; }',
                "require_controller_tool() { :; }", "bring_up_fullstack() { :; }",
                "wait_for_anvil_proxy() { :; }", "assert_two_chain_endpoints() { :; }",
                "run_fullchain() { :; }", "perf_prepare_fixture() { :; }", "teardown_fullstack() { :; }",
                "acceptance_failed_only_in_browser() { return 1; }",
                'docker() { case "$*" in *"SELECT id FROM"*) echo 11111111-1111-4111-8111-111111111111 ;; *"SELECT file_hash"*) echo ' + ("e" * 64) + ' ;; *) : ;; esac; }',
                'bash() { case "$*" in *perf-acceptance.sh*) printf "%s|%s" "${PERF_COMMIT:-missing}" "${PERF_HARNESS_SHA:-missing}" > '
                + shlex.quote(str(seen)) + ' ;; esac; return 0; }',
                'python3() { if [ "$1" = "-" ]; then command python3 "$@"; else return 0; fi; }',
                function("perf_acceptance_config"), function("perf_acceptance_list"),
                function("stage_perf"), "stage_perf",
            ])
            result = subprocess.run(["bash", "-c", script], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(seen.read_text(), revision + "|" + revision,
                             "the measured matrix must stamp the candidate revision for both identities")

    def test_cleanup_removes_only_own_private_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); own = root / "own"; foreign = root / "foreign"
            for path in [own, foreign]:
                (path / ".private").mkdir(parents=True)
                (path / ".private" / "wallets.keys").write_text("test-only")
            (own / "perf-loader.test").write_text("test binary")
            script = "\n".join([
                "set -euo pipefail", "docker() { return 0; }", 'PROXY_PIDS=""',
                'PROJECT_NAME=own-run', 'COMPOSE_FILE=own-file', 'CREATED_PROJECTS=""',
                "ARTIFACT_DIR=" + shlex.quote(str(own)), function("cleanup"), "trap cleanup EXIT", "exit 0",
            ])
            result = subprocess.run(["bash", "-c", script], capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertFalse((own / ".private").exists())
            self.assertFalse((own / "perf-loader.test").exists())
            self.assertTrue((foreign / ".private" / "wallets.keys").exists())

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
