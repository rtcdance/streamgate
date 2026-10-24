#!/usr/bin/env python3
"""Fail closed unless the derived local candidate index matches raw evidence."""
import json
import re
from pathlib import Path
import sys
import hashlib


def file_hash(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def validate(root, repository=None):
    root = root.resolve()
    repository = (repository or Path.cwd()).resolve()
    candidate = json.loads((root / "candidate.json").read_text())
    index = json.loads((root / "candidate-gates.json").read_text())
    revision = candidate["revision"]
    images = {entry["imageID"] for entry in candidate["images"].values()}
    if not re.fullmatch(r"[0-9a-f]{40}", revision) or not images or index.get("candidateSHA") != revision:
        raise ValueError("acceptance index does not identify this candidate")
    if index.get("schemaVersion") != 2:
        raise ValueError("acceptance index lacks required provenance schema")
    # The manifest may be produced on one runner and validated on another, so
    # a relative archive is resolved against THIS run's root. An absolute path
    # is still accepted only when it already lives inside the root.
    stored = Path(candidate["archive"])
    archive = (root / stored if not stored.is_absolute() else stored).resolve()
    try:
        archive.relative_to(root)
    except ValueError as exc:
        raise ValueError("candidate archive escapes this run's directory") from exc
    if not archive.is_file() or file_hash(archive) != candidate.get("archiveSHA256"):
        raise ValueError("candidate archive identity is missing or invalid")
    if not re.fullmatch(r"[0-9a-f]{64}", str(candidate.get("archiveSHA256", ""))):
        raise ValueError("candidate archive SHA256 is malformed")
    if index.get("archiveSHA256") != candidate.get("archiveSHA256"):
        raise ValueError("acceptance index archive SHA256 differs from candidate manifest")
    expected_images = {
        service: {
            "imageID": entry["imageID"], "registryDigest": entry.get("registryDigest"),
            "buildTags": entry["buildTags"], "platform": entry["platform"],
            "revision": entry["revision"],
        }
        for service, entry in sorted(candidate["images"].items())
    }
    if set(expected_images) != {"monolith", "api-gateway", "transcoder"}:
        raise ValueError("candidate manifest does not contain exactly the required application images")
    for service, image in expected_images.items():
        digest = image.get("registryDigest")
        if (not re.fullmatch(r"sha256:[0-9a-f]{64}", str(image.get("imageID", "")))
                or image.get("revision") != revision
                or image.get("platform") != index.get("platform")
                or image.get("buildTags") != ["nats_integration"]
                or (digest is not None and not re.fullmatch(r"sha256:[0-9a-f]{64}", str(digest)))):
            raise ValueError(f"candidate image identity is invalid for {service}")
    if index.get("images") != expected_images:
        raise ValueError("acceptance index image identities differ from candidate manifest")
    config_hash = file_hash(repository / "config/performance.acceptance.json")
    environment = json.loads((root / "perf/environment.json").read_text())
    fixture_hash = environment.get("fixture_hash")
    if (not re.fullmatch(r"[0-9a-f]{64}", str(fixture_hash or ""))
            or environment.get("commit") != revision
            or environment.get("harness_revision") != revision
            or environment.get("protocolSHA256") != config_hash):
        raise ValueError("M4 fixture/config hash provenance does not match candidate SHA")
    if index.get("configHash") != config_hash or index.get("fixtureHash") != fixture_hash:
        raise ValueError("acceptance index config/fixture hash differs from raw M4 environment")
    if index.get("command") != "VERIFY_STAGE=all bash scripts/verify-docker.sh" or index.get("rawExitCode") != 0:
        raise ValueError("acceptance index does not record a successful full all command")
    if index.get("localAcceptance") != "PASS":
        raise ValueError("local candidate acceptance is not PASS")
    remote = index.get("sameShaRemoteCI", {})
    if remote.get("required") is not True or remote.get("status") != "PENDING":
        raise ValueError("remote same-SHA CI must remain explicitly pending")
    gates = index["gates"]
    if len(gates) != 9 or {gate["id"] for gate in gates} != {f"M{i}" for i in range(9)}:
        raise ValueError("M0-M8 must each be present exactly once")
    for gate in gates:
        gid = gate["id"]
        if gate.get("status") != "PASS" or gate.get("candidateSHA") != revision:
            raise ValueError(f"{gid}: incomplete, skipped, failed or wrong candidate")
        if (gate.get("exitCode") != 0 or gate.get("ciRun") is not None
                or gate.get("configHash") != config_hash
                or gate.get("fixtureHash") != (fixture_hash if gid == "M4" else None)
                or gate.get("archiveSHA256") != candidate.get("archiveSHA256")
                or gate.get("images") != expected_images
                or not gate.get("limitations")):
            raise ValueError(f"{gid}: missing exit, CI, fixture, config or image provenance")
        if set(gate.get("imageIDs", [])) != images:
            raise ValueError(f"{gid}: candidate image identity mismatch")
        if not gate.get("command") or not gate.get("scenarios") or not gate.get("artifacts"):
            raise ValueError(f"{gid}: missing commands, scenarios or raw evidence")
        seen = set()
        for artifact in gate["artifacts"]:
            relative = artifact["path"]
            if relative in seen:
                raise ValueError(f"{gid}: duplicate evidence artifact {relative}")
            seen.add(relative)
            path = (root / relative).resolve()
            if not path.is_relative_to(root):
                raise ValueError("artifact escapes this run's directory")
            if not path.is_file() or path.stat().st_size == 0:
                raise ValueError(f"{gid}: missing or empty raw evidence {relative}")
            if file_hash(path) != artifact.get("sha256"):
                raise ValueError(f"{gid}: evidence hash mismatch for {relative}")


def main():
    try:
        validate(Path(sys.argv[1]))
    except (OSError, ValueError, KeyError, TypeError, IndexError) as exc:
        print(f"OVERALL ACCEPTANCE FAILURE: {exc}", file=sys.stderr)
        return 1
    print("Local M0-M8 candidate evidence index is complete; remote CI remains pending")
    return 0


if __name__ == "__main__":
    sys.exit(main())
