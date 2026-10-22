#!/usr/bin/env python3
"""Derive a local candidate gate index from this run's immutable raw evidence."""
import argparse
import hashlib
import json
import re
import subprocess
import sys
from pathlib import Path

GATES = {
    "M0": ("unit", "chain", "durability", "ops", "faults", "media", "recovery-seed", "recovery", "perf", "release-smoke", "release-compat", "release-drills"),
    "M1": ("chain", "durability", "faults", "media", "release-smoke"),
    "M2": ("media", "release-smoke"),
    "M3": ("durability", "media", "release-smoke"),
    "M4": ("perf",),
    "M5": ("release-smoke", "release-compat", "release-drills"),
    "M6": (),
    "M7": ("chain", "durability", "faults"),
    "M8": ("durability", "ops", "recovery-seed", "recovery", "release-smoke", "release-compat"),
}
STAGE_LOGS = {
    "unit": "logs/unit.log", "chain": "logs/chain.log",
    "durability": "logs/durability.log", "ops": "logs/ops.log",
    "faults": "logs/faults.log", "media": "logs/media.log",
    "recovery-seed": "logs/recovery-seed.log", "recovery": "logs/recovery.log",
    "perf": "logs/perf.log", "release-smoke": "logs/release-smoke.log",
    "release-compat": "logs/release-compat.log",
    "release-drills": "logs/release-drills.log",
}
M3_MARKERS = (
    "M3_HTTP_TRANSFER_70S_PASS", "M3_GRPC_TRANSFER_70S_PASS",
    "M3_HTTP_LOGOUT_CAPABILITY_5S_PASS", "M3_HTTP_LOGOUT_SESSION_5S_PASS",
    "M3_HTTP_RULE_REVOCATION_5S_PASS", "M3_HTTP_RULE_CAPABILITY_5S_PASS",
    "M3_GRPC_LOGOUT_5S_PASS", "M3_GRPC_RULE_REVOCATION_5S_PASS",
)
M7_MARKERS = ("M7_LEGACY_EMPTY_ANCHOR_REJECTED", "M7_ROLLBACK_CRASH_RECOVERY_PASS")
M7_TESTS = (
    ("TestAcceptance_EventRejectsLegacyEmptyCursorAnchor", M7_MARKERS[0]),
    ("TestAcceptance_EventRollbackMutationIsAtomicAcrossCrashBoundary", M7_MARKERS[1]),
)
M7_PACKAGE = "github.com/rtcdance/streamgate/pkg/web3/event"
M5_DRILL_MARKERS = tuple(
    marker
    for kind in ("START", "HEALTH", "SMOKE")
    for marker in (f"RELEASE_DRILL_{kind}_B_FAILED", f"RELEASE_DRILL_{kind}_A_RECOVERED")
)
DOCS = (
    "docs/resume/RESUME_CN.md", "docs/resume/RESUME_EN.md",
    "docs/resume/PROJECT_TALK_SCRIPT.md",
)
PLAN = "TRANSITION_PLAN.md"
PLAN_STATUS_MARKER = re.compile(r"<!--\s*streamgate-acceptance-status:\s*(incomplete|complete)\s*-->")


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def checked_artifact(root, relative):
    path = (root / relative).resolve()
    if not path.is_relative_to(root):
        raise ValueError(f"artifact escapes candidate directory: {relative}")
    if not path.is_file() or path.stat().st_size == 0:
        raise ValueError(f"required raw evidence is absent or empty: {relative}")
    return {"path": relative, "sha256": sha256(path)}


def tracked(repository, relative):
    result = subprocess.run(
        ["git", "-C", str(repository), "ls-files", "--error-unmatch", "--", relative],
        text=True, capture_output=True, check=False,
    )
    if result.returncode or result.stdout.strip() != relative:
        raise ValueError(f"required M6 material is not tracked at candidate SHA: {relative}")


def candidate_file(repository, revision, relative):
    result = subprocess.run(
        ["git", "-C", str(repository), "show", f"{revision}:{relative}"],
        capture_output=True, check=False,
    )
    if result.returncode:
        raise ValueError(f"required M6 material is absent from candidate tree: {relative}")
    return result.stdout


def pass_scenarios(root, paths):
    rows = []
    for relative in paths:
        content = (root / relative).read_text(errors="replace")
        rows.extend(f"{relative}: {line.strip()[6:]}" for line in content.splitlines()
                    if line.startswith("PASS: "))
    return sorted(set(rows))


def go_test_passed(path, package, test_id):
    for line in path.read_text(errors="replace").splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        if (event.get("Action") == "pass" and event.get("Package") == package
                and event.get("Test") == test_id):
            return True
    return False


