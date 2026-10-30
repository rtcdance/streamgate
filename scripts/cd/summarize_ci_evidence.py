#!/usr/bin/env python3
"""Bind a full Docker gate index to this CI run's scanned and published build."""
import argparse
import copy
import hashlib
import json
from pathlib import Path
import re

from assert_candidate_evidence import validate
from candidate_manifest import file_hash
from image_identity import BINDING_FIELDS
from generate_candidate_gates import candidate_file
from write_candidate_manifest import REQUIRED_SERVICES, merge_publications


REQUIRED_JOBS = {
    "lint", "test-go", "test-contracts", "test-anvil", "build", "security",
    "candidate", "docker", "durability", "ops", "perf", "test-suite", "acceptance", "manifest",
}
REQUIREMENTS = {
    "R01": (("M1", "M2"), ("pkg/gateway/routes.go",)),
    "R02": (("M1", "M3"), ("pkg/service/auth_wallet.go", "pkg/service/gating/rule.go")),
    "R03": (("M1", "M3"), ("pkg/web3/nft/verifier.go", "pkg/middleware/nft_gate.go")),
    "R04": (("M1", "M4", "M7"), ("pkg/web3/chain.go",)),
    "R05": (("M2", "M3", "M8"), ("pkg/middleware/playback_token_auth.go", "pkg/service/playback_authz.go")),
    "R06": (("M2", "M7"), ("pkg/service/upload/upload.go", "pkg/service/upload/original_seal_test.go")),
    "R07": (("M8",), ("pkg/service/transcoding/transcoding.go",)),
    "R08": (("M7",), ("pkg/web3/tx/nonce_ledger.go", "pkg/service/registry/publisher.go")),
    "R09": (("M5", "M8"), ("pkg/gateway/providers.go",)),
    "R10": (("M4",), ("test/load/harness.go", "pkg/monitoring/prometheus.go")),
    "R11": (("M0", "M5"), (".github/workflows/ci.yml", "scripts/verify-docker.sh")),
    "R12": (("M6",), ("TRANSITION_PLAN.md", "docs/resume/PROJECT_TALK_SCRIPT.md")),
}


def verify_publication(manifest, manifest_path, ci_run, ci_attempt, event, registry_dir, registry_repository):
    source = manifest.get("registryPublication", {})
    if event == "pull_request":
        if source != {"status": "NOT_RECORDED"} or registry_dir is not None or registry_repository is not None:
            raise ValueError("PRs must not claim a registry publication")
        return source, None
    expected = {"status": "RECORDED", "ciRun": ci_run, "ciAttempt": ci_attempt}
    if source == expected and registry_dir is None and registry_repository is None:
        return source, None
    attempt = source.get("ciAttempt")
    if (set(source) != {"status", "ciRun", "ciAttempt"} or source.get("status") != "RECORDED"
            or source.get("ciRun") != ci_run or not isinstance(attempt, str)
            or not re.fullmatch(r"[1-9][0-9]*", attempt) or int(attempt) > int(ci_attempt)
            or registry_dir is None or not registry_repository):
        raise ValueError("candidate publication receipts must be verified in this CI run/attempt")
    # Re-check raw receipts instead of relabelling the inherited manifest. The
    # accepted image/digest and original producer attempt must remain identical.
    images = copy.deepcopy(manifest["images"])
    merge_publications(images, registry_dir, registry_repository, ci_run, attempt)
    receipts = {}
    for service, image in images.items():
        original = manifest["images"][service]
        if any(image.get(key) != original.get(key) for key in
               ("registryDigest", "registryReference", "registryPublicationAttempt")):
            raise ValueError(f"{service}: revalidation changed the accepted registry identity")
        receipts[service] = {}
        for suffix in (".json", ".push.log", ".push.exit"):
            path = registry_dir / f"{service}{suffix}"
            if path.is_symlink() or not path.resolve().is_relative_to(registry_dir.resolve()):
                raise ValueError(f"{service}: publication evidence escapes its artifact directory")
            receipts[service][path.name] = file_hash(path)
    return expected, {"sourceManifestSHA256": file_hash(manifest_path),
                      "sourceVerification": source, "receipts": receipts}


