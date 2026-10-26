#!/usr/bin/env python3
"""Record distinct image/archive/registry identities; verify every stage reuse."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess


def docker_json(*args):
    return json.loads(subprocess.check_output(["docker", *args], text=True))


def file_hash(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def verify(document, project=None, platform=None, archive_path=None):
    # A relocated manifest stores the archive relative to its own directory;
    # the caller passes the resolved file so verification never guesses a cwd.
    archive = Path(archive_path) if archive_path is not None else Path(document["archive"])
    if not archive.stat().st_size:
        raise ValueError("candidate archive is empty")
    if file_hash(archive) != document["archiveSHA256"]:
        raise ValueError("candidate archive SHA256 mismatch")
    required_roles = {"monolith", "api-gateway", "transcoder"}
    if not required_roles.issubset(document["images"]):
        raise ValueError("candidate manifest is missing a required application image")
    for service, entry in document["images"].items():
        if service in required_roles and entry["buildTags"] != ["nats_integration"]:
            raise ValueError(f"{service}: required build tags are absent")
        if platform and entry["platform"] != platform:
            raise ValueError(f"{service}: requested platform {platform} differs from manifest {entry['platform']}")
        info = docker_json("image", "inspect", entry["tag"])[0]
        # imageID is the archive identity every store agrees on; an importing
        # daemon records its own ID beside it because the containerd store
        # reports the OCI manifest digest instead of the config digest.
        expected_id = entry.get("daemonImageID") or entry["imageID"]
        if info["Id"] != expected_id:
            raise ValueError(f"{service}: candidate image tag was replaced")
        labels = info["Config"].get("Labels") or {}
        if labels.get("org.opencontainers.image.revision") != document["revision"]:
            raise ValueError(f"{service}: revision label mismatch")
        if labels.get("com.streamgate.build.tags", "").split(",") != entry["buildTags"]:
            raise ValueError(f"{service}: actual build tags mismatch")
        if info["Os"] + "/" + info["Architecture"] != entry["platform"]:
            raise ValueError(f"{service}: platform mismatch")
        if project:
            ids = subprocess.check_output([
                "docker", "ps", "-aq", "--filter", f"label=com.docker.compose.project={project}",
                "--filter", f"label=com.docker.compose.service={service}",
            ], text=True).split()
            for container_id in ids:
                actual = docker_json("inspect", container_id)[0]["Image"]
                if actual != expected_id:
                    raise ValueError(f"{service}: running stage does not use candidate image")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest", type=Path)
    parser.add_argument("--prefix")
    parser.add_argument("--archive", type=Path)
    parser.add_argument("--revision")
    parser.add_argument("--platform")
    parser.add_argument("--project")
    args = parser.parse_args()
    if args.prefix:
        document = {
            "version": 1, "revision": args.revision,
            "archive": str(args.archive), "archiveSHA256": file_hash(args.archive), "images": {},
        }
        for service in ("monolith", "api-gateway", "transcoder"):
            tag = f"{args.prefix}-{service}:latest"
            info = docker_json("image", "inspect", tag)[0]
            actual_platform = info["Os"] + "/" + info["Architecture"]
            if not args.platform or actual_platform != args.platform:
                raise ValueError(f"{service}: requested platform {args.platform} differs from actual {actual_platform}")
            document["images"][service] = {
                "tag": tag, "imageID": info["Id"],
                "registryDigest": None,
                "dockerRepoDigests": info.get("RepoDigests") or [],
                "buildTags": ["nats_integration"],
                "platform": actual_platform,
                "revision": args.revision,
            }
        args.manifest.write_text(json.dumps(document, indent=2) + "\n")
        archive_path = args.archive
    else:
        document = json.loads(args.manifest.read_text())
        if args.revision and document["revision"] != args.revision:
            raise ValueError("candidate manifest revision does not match this checkout")
        # A downloaded artifact root stores the archive beside its manifest, so
        # the CLI must not resolve it against whatever directory it was called from.
        stored = Path(document["archive"])
        archive_path = stored if stored.is_absolute() else args.manifest.parent / stored
    verify(document, args.project, args.platform, archive_path=archive_path)
    print(f"candidate identity verified: {document['revision']}")


if __name__ == "__main__":
    main()
