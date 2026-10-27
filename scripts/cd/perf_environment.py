"""Capture the actual Docker runner, service budgets and loader binary identity."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

from perf_protocol import read_json


def command(*args):
    return subprocess.check_output(args, text=True, timeout=30).strip()


def background_peer_descriptor(identity):
    """Describe a neighbour container by what it is, never by which instance it is.

    The ID is minted on every start, so two runs of the same workload next to the
    same neighbours disagree on it; the compose project, service, image and
    resource limits are what actually change the background load.
    """
    record = json.loads(command("docker", "inspect", identity))[0]
    labels = record["Config"]["Labels"] or {}
    return {
        "composeProject": labels.get("com.docker.compose.project", ""),
        "service": labels.get("com.docker.compose.service", ""),
        "imageID": record["Image"],
        "imageReference": record["Config"]["Image"],
        "limits": {
            "nanoCPUs": record["HostConfig"]["NanoCpus"],
            "memoryBytes": record["HostConfig"]["Memory"],
        },
    }


def capture(root, project, compose):
    root = Path(root)
    rows = sorted(p for p in root.rglob("*-viewers-*.json") if not p.name.endswith(".execution.json"))
    if not rows: raise ValueError("no completed playback result")
    playback = read_json(rows[0])
    resources = read_json(rows[0].with_name(f'resources-{playback["scenario"]}-{playback["viewers"]}.json'))
    harness = command("git", "rev-parse", "HEAD")
    target = os.environ.get("PERF_TARGET_SHA") or harness
    if playback["commit"] != target or playback["harness_revision"] != harness:
        raise ValueError("captured workload differs from expected application/harness")
    binary = Path(os.environ["PERF_LOADER_BINARY"])
    image = os.environ["PERF_LOADER_IMAGE"]
    if not binary.is_file() or not re.fullmatch(r"sha256:[0-9a-f]{64}", image):
        raise ValueError("loader image or binary identity unavailable")
    build = read_json(binary.with_suffix(".build.json"))
    if build != {"harnessRevision": harness, "imageID": image, "binarySHA256": hashlib.sha256(binary.read_bytes()).hexdigest()}:
        raise ValueError("loader build manifest differs from binary or source identity")
    (root / "loader-build.json").write_text(json.dumps(build, sort_keys=True) + "\n")
    docker = json.loads(command("docker", "info", "--format", "{{json .}}"))
    runner = {key: docker[key] for key in ("ID", "OSType", "Architecture", "NCPU", "MemTotal", "KernelVersion", "ServerVersion")}
    if not runner["ID"] or not runner["NCPU"] or not runner["MemTotal"]:
        raise ValueError("missing Docker runner identity or capacity")
    runner["operatingSystem"] = docker.get("OperatingSystem", "unknown")
    runner["executionProfile"] = os.environ.get("VERIFY_ENVIRONMENT_PROFILE", "native-linux")
    runner["executionPlatform"] = os.environ.get("VERIFY_PLATFORM", "linux/amd64")
    runner["measurementScope"] = ("local Docker Desktop; shared/emulated latency, no production capacity claim"
                                   if runner["executionProfile"] == "desktop-local" else "dedicated native Linux")
    # Keep background peers in the compared runner identity, but by what they
    # ARE rather than by which instance they are. A container ID is minted per
    # start, so an unrelated neighbour restarting mid-run used to make two
    # otherwise identical environments fail the identity comparison (observed
    # on 40566983: the only differing field was backgroundContainers, because
    # another compose project on the same Mac started five containers between
    # the reference and candidate rounds). Compose project, service, image and
    # resource limits still refuse a real change in the background load.
    own_run = os.environ.get("VERIFY_PROJECT", project)
    peers = command("docker", "ps", "--format", '{{.ID}}|{{.Label "com.streamgate.verify.run"}}')
    peer_ids = sorted(row.split("|", 1)[0] for row in peers.splitlines()
                      if row and row.split("|", 1)[-1] != own_run)
    runner["backgroundContainers"] = sorted((background_peer_descriptor(i) for i in peer_ids),
                                             key=lambda peer: json.dumps(peer, sort_keys=True))
    background_load = command("docker", "stats", "--no-stream", "--format", '{{json .}}')
    identifiers = command("docker", "ps", "-aq", "--filter", "label=com.docker.compose.project=" + project).split()
    services = {}
    for identity in identifiers:
        record = json.loads(command("docker", "inspect", identity))[0]
        labels = record["Config"]["Labels"] or {}
        service = labels.get("com.docker.compose.service")
        if not service: continue
        if service in services: raise ValueError("duplicate service instance in benchmark project")
        services[service] = {"imageID": record["Image"], "imageReference": record["Config"]["Image"],
                             "limits": {"nanoCPUs": record["HostConfig"]["NanoCpus"], "memoryBytes": record["HostConfig"]["Memory"]}}
    protocol = read_json("config/performance.acceptance.json")
    if set(services) != {"monolith", "transcoder"} | set(protocol["infrastructureResources"]):
        raise ValueError("unexpected or missing benchmark infrastructure")
    for service, limits in protocol["infrastructureResources"].items():
        if services[service]["limits"] != limits:
            raise ValueError("infrastructure budget differs from the versioned protocol")
    for role, service in [("api", "monolith"), ("worker", "transcoder")]:
        if services[service]["limits"] != protocol["resources"][role] or services[service]["imageID"] != resources["containers"][role]["imageID"]:
            raise ValueError("live application budget/image differs from measured cgroup identity")
    if resources["containers"]["loader"]["imageID"] != image:
        raise ValueError("loader tool image differs from measured image")
    fixture = Path(os.environ["PERF_MEDIA_HASHES_FILE"])
    for source, destination in [(fixture, root / "media-hashes.json"), (fixture.with_suffix(".provenance.json"), root / "media-hashes.provenance.json")]:
        raw = source.read_bytes()
        if not raw: raise ValueError("independent fixture evidence is empty")
        if source.resolve() != destination.resolve(): destination.write_bytes(raw)
    task_evidence = Path(os.environ["PERF_TRANSCODE_EVIDENCE_FILE"])
    destination = root / "transcoding-metadata.json"
    if task_evidence.resolve() != destination.resolve(): destination.write_bytes(task_evidence.read_bytes())
    if any(not r["limits"]["nanoCPUs"] or not r["limits"]["memoryBytes"] for r in services.values()):
        raise ValueError("unbounded infrastructure service")
    versions = {name: command(*args).splitlines()[0] for name, args in {
        "go": ("go", "version"), "ffmpeg": ("ffmpeg", "-version"), "forge": ("forge", "--version"),
        "node": ("node", "--version"), "docker": ("docker", "--version")}.items()}
    manifest = {"version": 2, "commit": target, "harness_revision": harness,
                "compose_project": project, "run": resources["containers"]["api"]["run"],
                "protocolSHA256": hashlib.sha256(Path("config/performance.acceptance.json").read_bytes()).hexdigest(),
                "composeSHA256": hashlib.sha256(Path(compose).read_bytes()).hexdigest(),
                "runner": runner, "backgroundLoadSnapshot": background_load,
                # Instance IDs are diagnostics only: they are recorded for the
                # operator and deliberately kept OUT of the compared identity.
                "backgroundContainerIDs": peer_ids,
                "services": services, "tools": versions,
                "applicationImages": {role: resources["containers"][role]["imageID"] for role in ("api", "worker")},
                "loader": {"kind": "docker", "imageID": image, "binarySHA256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                           "limits": protocol["resources"]["loader"], "maxParallelMediaRequestsPerViewer": 1}}
    for key in ("fixture_hash", "content_id", "quality", "media_hashes_sha256"):
        manifest[key] = playback[key]
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path)
    parser.add_argument("project")
    parser.add_argument("compose")
    args = parser.parse_args()
    manifest = capture(args.root, args.project, args.compose)
    (args.root / "environment.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    print(f"captured actual Docker environment for {manifest['commit'][:12]}")


if __name__ == "__main__":
    try: main()
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as exc:
        raise SystemExit(f"performance environment capture failed: {exc}")