def summarize(root, manifest_path, needs, revision, ci_run, ci_attempt, event, repository,
              registry_dir=None, registry_repository=None):
    if (not re.fullmatch(r"[0-9a-f]{40}", revision)
            or not re.fullmatch(r"[1-9][0-9]*", ci_run)
            or not re.fullmatch(r"[1-9][0-9]*", ci_attempt)
            or event not in {"pull_request", "push", "workflow_dispatch"}):
        raise ValueError("an exact candidate, CI run/attempt and supported event are required")
    if set(needs) != REQUIRED_JOBS or any(job.get("result") != "success" for job in needs.values()):
        raise ValueError("every required CI job must be present and successful; skip/cancel/failure is not proof")
    root = root.resolve()
    if (root / "command.exit").read_text().strip() != "0":
        raise ValueError("complete Docker entry did not preserve a successful raw exit")
    validate(root, repository)
    candidate = json.loads((root / "candidate.json").read_text())
    manifest = json.loads(manifest_path.read_text())
    if (manifest.get("schemaVersion") != 2 or manifest.get("candidateSHA") != revision
            or candidate.get("revision") != revision
            or manifest.get("revision") != revision
            or set(manifest.get("images", {})) != REQUIRED_SERVICES):
        raise ValueError("CI manifest and full Docker evidence do not identify this complete candidate")
    publication, publication_evidence = verify_publication(
        manifest, manifest_path, ci_run, ci_attempt, event, registry_dir, registry_repository)
    originals = candidate.get("ciBuildInputs", {}).get("images", {})
    if set(originals) != REQUIRED_SERVICES:
        raise ValueError("full Docker acceptance is not linked to all original CI build archives")
    for service, image in manifest["images"].items():
        if (any(originals[service].get(field) != image.get(field) for field in BINDING_FIELDS)
                or image.get("revision") != revision or image.get("platform") != manifest.get("platform")
                or image.get("buildTags") != ["nats_integration"]):
            raise ValueError(f"{service}: acceptance did not consume this CI build/archive")
        digest = image.get("registryDigest")
        if event == "pull_request":
            if digest is not None:
                raise ValueError("PR image has an unverified publication digest")
        elif not re.fullmatch(r"sha256:[0-9a-f]{64}", str(digest)):
            raise ValueError(f"{service}: missing immutable registry publication identity")
        runtime = candidate["images"].get(service)
        if runtime is not None and (runtime["imageID"] != image["imageID"]
                                    or runtime.get("buildArchiveSHA256") != image["archiveSHA256"]):
            raise ValueError(f"{service}: tested runtime image/archive differs from the scanned build")
    index = json.loads((root / "candidate-gates.json").read_text())
    gates = {gate["id"]: gate for gate in index["gates"]}
    requirements = {}
    for requirement, (gate_ids, files) in REQUIREMENTS.items():
        requirements[requirement] = {
            "candidateSHA": revision, "gates": list(gate_ids),
            "code": [{"path": path, "sha256": hashlib.sha256(candidate_file(repository, revision, path)).hexdigest()}
                     for path in files],
            "scenarios": sorted({scenario for gid in gate_ids for scenario in gates[gid]["scenarios"]}),
            "artifacts": sorted({artifact["path"] for gid in gate_ids for artifact in gates[gid]["artifacts"]}),
        }
    return {
        "schemaVersion": 1, "candidateSHA": revision, "ciRun": ci_run, "ciAttempt": ci_attempt,
        "status": "REQUIRED_GATES_PASS", "rawExitCode": 0, "event": event,
        "jobResults": {key: needs[key]["result"] for key in sorted(needs)},
        "gates": [gate["id"] for gate in index["gates"]],
        "candidateManifestSHA256": file_hash(manifest_path),
        "dockerGateIndexSHA256": file_hash(root / "candidate-gates.json"),
        "registryPublication": publication, "registryPublicationEvidence": publication_evidence,
        "images": manifest["images"],
        "requirements": requirements,
        "limitations": ["Human demonstration and production deployment remain outside this gate."],
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("artifact_root", type=Path)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--needs", type=Path, required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--ci-run", required=True)
    parser.add_argument("--ci-attempt", required=True)
    parser.add_argument("--event", required=True)
    parser.add_argument("--registry-dir", type=Path)
    parser.add_argument("--registry-repository")
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    result = summarize(args.artifact_root, args.manifest, json.loads(args.needs.read_text()),
                       args.revision, args.ci_run, args.ci_attempt, args.event, Path.cwd(),
                       registry_dir=args.registry_dir, registry_repository=args.registry_repository)
    args.out.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(f"M0-M8 and required upstream CI jobs bound to candidate {args.revision}, run {args.ci_run}/{args.ci_attempt}")


if __name__ == "__main__":
    main()
