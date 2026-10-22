from pathlib import Path
import json
import re
import shlex
import subprocess
import tempfile
import unittest


SOURCE = Path(__file__).resolve().parent.parent / "verify-docker.sh"


def function(name, source=SOURCE):
    return re.search(r"^" + name + r"\(\) \{\n.*?^\}[ \t]*$", source.read_text(), re.M | re.S)[0]


class VerifyShellTests(unittest.TestCase):
    def test_release_reads_the_actual_frozen_baseline_from_the_protocol_helper(self):
        result = subprocess.run(["bash", "-c", function("perf_acceptance_config") + '\nperf_acceptance_config safetyBaselineRevision'],
                                cwd=SOURCE.parent.parent, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        configured = json.loads((SOURCE.parent.parent / "config/performance.acceptance.json").read_text())
        self.assertEqual(result.stdout.strip(), configured["safetyBaselineRevision"])

    def test_ci_requires_complete_docker_all_and_bound_evidence_before_promotion(self):
        source = (SOURCE.parent.parent / ".github/workflows/ci.yml").read_text()
        def job(name):
            match = re.search(r"^  " + name + r":\n.*?(?=^  [a-z][\w-]*:|\Z)", source, re.M | re.S)
            self.assertIsNotNone(match, f"missing required CI job: {name}")
            return match[0]
        acceptance = job("acceptance")
        self.assertIn("VERIFY_STAGE: all", acceptance)
        self.assertIn("VERIFY_MODE: candidate", acceptance)
        self.assertIn("VERIFY_BUILD_ARTIFACTS:", acceptance)
        self.assertIn("bash scripts/verify-docker.sh", acceptance)
        self.assertIn("command.exit", acceptance)
        self.assertIn("if: always()", acceptance)
        self.assertNotIn("continue-on-error", acceptance)
        self.assertNotIn("if: github.event_name", acceptance)
        evidence = job("evidence")
        self.assertIn("summarize_ci_evidence.py", evidence)
        self.assertIn("toJSON(needs)", evidence)
        self.assertIn("ci-acceptance-${{ github.sha }}", evidence)
        self.assertRegex(job("publishable"), r"needs:\s*\[evidence\]")

    def test_standalone_release_uses_the_same_container_entry_and_original_build_inputs(self):
        source = (SOURCE.parent.parent / ".github/workflows/release-validation.yml").read_text()
        self.assertIn("VERIFY_STAGE: release", source)
        self.assertIn("VERIFY_BUILD_ARTIFACTS:", source)
        self.assertIn("candidate_run_id:", source)
        self.assertIn("bash scripts/verify-docker.sh", source)
        for legacy in ("docker-compose.fullchain.yml", "sg-fc-monolith", "actions/setup-go@", "apt-get", "npm ci"):
            self.assertNotIn(legacy, source)

    def test_durability_entrypoints_schedule_every_required_package(self):
        root = SOURCE.parent.parent
        suite = json.loads((SOURCE.parent / "cd/required-tests.json").read_text())["suites"]["durability-tests"]
        prefix = "github.com/rtcdance/streamgate/"
        required = {"./" + row["package"].removeprefix(prefix) for row in suite}
        for entry in (SOURCE, root / ".github/workflows/ci.yml"):
            unfolded = re.sub(r"\\\n\s*", " ", entry.read_text())
            command = re.search(r"^\s*go test (?P<args>[^\n]+?)\s*>\s*(?:/artifacts/)?durability-tests\.json", unfolded, re.M)
            with self.subTest(entry=str(entry)):
                self.assertIsNotNone(command, "durability command was removed")
                packages = set(re.findall(r"(?:^|\s)(\./[\w./-]+)", command["args"]))
                self.assertFalse(required - packages, f"required packages not scheduled: {required - packages}")
                self.assertRegex(command["args"], r"(?:^|\s)-p\s+1(?:\s|$)", "DB fixtures must run serially across packages")

    def test_required_ci_artifacts_explicitly_retain_at_least_30_days(self):
        for workflow in ("ci.yml", "test.yml", "build.yml", "release-validation.yml"):
            lines = (SOURCE.parent.parent / ".github/workflows" / workflow).read_text().splitlines()
            uploads = 0
            for offset, line in enumerate(lines):
                if "uses: actions/upload-artifact@" not in line:
                    continue
                uploads += 1
                indentation = len(line) - len(line.lstrip())
                block = []
                for following in lines[offset + 1:]:
                    if following.strip() and len(following) - len(following.lstrip()) < indentation:
                        break
                    block.append(following)
                retention = re.search(r"retention-days:\s*(\d+)", "\n".join(block))
                with self.subTest(workflow=workflow, upload=offset + 1):
                    self.assertIsNotNone(retention, "retention must not depend on an organization default")
                    self.assertGreaterEqual(int(retention[1]), 30)
            self.assertGreater(uploads, 0)

    def run_schema_compatibility(self, *, smoke_exit=0, wrong_image=False, changed_schema=False, wrong_baseline=False):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            image = "sha256:" + "1" * 64
            baseline_image = "sha256:" + "2" * 64
            (root / "candidate.json").write_text(json.dumps({"images": {"monolith": {"imageID": image}}}))
            (root / "reference.json").write_text(json.dumps({
                "revision": ("f" if wrong_baseline else "c") * 40,
                "images": {"monolith": {"imageID": baseline_image, "tag": "sg-reference-monolith:latest"}},
            }))
            script = '\n'.join([
                'set -euo pipefail', f'ARTIFACT_DIR={shlex.quote(directory)}',
                f'CANDIDATE_MANIFEST={shlex.quote(str(root / "candidate.json"))}',
                'VERIFY_SOURCE_REVISION=' + "a" * 40, 'VERIFY_PLATFORM=linux/amd64',
                'VERIFY_COMPOSE_FILE=fixture.yml; VERIFY_MONO_PORT=12345; VERIFY_ANVIL_PORT=12346; VERIFY_MINIO_PORT=12347',
                'log() { echo "$*"; }; fail() { echo "$*" >&2; exit 1; }',
                'perf_acceptance_config() { echo ' + "c" * 40 + '; }',
                'python3() { if [ "$1" = scripts/cd/candidate_manifest.py ]; then return 0; fi; command python3 "$@"; }',
                'docker() { case "$1" in',
                f' inspect) echo {image if wrong_image else baseline_image} ;;',
                ' exec) if [ -f "$ARTIFACT_DIR/business-called" ] && [ ' + str(int(changed_schema)) + ' = 1 ]; then echo changed; else echo current-schema; fi ;;',
                ' compose) [[ " $* " != *" -p 8080 "* ]] || return 125 ;;',
                ' *) return 125 ;; esac; }',
                'release_remove_monolith_a() { :; }',
                'release_recover_monolith() { echo "$3"; touch "$ARTIFACT_DIR/candidate-recovered"; }',
                f'run_fullchain() {{ touch "$ARTIFACT_DIR/business-called"; return {smoke_exit}; }}',
                function("release_schema_compatibility"), 'release_schema_compatibility own-project',
            ])
            result = subprocess.run(["bash", "-c", script], capture_output=True, text=True, timeout=10)
            proof = root / "release-compat/compatibility.json"
            return result, json.loads(proof.read_text()) if proof.exists() else None

    def test_schema_compatibility_runs_business_smoke_and_restores_candidate(self):
        result, proof = self.run_schema_compatibility()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(proof["status"], "PASS")
        self.assertEqual(proof["schemaBeforeSHA256"], proof["schemaAfterSHA256"])
        self.assertEqual(proof["baselineSHA"], "c" * 40)
        self.assertIn("RELEASE_SCHEMA_BUSINESS_PASS", result.stdout)
        self.assertIn("RELEASE_SCHEMA_A_RECOVERED", result.stdout)
        self.assertIn('run_logged release-compat release_schema_compatibility "$project"', function("stage_release"))

    def test_schema_compatibility_does_not_turn_failed_business_smoke_into_health_success(self):
        result, proof = self.run_schema_compatibility(smoke_exit=17)
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertIsNone(proof)
        self.assertNotIn("RELEASE_SCHEMA_A_RECOVERED", result.stdout)

    def test_schema_compatibility_rejects_wrong_baseline_image_or_schema_changes(self):
        for condition, diagnostic in (("wrong_image", "not running the frozen baseline"),
                                      ("changed_schema", "changed the candidate schema"),
                                      ("wrong_baseline", "differs from the frozen safety baseline")):
            result, proof = self.run_schema_compatibility(**{condition: True})
            with self.subTest(condition=condition):
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertIsNone(proof)
                self.assertIn(diagnostic, result.stderr)

    def run_release_drills(self, smoke_exit=1, smoke_output=None, recovery_image=None, start_exit=1,
                           start_output="auth.jwt_secret is only 6 bytes — minimum 32 bytes required for HS256"):
        image = "sha256:" + "1" * 64
        if smoke_output is None:
            smoke_output = "PASS: Anvil chainId=31337\nFAIL: dev-mint failed: HTTP 500"
        with tempfile.TemporaryDirectory() as directory:
            manifest = Path(directory) / "candidate.json"
            manifest.write_text(json.dumps({"images": {"monolith": {"imageID": image}}}))
            script = '\n'.join([
                'set -euo pipefail',
                f'ARTIFACT_DIR={shlex.quote(directory)}',
                f'CANDIDATE_MANIFEST={shlex.quote(str(manifest))}',
                'VERIFY_COMPOSE_FILE=fixture.yml; VERIFY_MONO_PORT=12345; VERIFY_ANVIL_PORT=12346; VERIFY_MINIO_PORT=12347',
                'log() { echo "$*"; }; fail() { echo "$*" >&2; exit 1; }',
                'sleep() { SECONDS=$((SECONDS+200)); }; wait_health() { return 0; }; curl() { echo 200; }',
                # This is absent from Dockerfile.verify; a host installation must
                # not hide a missing controller dependency.
                'rg() { echo "unsupported controller tool: rg" >&2; return 127; }',
                'docker() {',
                '  local args="$*" field="${3:-}" name="${@: -1}" run_args',
                '  case "$1" in',
                '    compose)',
                '      if [[ " $args " == *" run "* ]]; then',
                '        run_args="${args#* run }"',
                '        [[ " $run_args " != *" -p "* ]] || { echo "unexpected host port publication" >&2; return 125; }',
                '        [[ "$args" != *"/nonexistent-streamgate-entrypoint"* ]] || return 125',
                '      fi; return 0 ;;',
                f'    logs) printf "%s\\n" {shlex.quote(start_output)} ;;',
                '    inspect)',
                '      case "$field" in',
                f'        "{{{{.Image}}}}") if [[ "$name" == *"-monolith-1" ]]; then echo {shlex.quote(recovery_image or image)}; else echo {image}; fi ;;',
                '        "{{.State.Status}}") if [[ "$name" == *"-drill-start" ]]; then echo exited; else echo running; fi ;;',
                f'        "{{{{.State.ExitCode}}}}") echo {start_exit} ;;',
                '        *) if [[ "$name" == *"-drill-health" ]]; then echo unhealthy; else echo healthy; fi ;;',
                '      esac ;;',
                '    rm) return 0 ;;',
                '    *) return 125 ;;',
                '  esac',
                '}',
                f'run_fullchain() {{ printf "%s\\n" {shlex.quote(smoke_output)}; return {smoke_exit}; }}',
                function("release_remove_monolith_a"), function("release_recover_monolith"),
                function("release_failure_drills"), 'release_failure_drills own-project',
            ])
            return subprocess.run(["bash", "-c", script], capture_output=True, text=True, timeout=10)

    def test_release_drills_require_all_three_failures_and_candidate_recovery(self):
        result = self.run_release_drills()
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        for kind in ("START", "HEALTH", "SMOKE"):
            self.assertIn(f"RELEASE_DRILL_{kind}_B_FAILED", result.stdout)
            self.assertIn(f"RELEASE_DRILL_{kind}_A_RECOVERED", result.stdout)

    def test_release_drills_refuse_a_wrong_recovered_image(self):
        result = self.run_release_drills(recovery_image="sha256:" + "2" * 64)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("RELEASE_DRILL_START_A_RECOVERED", result.stdout)

    def test_release_drills_refuse_unrelated_or_timed_out_smoke_failure(self):
        for code, output in ((1, "FAIL: missing browser"), (124, "PASS: Anvil chainId=31337\nFAIL: dev-mint failed: HTTP 500")):
            with self.subTest(code=code):
                result = self.run_release_drills(smoke_exit=code, smoke_output=output)
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("RELEASE_DRILL_SMOKE_B_FAILED", result.stdout)

    def test_release_drills_refuse_a_successful_start_of_broken_b(self):
        result = self.run_release_drills(start_exit=0)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("RELEASE_DRILL_START_B_FAILED", result.stdout)

    def test_release_drills_refuse_an_unrelated_startup_failure(self):
        result = self.run_release_drills(start_output="Postgres unavailable")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("RELEASE_DRILL_START_B_FAILED", result.stdout)

    def test_fullchain_preserves_raw_failure_exit_for_each_deployment_mode(self):
        for target in (1, 2):
            for code in (1, 124, 137):
                script = '\n'.join([
                    'set -euo pipefail; calls=0; VERIFY_H5_PORT=12345',
                    'require_controller_tool() { :; }; ensure_playwright() { :; }; start_h5_nginx() { :; }',
                    'log() { :; }; diag_stack_logs() { echo diagnostic; }',
                    f'env() {{ calls=$((calls+1)); if [ "$calls" = {target} ]; then return {code}; fi; }}',
                    function("run_fullchain"), 'run_fullchain own-project 12346 12347 12348 12349',
                ])
                with self.subTest(target=target, code=code):
                    result = subprocess.run(["bash", "-c", script], capture_output=True, text=True)
                    self.assertEqual(result.returncode, code, result.stderr + result.stdout)

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

    def test_h5_static_dir_is_traversable_by_the_unprivileged_nginx_worker(self):
        # Regression from sg-all-1021-final10: mktemp -d left the extracted
        # frontend at 0700 root, the unprivileged nginx worker answered 403 on
        # /demo/, and wait_health (which requires 200) failed the media stage
        # with "H5 proxy did not become ready". function() stops at the nginx
        # config heredoc's column-0 brace, so extract past the NGINX delimiter.
        source = SOURCE.read_text()
        start = source.index("start_h5_nginx() {")
        body = source[start:source.index("\n}\n", source.index("\nNGINX\n", start)) + 2]
        with tempfile.TemporaryDirectory() as directory:
            script = '\n'.join([
                'set -euo pipefail', 'log() { :; }', 'fail() { echo "$*" >&2; exit 1; }',
                f'ARTIFACT_DIR="{directory}"', 'VERIFY_GW_PORT=1', 'VERIFY_H5_PORT=2',
                'VERIFY_PLATFORM=linux/amd64', 'VERIFY_IMAGE_PREFIX=pfx', 'PROJECT_NAME=proj',
                'ensure_runtime_image() { :; }', 'wait_health() { :; }',
                'docker() { case "$1" in create) echo cid ;; cp|rm|run) : ;; *) return 1 ;; esac; }',
                body,
                'start_h5_nginx proj 33967',
            ])
            result = subprocess.run(["bash", "-c", script], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            static = next(Path(directory).glob("h5-*"))
            self.assertEqual(static.stat().st_mode & 0o777, 0o755)

    def test_target_log_match_survives_a_log_larger_than_the_pipe_buffer(self):
        # Regression from sg-all-1021-final11: `docker logs ... | grep -q ...`
        # under `set -o pipefail` reports a FALSE failure once the log outgrows
        # the pipe buffer — grep -q exits at the early match, the still-writing
        # docker logs dies of SIGPIPE (141), and pipefail returns that. The
        # recovery drill's registry-hub check lost exactly this race while the
        # line was present in the target's log.
        source = SOURCE.parent / "backup-restore-drill.sh"
        stub = ('docker() { if [ "$1" = logs ]; then '
                'i=0; while [ "$i" -lt 20000 ]; do echo "startup line $i"; i=$((i+1)); done; '
                'echo "On-chain content registry enabled (durable intents)"; '
                'i=0; while [ "$i" -lt 20000 ]; do echo "later line $i"; i=$((i+1)); done; '
                'fi; }')
        old_form = '\n'.join(['set -euo pipefail', stub,
                              'docker logs target 2>&1 | grep -q "On-chain content registry enabled"'])
        self.assertNotEqual(subprocess.run(["bash", "-c", old_form], capture_output=True).returncode, 0)
        fixed = '\n'.join(['set -euo pipefail', stub, function("target_log_reports", source),
                           'target_log_reports target "On-chain content registry enabled"'])
        self.assertEqual(subprocess.run(["bash", "-c", fixed], capture_output=True).returncode, 0)
        missing = '\n'.join(['set -euo pipefail', stub, function("target_log_reports", source),
                             'target_log_reports target "never logged"'])
        self.assertNotEqual(subprocess.run(["bash", "-c", missing], capture_output=True).returncode, 0)

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

    # The exact sg-all-1019-f perf-smoke playback.log: the browser died with a
    # missing shared library, the harness ran its temporary-directories cleanup
    # SUCCESSFULLY, and the old blind `tail -c 200` reported the truncated tail
    # of that success ("finished ... cleanup" -> "shed temporary directories
    # cleanup") as if cleanup were the failure.
    PLAYBACK_LOG = "\n".join([
        "browserType.launch: Target page, context or browser has been closed",
        "Browser logs:",
        "<launched> pid=9381",
        "[pid=9381][err] /x/chrome-headless-shell: error while loading shared libraries: "
        "libnspr4.so: cannot open shared object file: No such file or directory",
        "Call log:",
        "  - [pid=9381] starting temporary directories cleanup",
        "  - [pid=9381] finished temporary directories cleanup",
        "  - [pid=9381] <gracefully close end>",
        "    at /src/scripts/h5-demo-acceptance/run.mjs:59:32",
    ])

    def run_playback_failure_detail(self, log):
        with tempfile.NamedTemporaryFile("w", suffix=".log", delete=False) as handle:
            handle.write(log)
        try:
            script = '\n'.join([
                'set -euo pipefail',
                function("playback_failure_detail", SOURCE.parent / "fullchain-acceptance.sh"),
                'playback_failure_detail ' + shlex.quote(handle.name),
            ])
            return subprocess.run(["bash", "-c", script], capture_output=True, text=True)
        finally:
            Path(handle.name).unlink()

    def test_playback_detail_names_the_browser_stderr_root_cause(self):
        result = self.run_playback_failure_detail(self.PLAYBACK_LOG)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("libnspr4.so", result.stdout)
        self.assertNotIn("shed temporary directories cleanup", result.stdout)

    def test_playback_detail_falls_back_to_the_report_marker(self):
        result = self.run_playback_failure_detail("runtime error: manifest denied\n✗ spec-throw: playback denied\n")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("playback denied", result.stdout)

    def test_playback_detail_survives_an_empty_log(self):
        result = self.run_playback_failure_detail("")
        self.assertEqual(result.returncode, 0, result.stderr)

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

    def test_final_all_remeasures_reference_instead_of_reusing_a_previous_run(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            cache = root / "cache/fixed-key"
            cache.mkdir(parents=True)
            (cache / "environment.json").write_text(json.dumps({"harness_revision": "a" * 40, "run": "earlier-run"}))
            script = '\n'.join([
                "set -euo pipefail", "VERIFY_STAGE=all", "PROJECT_NAME=own-run",
                "VERIFY_SOURCE_REVISION=" + "a" * 40,
                "ARTIFACT_DIR=" + shlex.quote(directory),
                "VERIFY_REFERENCE_CACHE=" + shlex.quote(str(root / "cache")),
                'log() { echo "$*"; }; fail() { exit 1; }',
                'perf_reference_key() { echo fixed-key; }',
                'python3() { if [ "$1" = scripts/cd/perf_protocol.py ]; then return 0; fi; command python3 "$@"; }',
                'bring_up_fullstack() { echo fresh-reference-started; return 17; }',
                function("perf_reference_pass"), "perf_reference_pass " + "c" * 40,
            ])
            result = subprocess.run(["bash", "-c", script], capture_output=True, text=True)
            self.assertEqual(result.returncode, 17, result.stderr)
            self.assertIn("fresh-reference-started", result.stdout)
            self.assertNotIn("REUSED", result.stdout)

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
