#!/usr/bin/env python3
"""Import the scanned CI build archives into the Docker acceptance entry."""
import argparse
import json
from pathlib import Path
import re
import subprocess

from candidate_manifest import docker_json, file_hash, verify
from image_identity import identity, verify_identity
from write_candidate_manifest import load_images


RUNTIME_SERVICES = ("monolith", "api-gateway", "transcoder")


def import_candidate(inputs, prefix, revision, platform, manifest):
    if (not re.fullmatch(r"[0-9a-f]{40}", revision)
            or not re.fullmatch(r"[a-z0-9][a-z0-9-]*", prefix)
            or platform != "linux/amd64"):
        raise ValueError("full candidate SHA, isolated image prefix and linux/amd64 are required")
    images, problems = load_images(inputs / "meta")
    if problems:
        raise ValueError("invalid CI build inputs: " + "; ".join(problems))
    if any(entry["revision"] != revision or entry["platform"] != platform
           or entry["buildTags"] != ["nats_integration"] or entry["registryDigest"] is not None
           for entry in images.values()):
        raise ValueError("CI build metadata differs from this candidate SHA/platform/tags")
    if manifest.exists():
        raise ValueError("candidate manifest already exists; use a fresh acceptance artifact directory")
    manifest.parent.mkdir(parents=True, exist_ok=True)
    archive = manifest.parent / "candidate-images.tar"
    if archive.exists():
        raise ValueError("candidate archive already exists; refusing to replace another run")
    entries = {}
    tags = []
    for service in RUNTIME_SERVICES:
        source = inputs / f"candidate-{service}.tar"
        subprocess.run(["docker", "load", "-i", str(source)], check=True)
        original_tag = f"streamgate-{service}:candidate"
        observed = identity(docker_json("image", "inspect", original_tag)[0], revision, file_hash(source))
        verify_identity(images[service], observed)
        tag = f"{prefix}-{service}:latest"
        subprocess.run(["docker", "tag", original_tag, tag], check=True)
        tags.append(tag)
        entries[service] = {
            "tag": tag, "imageID": observed["imageID"], "registryDigest": None,
            "buildTags": observed["buildTags"], "platform": platform, "revision": revision,
            "buildArchiveSHA256": observed["archiveSHA256"],
        }
    subprocess.run(["docker", "save", "-o", str(archive), *tags], check=True)
    document = {
        "version": 1, "revision": revision, "archive": str(archive.resolve()),
        "archiveSHA256": file_hash(archive), "images": entries,
        "ciBuildInputs": {"path": str(inputs.resolve()), "images": images},
    }
    verify(document, platform=platform)
    manifest.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n")
    return document


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inputs", type=Path, required=True)
    parser.add_argument("--prefix", required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--platform", required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    import_candidate(args.inputs, args.prefix, args.revision, args.platform, args.out)
    print(f"CI build archives verified and imported without rebuilding: {args.revision}")


if __name__ == "__main__":
    main()