def go_test_output_contains(path, package, test_id, marker):
    """Bind a Go test stdout marker to that exact test's JSON output event."""
    for line in path.read_text(errors="replace").splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        if (event.get("Action") == "output" and event.get("Package") == package
                and event.get("Test") == test_id and marker in event.get("Output", "")):
            return True
    return False


def _doc_scenarios(root, repository, revision):
    artifacts = []
    scenarios = []
    tracked(repository, PLAN)
    plan_bytes = candidate_file(repository, revision, PLAN)
    plan_text = plan_bytes.decode("utf-8", errors="replace")
    status_matches = PLAN_STATUS_MARKER.findall(plan_text[:8192])
    if len(status_matches) != 1:
        raise ValueError("TRANSITION_PLAN.md must declare exactly one current acceptance status marker near its top")
    incomplete = status_matches[0] == "incomplete"
    plan_snapshot = root / PLAN
    plan_snapshot.parent.mkdir(parents=True, exist_ok=True)
    plan_snapshot.write_bytes(plan_bytes)
    artifacts.append(checked_artifact(root, PLAN))
    scenarios.append(f"candidate plan status snapshot: {status_matches[0]}")

    for relative in DOCS:
        tracked(repository, relative)
        content_bytes = candidate_file(repository, revision, relative)
        content = content_bytes.decode("utf-8", errors="replace")
        lowered = content.lower()
        if not content.strip():
            raise ValueError(f"M6 material is empty: {relative}")
        stale_claims = (
            r"only\s+(?:the\s+)?production\s+rollout\s+remains",
            r"只剩(?:本人事项和)?生产(?:发布|部署|rollout)",
            r"当前完成候选.{0,80}ab172c2",
            r"completed candidate.{0,80}ab172c2",
            r"证据状态\s*[:：]\s*已完成并有实测",
            r"evidence status\s*:\s*completed with measured evidence",
            r"远端\s*CI\s*已运行并全绿",
            r"remote CI has run and is green",
        )
        if incomplete and any(re.search(pattern, lowered, re.IGNORECASE) for pattern in stale_claims):
            raise ValueError(f"M6 material contains a stale completion claim: {relative}")
        if not any(word in lowered for word in ("人工", "本人", "human", "personal")):
            raise ValueError(f"M6 material does not separate personal work from AI deliverables: {relative}")
        if not any(word in lowered for word in ("evidence", "证据", "transition_plan")):
            raise ValueError(f"M6 material is not traceable to the project evidence: {relative}")
        snapshot = root / relative
        snapshot.parent.mkdir(parents=True, exist_ok=True)
        snapshot.write_bytes(content_bytes)
        artifacts.append(checked_artifact(root, relative))
        scenarios.append(f"tracked and hashed candidate material: {relative}")
    return artifacts, scenarios


