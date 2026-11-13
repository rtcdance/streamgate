from pathlib import Path
import hashlib
import io
import json
import os
import re
import subprocess
import sys
import tarfile
import tempfile
import textwrap
import unittest

sys.path.insert(0, str(Path(__file__).parent))
from image_identity import archive_digests, identity, push_digest, verify_identity


class CIManifestTests(unittest.TestCase):
    SERVICES = ("monolith", "transcoder", "api-gateway", "auth", "cache", "metadata", "monitor", "streaming", "upload", "worker")

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.meta = self.root / "meta"
        self.meta.mkdir()
        self.revision = "a" * 40
        for service in self.SERVICES:
            archive = self.root / f"candidate-{service}.tar"
            archive.write_bytes(f"archive fixture for {service}".encode())
            image_id = "sha256:" + hashlib.sha256(service.encode()).hexdigest()
            data = {
                "imageID": image_id, "archiveSHA256": hashlib.sha256(archive.read_bytes()).hexdigest(),
                "registryDigest": None, "buildTags": ["nats_integration"], "platform": "linux/amd64",
                "revision": self.revision, "image_id": image_id, "commit": self.revision,
            }
            (self.meta / f"{service}.json").write_text(json.dumps(data))

    def invoke(self, *, tags="nats_integration", platform="linux/amd64", registry_dir=None, ci_attempt="1"):
        command = [
            sys.executable, str(Path(__file__).with_name("write_candidate_manifest.py")),
            "--meta-dir", str(self.meta), "--sha", self.revision, "--build-tags", tags,
            "--platform", platform, "--out", str(self.root / "manifest.json"),
        ]
        if registry_dir is not None:
            command += ["--registry-dir", str(registry_dir), "--ci-run", "12345", "--ci-attempt", ci_attempt,
                        "--registry-repository", "ghcr.io/example/streamgate"]
        return subprocess.run(command, capture_output=True, text=True)

    def write_receipts(self):
        directory = self.root / "registry-publications"
        directory.mkdir()
        for service in self.SERVICES:
            digest = "sha256:" + hashlib.sha256(("registry:" + service).encode()).hexdigest()
            log = directory / f"{service}.push.log"
            log.write_text(f"candidate: digest: {digest} size: 1234\n")
            (directory / f"{service}.push.exit").write_text("0\n")
            original = json.loads((self.meta / f"{service}.json").read_text())
            receipt = {key: original[key] for key in ("imageID", "archiveSHA256", "revision", "buildTags", "platform")}
            receipt.update({
                "schemaVersion": 1, "service": service, "candidateSHA": self.revision,
                "registryDigest": digest, "registryReference": f"ghcr.io/example/streamgate/{service}@{digest}",
                "ciRun": "12345", "ciAttempt": "1", "pushExitCode": 0,
                "pushLog": log.name, "pushLogSHA256": hashlib.sha256(log.read_bytes()).hexdigest(),
            })
            (directory / f"{service}.json").write_text(json.dumps(receipt))
        return directory

    def change(self, **updates):
        path = self.meta / "monolith.json"
        data = json.loads(path.read_text())
        data.update(updates)
        path.write_text(json.dumps(data))

    def assert_receipt_rejected(self, directory):
        result = self.invoke(registry_dir=directory)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("registry publication", result.stderr.lower())

    def run_retention_push(self, *, image_id=None, push_exit=0, tampered_archive=False):
        metadata = json.loads((self.meta / "monolith.json").read_text())
        inspected = self.root / "inspect.json"
        inspected.write_text(json.dumps([{
            "Id": image_id or metadata["imageID"], "Config": {"Labels": {
                "org.opencontainers.image.revision": self.revision,
                "com.streamgate.build.tags": "nats_integration",
            }}, "Os": "linux", "Architecture": "amd64",
        }]))
        binary_dir = self.root / "bin"
        binary_dir.mkdir()
        docker = binary_dir / "docker"
        docker.write_text('''#!/bin/sh
case "$1" in
  load|tag) exit 0 ;;
  image) cat "$SG_TEST_INSPECT" ;;
  push) echo attempted >> "$SG_TEST_PUSH_MARKER"; cat "$SG_TEST_PUSH_LOG"; exit "$SG_TEST_PUSH_EXIT" ;;
  *) exit 125 ;;
esac
''')
        docker.chmod(0o755)
        source = (Path(__file__).resolve().parents[2] / ".github/workflows/build.yml").read_text()
        push_function = textwrap.dedent(re.search(r"^          push_one\(\).*?^          \}", source, re.M | re.S)[0])
        (self.root / "scripts").symlink_to(Path(__file__).resolve().parents[1], target_is_directory=True)
        publications = self.root / "registry-publications"
        publications.mkdir()
        output = self.root / "github-output"
        output.write_text("")
        push_log = self.root / "docker-push.log"
        push_log.write_text(f"candidate: digest: sha256:{'3' * 64} size: 1234\n")
        if tampered_archive:
            (self.root / "candidate-monolith.tar").write_bytes(b"different archive bytes")
        env = {**os.environ, "PATH": str(binary_dir) + os.pathsep + os.environ["PATH"],
               "SG_TEST_INSPECT": str(inspected), "SG_TEST_PUSH_MARKER": str(self.root / "push-called"),
               "SG_TEST_PUSH_LOG": str(push_log), "SG_TEST_PUSH_EXIT": str(push_exit),
               "GITHUB_OUTPUT": str(output), "GITHUB_RUN_ID": "12345", "GITHUB_RUN_ATTEMPT": "1"}
        script = '\n'.join(["set -euo pipefail", "repo=ghcr.io/example/streamgate", f"sha={self.revision}",
                            push_function, "push_one streamgate-monolith:candidate monolith monolith_digest"])
        return subprocess.run(["bash", "-c", script], cwd=self.root, env=env, capture_output=True, text=True, timeout=30)

    def test_ci_manifest_keeps_all_identity_types_and_build_facts(self):
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        document = json.loads((self.root / "manifest.json").read_text())
        self.assertEqual(document["schemaVersion"], 2)
        self.assertEqual(set(document["images"]), set(self.SERVICES))
        self.assertEqual(document["buildTags"], ["nats_integration"])
        for service, image in document["images"].items():
            original = json.loads((self.meta / f"{service}.json").read_text())
            for field in ("imageID", "archiveSHA256", "registryDigest", "revision", "buildTags", "platform"):
                self.assertEqual(image[field], original[field])
            self.assertNotIn("imageDigest", image)

    def test_ci_manifest_rejects_missing_service_metadata(self):
        (self.meta / "auth.json").unlink()
        self.assertNotEqual(self.invoke().returncode, 0)

    def test_ci_manifest_rejects_changed_or_missing_archive(self):
        archive = self.root / "candidate-monolith.tar"
        archive.write_bytes(b"changed bytes")
        self.assertNotEqual(self.invoke().returncode, 0)
        archive.unlink()
        self.assertNotEqual(self.invoke().returncode, 0)

    def test_ci_manifest_requires_explicit_registry_publication_status(self):
        path = self.meta / "monolith.json"
        data = json.loads(path.read_text())
        del data["registryDigest"]
        path.write_text(json.dumps(data))
        self.assertNotEqual(self.invoke().returncode, 0)

    def test_ci_manifest_rejects_mixed_revisions(self):
        self.change(revision="b" * 40, commit="b" * 40)
        self.assertNotEqual(self.invoke().returncode, 0)

    def test_ci_manifest_rejects_actual_tags_or_platform_mismatch(self):
        self.change(buildTags=["wrong-tag"])
        self.assertNotEqual(self.invoke().returncode, 0)
        self.change(buildTags=["nats_integration"], platform="linux/arm64")
        self.assertNotEqual(self.invoke().returncode, 0)

    def test_ci_manifest_rejects_conflicting_legacy_aliases(self):
        self.change(image_id="sha256:" + "f" * 64)
        self.assertNotEqual(self.invoke().returncode, 0)

    def test_ci_manifest_rejects_an_unexpected_service(self):
        (self.meta / "unexpected.json").write_text((self.meta / "monolith.json").read_text())
        self.assertNotEqual(self.invoke().returncode, 0)

    def test_ci_manifest_merges_only_bound_registry_receipts(self):
        receipts = self.write_receipts()
        result = self.invoke(registry_dir=receipts)
        self.assertEqual(result.returncode, 0, result.stderr)
        document = json.loads((self.root / "manifest.json").read_text())
        self.assertEqual(document["registryPublication"]["status"], "RECORDED")
        for service, image in document["images"].items():
            receipt = json.loads((receipts / f"{service}.json").read_text())
            self.assertEqual(image["registryDigest"], receipt["registryDigest"])
            self.assertEqual(image["registryReference"], receipt["registryReference"])
            self.assertNotEqual(image["registryDigest"], image["imageID"])

    def test_same_run_rerun_can_reuse_its_exact_build_publication(self):
        receipts = self.write_receipts()
        result = self.invoke(registry_dir=receipts, ci_attempt="2")
        self.assertEqual(result.returncode, 0, result.stderr)
        document = json.loads((self.root / "manifest.json").read_text())
        self.assertEqual(document["images"]["monolith"]["registryPublicationAttempt"], "1")

    def test_ci_manifest_rejects_a_missing_publication_receipt_or_raw_log(self):
        receipts = self.write_receipts()
        for name in ("auth.json", "monolith.push.log", "monolith.push.exit"):
            path = receipts / name
            original = path.read_bytes()
            path.unlink()
            with self.subTest(name=name):
                self.assert_receipt_rejected(receipts)
            path.write_bytes(original)

    def test_ci_manifest_rejects_cross_run_or_mismatched_registry_receipts(self):
        receipts = self.write_receipts()
        path = receipts / "monolith.json"
        original = json.loads(path.read_text())
        for field, value in (("imageID", "sha256:" + "f" * 64), ("archiveSHA256", "e" * 64),
                             ("revision", "b" * 40), ("ciRun", "12344"), ("ciAttempt", "2"),
                             ("registryReference", "ghcr.io/wrong/monolith@" + original["registryDigest"]),
                             ("pushExitCode", 1)):
            path.write_text(json.dumps({**original, field: value}))
            with self.subTest(field=field):
                self.assert_receipt_rejected(receipts)
        path.write_text(json.dumps(original))

    def test_ci_manifest_rejects_tampered_or_ambiguous_push_output(self):
        receipts = self.write_receipts()
        log = receipts / "monolith.push.log"
        log.write_text("not a successful registry push\n")
        self.assert_receipt_rejected(receipts)
        receipt_path = receipts / "monolith.json"
        receipt = json.loads(receipt_path.read_text())
        receipt["pushLogSHA256"] = hashlib.sha256(log.read_bytes()).hexdigest()
        receipt_path.write_text(json.dumps(receipt))
        self.assert_receipt_rejected(receipts)
        log.write_text(f"digest: {receipt['registryDigest']} size: 1234\ndigest: sha256:{'f' * 64} size: 456\n")
        receipt["pushLogSHA256"] = hashlib.sha256(log.read_bytes()).hexdigest()
        receipt_path.write_text(json.dumps(receipt))
        self.assert_receipt_rejected(receipts)

    def test_registry_workflow_records_publication_without_rewriting_build_metadata(self):
        before = (self.meta / "monolith.json").read_bytes()
        result = self.run_retention_push()
        self.assertEqual(result.returncode, 0, result.stderr)
        receipt = json.loads((self.root / "registry-publications/monolith.json").read_text())
        self.assertEqual(receipt["registryDigest"], "sha256:" + "3" * 64)
        self.assertEqual(receipt["ciRun"], "12345")
        self.assertEqual((self.meta / "monolith.json").read_bytes(), before)
        self.assertIn("monolith_digest=" + receipt["registryDigest"], (self.root / "github-output").read_text())

    def test_registry_workflow_rejects_a_substituted_image_before_push(self):
        result = self.run_retention_push(image_id="sha256:" + "f" * 64)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "push-called").exists())

    def test_registry_workflow_rejects_a_changed_archive_before_push(self):
        result = self.run_retention_push(tampered_archive=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "push-called").exists())

    def test_registry_workflow_preserves_failed_push_exit_and_never_emits_a_receipt(self):
        result = self.run_retention_push(push_exit=42)
        self.assertEqual(result.returncode, 42, result.stderr)
        self.assertEqual((self.root / "registry-publications/monolith.push.exit").read_text().strip(), "42")
        self.assertFalse((self.root / "registry-publications/monolith.json").exists())
        self.assertEqual((self.root / "github-output").read_text(), "")

    def test_scan_steps_bind_archive_across_docker_stores_and_reject_substitution(self):
        source = (Path(__file__).resolve().parents[2] / ".github/workflows/build.yml").read_text()
        steps = re.findall(r"^      - name: Load candidate and verify identity\n(?:        id: load\n)?        run: \|\n((?:          .*\n|\n)+)", source, re.M)
        self.assertEqual(len(steps), 3)
        (self.root / "scripts").symlink_to(Path(__file__).resolve().parents[1], target_is_directory=True)
        binary_dir = self.root / "bin"
        binary_dir.mkdir()
        docker = binary_dir / "docker"
        docker.write_text('''#!/bin/sh
case "$1" in
  load) exit 0 ;;
  image) cat "$SG_TEST_INSPECT" ;;
  inspect) python3 -c 'import json,os; print(json.load(open(os.environ["SG_TEST_INSPECT"]))[0]["Id"])' ;;
  *) exit 125 ;;
esac
''')
        docker.chmod(0o755)
        inspected = self.root / "inspect.json"
        env = {**os.environ, "PATH": str(binary_dir) + os.pathsep + os.environ["PATH"], "SG_TEST_INSPECT": str(inspected)}
        config, manifest = "sha256:" + "a" * 64, "sha256:" + "b" * 64
        for service, step in zip(("monolith", "transcoder", "auth"), steps):
            with self.subTest(service=service):
                archive = self.root / f"candidate-{service}.tar"
                with tarfile.open(archive, "w") as tar:
                    for name, document in {
                        "manifest.json": [{"Config": f"blobs/sha256/{config[7:]}", "RepoTags": [f"streamgate-{service}:candidate"]}],
                        "index.json": {"manifests": [{"digest": manifest, "annotations": {"io.containerd.image.name": f"docker.io/library/streamgate-{service}:candidate"}}]},
                    }.items():
                        payload = json.dumps(document).encode()
                        entry = tarfile.TarInfo(name); entry.size = len(payload)
                        tar.addfile(entry, io.BytesIO(payload))
                info = {"Id": manifest, "Config": {"Labels": {"org.opencontainers.image.revision": self.revision,
                        "com.streamgate.build.tags": "nats_integration"}}, "Os": "linux", "Architecture": "amd64"}
                metadata = identity({**info, "Id": config}, self.revision, hashlib.sha256(archive.read_bytes()).hexdigest(), (config, manifest))
                (self.meta / f"{service}.json").write_text(json.dumps(metadata))
                script = textwrap.dedent(step).replace("${{ github.sha }}", self.revision).replace("${{ matrix.service }}", service)
                inspected.write_text(json.dumps([info]))
                result = subprocess.run(["bash", "-c", script], cwd=self.root, env=env, capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                inspected.write_text(json.dumps([{**info, "Id": "sha256:" + "c" * 64}]))
                result = subprocess.run(["bash", "-c", script], cwd=self.root, env=env, capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                inspected.write_text(json.dumps([info]))
                with archive.open("ab") as stream:
                    stream.write(b"tampered")
                result = subprocess.run(["bash", "-c", script], cwd=self.root, env=env, capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)

    def test_smoke_step_binds_archive_across_docker_stores_and_rejects_substitution(self):
        # The smoke job runs on whichever store its runner has: a classic store
        # reports the config digest as the image ID, a containerd store the OCI
        # manifest digest. It must accept both, and still reject a substituted
        # image or a tampered archive.
        source = (Path(__file__).resolve().parents[2] / ".github/workflows/ci.yml").read_text()
        steps = re.findall(r"^      - name: Load candidate and verify its identity\n        run: \|\n((?:          .*\n|\n)+)", source, re.M)
        self.assertEqual(len(steps), 1)
        (self.root / "scripts").symlink_to(Path(__file__).resolve().parents[1], target_is_directory=True)
        binary_dir = self.root / "bin"
        binary_dir.mkdir()
        docker = binary_dir / "docker"
        docker.write_text('''#!/bin/sh
case "$1" in
  load) exit 0 ;;
  image) cat "$SG_TEST_INSPECT" ;;
  inspect) python3 -c 'import json,os; print(json.load(open(os.environ["SG_TEST_INSPECT"]))[0]["Id"])' ;;
  tag) exit 0 ;;
  *) exit 125 ;;
esac
''')
        docker.chmod(0o755)
        inspected = self.root / "inspect.json"
        env = {**os.environ, "PATH": str(binary_dir) + os.pathsep + os.environ["PATH"], "SG_TEST_INSPECT": str(inspected)}
        config, manifest = "sha256:" + "a" * 64, "sha256:" + "b" * 64
        archive = self.root / "candidate-monolith.tar"
        with tarfile.open(archive, "w") as tar:
            for name, document in {
                "manifest.json": [{"Config": f"blobs/sha256/{config[7:]}", "RepoTags": ["streamgate-monolith:candidate"]}],
                "index.json": {"manifests": [{"digest": manifest, "annotations": {"io.containerd.image.name": "docker.io/library/streamgate-monolith:candidate"}}]},
            }.items():
                payload = json.dumps(document).encode()
                entry = tarfile.TarInfo(name); entry.size = len(payload)
                tar.addfile(entry, io.BytesIO(payload))
        info = {"Id": manifest, "Config": {"Labels": {"org.opencontainers.image.revision": self.revision,
                "com.streamgate.build.tags": "nats_integration"}}, "Os": "linux", "Architecture": "amd64"}
        (self.meta / "monolith.json").write_text(json.dumps(identity(
            {**info, "Id": config}, self.revision, hashlib.sha256(archive.read_bytes()).hexdigest(), (config, manifest))))
        script = textwrap.dedent(steps[0]).replace("${{ github.sha }}", self.revision)
        inspected.write_text(json.dumps([info]))
        result = subprocess.run(["bash", "-c", script], cwd=self.root, env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        inspected.write_text(json.dumps([{**info, "Id": "sha256:" + "c" * 64}]))
        result = subprocess.run(["bash", "-c", script], cwd=self.root, env=env, capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        inspected.write_text(json.dumps([info]))
        with archive.open("ab") as stream:
            stream.write(b"tampered")
        result = subprocess.run(["bash", "-c", script], cwd=self.root, env=env, capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)


class ImageIdentityTests(unittest.TestCase):
    def setUp(self):
        self.info = {
            "Id": "sha256:image-config", "RepoDigests": ["registry/image@sha256:registry-manifest"],
            "Os": "linux", "Architecture": "amd64",
            "Config": {"Labels": {"org.opencontainers.image.revision": "revision", "com.streamgate.build.tags": "nats_integration"}},
        }

    def test_digest_types_stay_distinct(self):
        result = identity(self.info, "revision", "archive-hash")
        self.assertEqual(result["imageID"], "sha256:image-config")
        self.assertEqual(result["archiveSHA256"], "archive-hash")
        self.assertIsNone(result["registryDigest"])
        self.assertEqual(result["dockerRepoDigests"], ["registry/image@sha256:registry-manifest"])
        self.assertEqual(result["platform"], "linux/amd64")
        self.assertEqual(result["buildTags"], ["nats_integration"])

    def write_archive(self, directory, documents, name="candidate-monolith.tar"):
        archive = Path(directory) / name
        with tarfile.open(archive, "w") as tar:
            for entry, document in documents.items():
                payload = json.dumps(document).encode()
                header = tarfile.TarInfo(entry)
                header.size = len(payload)
                tar.addfile(header, io.BytesIO(payload))
        return archive

    def test_containerd_store_id_binds_through_the_archive_config_digest(self):
        # The classic store reports the config digest as the image ID, the
        # containerd store reports the OCI manifest digest. One scanned archive
        # must still bind both, or a containerd acceptance host can never prove
        # it loaded the image CI built and scanned.
        config, manifest = "sha256:" + "a" * 64, "sha256:" + "b" * 64
        with tempfile.TemporaryDirectory() as directory:
            archive = self.write_archive(directory, {
                "manifest.json": [{"Config": f"blobs/sha256/{config[7:]}",
                                   "RepoTags": ["streamgate-monolith:candidate"]}],
                "index.json": {"manifests": [{"digest": manifest, "annotations": {
                    "io.containerd.image.name": "docker.io/library/streamgate-monolith:candidate"}}]},
            })
            digests = archive_digests(archive)
            self.assertEqual(digests, (config, manifest))
            build = identity({**self.info, "Id": config}, "revision", "archive-hash", digests)
            loaded = identity({**self.info, "Id": manifest}, "revision", "archive-hash", digests)
            verify_identity(build, loaded)
            self.assertEqual(build["imageID"], config)
            self.assertEqual(loaded["imageID"], config)
            self.assertEqual(loaded["daemonImageID"], manifest)
            with self.assertRaisesRegex(ValueError, "not a digest"):
                identity({**self.info, "Id": "sha256:" + "c" * 64}, "revision", "archive-hash", digests)

    def test_unannotated_single_manifest_index_still_binds_the_manifest_digest(self):
        config, manifest = "sha256:" + "e" * 64, "sha256:" + "f" * 64
        with tempfile.TemporaryDirectory() as directory:
            archive = self.write_archive(directory, {
                "manifest.json": [{"Config": f"blobs/sha256/{config[7:]}",
                                   "RepoTags": ["streamgate-monolith:candidate"]}],
                "index.json": {"manifests": [{"digest": manifest}]},
            })
            self.assertEqual(archive_digests(archive), (config, manifest))
            build = identity({**self.info, "Id": config}, "revision", "archive-hash", archive_digests(archive))
            loaded = identity({**self.info, "Id": manifest}, "revision", "archive-hash", archive_digests(archive))
            verify_identity(build, loaded)

    def test_ambiguous_unannotated_index_does_not_guess_a_manifest(self):
        config = "sha256:" + "e" * 64
        with tempfile.TemporaryDirectory() as directory:
            archive = self.write_archive(directory, {
                "manifest.json": [{"Config": f"blobs/sha256/{config[7:]}",
                                   "RepoTags": ["streamgate-monolith:candidate"]}],
                "index.json": {"manifests": [{"digest": "sha256:" + "1" * 64},
                                             {"digest": "sha256:" + "2" * 64}]},
            })
            self.assertEqual(archive_digests(archive), (config, None))

    def test_workflows_never_compare_a_raw_store_image_id(self):
        # A classic store reports the config digest, a containerd store the
        # manifest digest; hosted runners drift between them (run 43359158354).
        # Every load-and-verify step must bind through the archive instead.
        root = Path(__file__).resolve().parents[2] / ".github/workflows"
        for workflow in ("build.yml", "ci.yml"):
            source = (root / workflow).read_text(encoding="utf-8")
            with self.subTest(workflow=workflow):
                self.assertNotRegex(source, r"\['image_id'\]")
                for step in re.findall(r"docker load -i (candidate-[^\s]+\.tar)\n(.*?)(?=\n\s*\n|\n\s+- )", source, re.S):
                    self.assertIn("image_identity.py", step[1], step[0])

    def test_classic_store_archive_reports_only_the_config_digest(self):
        config = "sha256:" + "d" * 64
        with tempfile.TemporaryDirectory() as directory:
            archive = self.write_archive(directory, {"manifest.json": [
                {"Config": f"{config[7:]}.json", "RepoTags": ["streamgate-monolith:candidate"]}]})
            self.assertEqual(archive_digests(archive), (config, None))
            self.assertEqual(identity({**self.info, "Id": config}, "revision", "archive-hash",
                                      archive_digests(archive))["imageID"], config)

    def test_non_archive_files_keep_the_store_id(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory) / "candidate-monolith.tar"
            fixture.write_bytes(b"not a tar")
            self.assertIsNone(archive_digests(fixture))

    def test_wrong_revision_fails(self):
        with self.assertRaisesRegex(ValueError, "revision"):
            identity(self.info, "wrong", "archive-hash")

    def test_missing_build_tags_fail(self):
        del self.info["Config"]["Labels"]["com.streamgate.build.tags"]
        with self.assertRaisesRegex(ValueError, "build tags"):
            identity(self.info, "revision", "archive-hash")

    def test_repo_digest_is_not_proof_of_a_registry_publication(self):
        self.info["RepoDigests"] = ["local-only@sha256:local-manifest"]
        result = identity(self.info, "revision", "archive-hash")
        self.assertIsNone(result["registryDigest"])

    def test_verify_rejects_changed_image_archive_or_build_facts(self):
        observed = identity(self.info, "revision", "archive-hash")
        verify_identity(observed, observed)
        for field, value in (("imageID", "different"), ("archiveSHA256", "different"),
                             ("revision", "different"), ("platform", "linux/arm64"),
                             ("buildTags", ["wrong"]), ("registryDigest", "not-a-publication")):
            expected = {**observed, field: value}
            with self.subTest(field=field), self.assertRaises(ValueError):
                verify_identity(expected, observed)

    def test_push_digest_requires_one_unambiguous_registry_digest(self):
        digest = "sha256:" + "a" * 64
        self.assertEqual(push_digest(f"tag: digest: {digest} size: 1234\n"), digest)
        for output in ("push failed", f"digest: {digest}\ndigest: sha256:{'b' * 64}\n", "digest: sha256:short size: 5"):
            with self.subTest(output=output), self.assertRaises(ValueError):
                push_digest(output)

    def test_verify_cli_preserves_build_record_and_emits_bound_publication(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            revision = "a" * 40
            info = json.loads(json.dumps(self.info))
            info["Id"] = "sha256:" + "1" * 64
            info["Config"]["Labels"]["org.opencontainers.image.revision"] = revision
            inspected = root / "inspect.json"
            inspected.write_text(json.dumps([info]))
            binary = root / "docker"
            binary.write_text('#!/bin/sh\ncat "$SG_TEST_INSPECT"\n')
            binary.chmod(0o755)
            archive = root / "candidate-monolith.tar"
            archive.write_bytes(b"original archive fixture")
            record = root / "monolith.json"
            value = identity(info, revision, hashlib.sha256(archive.read_bytes()).hexdigest())
            record.write_text(json.dumps({**value, "imageID": "sha256:" + "2" * 64}))
            command = [sys.executable, str(Path(__file__).with_name("image_identity.py")),
                       "--tag", "candidate", "--archive", str(archive), "--revision", revision,
                       "--out", str(record), "--verify"]
            env = {**os.environ, "PATH": str(root) + os.pathsep + os.environ["PATH"], "SG_TEST_INSPECT": str(inspected)}
            original = record.read_bytes()
            bad = subprocess.run(command, env=env, capture_output=True, text=True)
            self.assertNotEqual(bad.returncode, 0)
            self.assertEqual(record.read_bytes(), original, "verification must not replace a mismatched build record")
            record.write_text(json.dumps(value))
            original = record.read_bytes()
            digest = "sha256:" + "3" * 64
            log = root / "monolith.push.log"
            log.write_text(f"candidate: digest: {digest} size: 1234\n")
            (root / "monolith.push.exit").write_text("0\n")
            publication = root / "publication.json"
            publish = command + ["--publication-log", str(log), "--publication-out", str(publication),
                                 "--service", "monolith", "--registry-repository", "ghcr.io/example/streamgate",
                                 "--ci-run", "12345", "--ci-attempt", "1"]
            result = subprocess.run(publish, env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(record.read_bytes(), original)
            receipt = json.loads(publication.read_text())
            self.assertEqual(receipt["registryDigest"], digest)
            self.assertEqual(receipt["imageID"], info["Id"])
            self.assertEqual(receipt["archiveSHA256"], value["archiveSHA256"])
            self.assertEqual(receipt["pushLogSHA256"], hashlib.sha256(log.read_bytes()).hexdigest())
            self.assertEqual(receipt["ciRun"], "12345")
            (root / "monolith.push.exit").write_text("1\n")
            publication.unlink()
            failed = subprocess.run(publish, env=env, capture_output=True, text=True)
            self.assertNotEqual(failed.returncode, 0)
            self.assertFalse(publication.exists(), "a failed push cannot create a publication receipt")


if __name__ == "__main__":
    unittest.main()