def stage_evidence(root, gate_id, stages, candidate):
    artifacts = [checked_artifact(root, "candidate.json")]
    archive = Path(candidate["archive"])
    if not archive.is_absolute():
        archive = root / archive
    artifacts.append(checked_artifact(root, archive.resolve().relative_to(root).as_posix()))
    log_paths = []
    scenarios = []
    for stage in stages:
        relative = STAGE_LOGS[stage]
        artifact = checked_artifact(root, relative)
        artifacts.append(artifact)
        log_paths.append(relative)
        text = (root / relative).read_text(errors="replace")
        if "stage=" + stage + " PASS" not in text:
            raise ValueError(f"{gate_id}: {stage} has no successful raw stage completion marker")

    combined = "\n".join((root / path).read_text(errors="replace") for path in log_paths)
    if gate_id == "M3":
        missing = [marker for marker in M3_MARKERS if marker not in combined]
        if missing:
            raise ValueError(f"M3 has no measured transfer/revocation bound evidence: {missing}")
        snapshot = checked_artifact(root, "durability-tests.json")
        artifacts.append(snapshot)
        if not go_test_passed(
            root / "durability-tests.json",
            "github.com/rtcdance/streamgate/pkg/service/gating",
            "TestAcceptance_GatingRuleSnapshotRepeatableRead",
        ):
            raise ValueError("M3 deterministic PostgreSQL policy snapshot test did not PASS")
        scenarios.append("durability-tests.json:PASS TestAcceptance_GatingRuleSnapshotRepeatableRead")
    if gate_id == "M7":
        snapshot = checked_artifact(root, "durability-tests.json")
        artifacts.append(snapshot)
        test_log = root / "durability-tests.json"
        missing = []
        for test_id, marker in M7_TESTS:
            if not go_test_passed(test_log, M7_PACKAGE, test_id):
                missing.append(f"{test_id}:PASS")
                continue
            if not go_test_output_contains(test_log, M7_PACKAGE, test_id, marker):
                missing.append(f"{test_id}:OUTPUT:{marker}")
            else:
                scenarios.append(f"durability-tests.json:PASS {test_id} emitted {marker}")
        if missing:
            raise ValueError(f"M7 lacks passing empty-anchor or rollback crash recovery test evidence: {missing}")
    if gate_id == "M5":
        for relative in ("reference.json", "release-compat/compatibility.json",
                         "release-compat/schema-before.txt", "release-compat/schema-after.txt"):
            artifacts.append(checked_artifact(root, relative))
        compatibility = json.loads((root / "release-compat/compatibility.json").read_text())
        reference = json.loads((root / "reference.json").read_text())
        floor = json.loads((Path.cwd() / "config/performance.acceptance.json").read_text())["safetyBaselineRevision"]
        if (compatibility.get("status") != "PASS" or compatibility.get("candidateSHA") != candidate["revision"]
                or not re.fullmatch(r"[0-9a-f]{40}", str(floor))
                or compatibility.get("baselineSHA") != floor or reference.get("revision") != floor
                or compatibility.get("baselineImageID") != reference.get("images", {}).get("monolith", {}).get("imageID")
                or not re.fullmatch(r"sha256:[0-9a-f]{64}", str(compatibility.get("baselineImageID")))
                or compatibility.get("restoredImageID") != candidate["images"]["monolith"]["imageID"]
                or compatibility.get("schemaBeforeSHA256") != sha256(root / "release-compat/schema-before.txt")
                or compatibility.get("schemaAfterSHA256") != sha256(root / "release-compat/schema-after.txt")
                or compatibility.get("schemaBeforeSHA256") != compatibility.get("schemaAfterSHA256")
                or any(marker not in combined for marker in ("RELEASE_SCHEMA_BASELINE_STARTED", "RELEASE_SCHEMA_BUSINESS_PASS", "RELEASE_SCHEMA_A_RECOVERED"))):
            raise ValueError("M5 lacks same-candidate business compatibility on the new schema and exact baseline/A identities")
        missing = [marker for marker in M5_DRILL_MARKERS if marker not in combined]
        if missing:
            raise ValueError(f"M5 local release drills are incomplete: {missing}")
        artifacts.append(checked_artifact(root, "release-drills/broken-start.log"))
        if "auth.jwt_secret is only 6 bytes" not in (root / "release-drills/broken-start.log").read_text():
            raise ValueError("M5 broken-start raw diagnostic does not match the injected JWT configuration")
        artifacts.append(checked_artifact(root, "release-drills/broken-smoke.log"))
        smoke = (root / "release-drills/broken-smoke.log").read_text()
        if "PASS: Anvil chainId=31337" not in smoke or "FAIL: dev-mint failed: HTTP 500" not in smoke:
            raise ValueError("M5 broken-smoke raw diagnostic does not match the injected DemoNFT address")
    if gate_id == "M4":
        if (root / "perf-reference/reference-reuse.json").exists():
            raise ValueError("M4 final all must remeasure its reference in the current run")
        checked_artifact(root, "perf-compare/comparison.json")
        result = json.loads((root / "perf-compare/comparison.json").read_text())
        if result.get("ratio", float("inf")) > 1.20:
            raise ValueError("M4 same-runner p95 regression exceeds 1.20")
        if result.get("candidateMedianP95MS", 0) <= 0 or result.get("referenceMedianP95MS", 0) <= 0:
            raise ValueError("M4 comparison is missing measured p95 values")
        # The protocol file and every result row are raw inputs to the claimed matrix.
        for directory in (root / "perf", root / "perf-compare", root / "perf-reference"):
            if not directory.is_dir():
                raise ValueError(f"M4 missing performance evidence directory: {directory.name}")
            for path in sorted(directory.rglob("*")):
                if path.is_file() and (path.suffix in {".json", ".jsonl", ".prom"} or path.name.endswith(".containers.json")):
                    artifacts.append(checked_artifact(root, path.relative_to(root).as_posix()))
        log_paths.extend(["perf-compare/comparison.json"])
    scenarios.extend(pass_scenarios(root, log_paths))
    scenarios = sorted(set(scenarios))
    if not scenarios:
        raise ValueError(f"{gate_id} has no scenario/assertion lines in the raw stage logs")
    return artifacts, scenarios


def generate(root, project, revision, platform):
    root = root.resolve()
    repository = Path.cwd().resolve()
    protocol = repository / "config/performance.acceptance.json"
    if not protocol.is_file():
        raise ValueError("versioned performance acceptance config is unavailable")
    config_hash = sha256(protocol)
    candidate = json.loads((root / "candidate.json").read_text())
    if candidate.get("revision") != revision:
        raise ValueError("candidate manifest SHA does not match the frozen checkout")
    archive = Path(candidate.get("archive", ""))
    if not archive.is_absolute():
        archive = root / archive
    archive = archive.resolve()
    try:
        archive.relative_to(root)
    except ValueError as exc:
        raise ValueError("candidate archive escapes the acceptance artifact directory") from exc
    expected_archive_hash = candidate.get("archiveSHA256")
    if not re.fullmatch(r"[0-9a-f]{64}", str(expected_archive_hash or "")):
        raise ValueError("candidate manifest has no valid archive SHA256")
    if sha256(archive) != expected_archive_hash:
        raise ValueError("candidate archive SHA256 mismatch")
    required_images = {"monolith", "api-gateway", "transcoder"}
    if set(candidate.get("images", {})) != required_images:
        raise ValueError("candidate manifest is missing required application image identities")
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("candidate revision is not a full Git SHA")
    if any(image.get("revision") != revision or image.get("platform") != platform
           or not re.fullmatch(r"sha256:[0-9a-f]{64}", str(image.get("imageID", "")))
           or image.get("buildTags") != ["nats_integration"]
           or image.get("registryDigest") is not None
           for image in candidate["images"].values()):
        raise ValueError("candidate image identity/platform/revision is inconsistent")
    all_ids = sorted({image["imageID"] for image in candidate["images"].values()})
    perf_environment = json.loads((root / "perf/environment.json").read_text())
    fixture_hash = perf_environment.get("fixture_hash")
    if not re.fullmatch(r"[0-9a-f]{64}", str(fixture_hash or "")):
        raise ValueError("M4 performance fixture hash is missing or invalid")
    if perf_environment.get("protocolSHA256") != config_hash:
        raise ValueError("M4 performance config hash differs from the versioned acceptance config")
    if perf_environment.get("commit") != revision or perf_environment.get("harness_revision") != revision:
        raise ValueError("M4 performance environment is not bound to this candidate SHA")
    images = {
        service: {
            "imageID": entry["imageID"],
            "registryDigest": entry.get("registryDigest"),
            "buildTags": entry["buildTags"],
            "platform": entry["platform"],
            "revision": entry["revision"],
        }
        for service, entry in sorted(candidate["images"].items())
    }
    gates = []
    for gate_id, stages in GATES.items():
        if gate_id == "M6":
            artifacts, scenarios = _doc_scenarios(root, repository, revision)
        else:
            artifacts, scenarios = stage_evidence(root, gate_id, stages, candidate)
        gates.append({
            "id": gate_id, "status": "PASS", "candidateSHA": revision,
            "exitCode": 0, "imageIDs": all_ids, "images": images,
            "archiveSHA256": expected_archive_hash, "registryDigest": None,
            "fixtureHash": fixture_hash if gate_id == "M4" else None,
            "configHash": config_hash,
            "command": "VERIFY_STAGE=all bash scripts/verify-docker.sh",
            "ciRun": None,
            "scenarios": scenarios, "artifacts": artifacts,
            "limitations": ["Exact-SHA remote CI has not run; this is local Docker evidence."],
        })
    index = {
        "schemaVersion": 2, "candidateSHA": revision, "project": project,
        "platform": platform, "localAcceptance": "PASS",
        "archiveSHA256": expected_archive_hash, "images": images,
        "fixtureHash": fixture_hash, "configHash": config_hash,
        "command": "VERIFY_STAGE=all bash scripts/verify-docker.sh",
        "rawExitCode": 0,
        "sameShaRemoteCI": {"required": True, "status": "PENDING"},
        "gates": gates,
    }
    output = root / "candidate-gates.json"
    output.write_text(json.dumps(index, indent=2, sort_keys=True) + "\n")
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("artifact_root", type=Path)
    parser.add_argument("--project", required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--platform", required=True)
    args = parser.parse_args()
    try:
        path = generate(args.artifact_root, args.project, args.revision, args.platform)
    except (OSError, ValueError, KeyError, TypeError, json.JSONDecodeError) as exc:
        print(f"candidate evidence generation failed: {exc}", file=sys.stderr)
        return 1
    print(f"derived local candidate evidence index from raw stage outputs: {path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
