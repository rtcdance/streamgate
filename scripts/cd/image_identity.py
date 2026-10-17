#!/usr/bin/env python3
"""Write the CI image identity without conflating its three digest types."""
import argparse
import json
from pathlib import Path

from candidate_manifest import docker_json, file_hash


def identity(info, revision, archive_hash):
    labels = info["Config"].get("Labels") or {}
    if labels.get("org.opencontainers.image.revision") != revision:
        raise ValueError("image revision does not match candidate SHA")
    tags = labels.get("com.streamgate.build.tags", "").split(",")
    if not all(tags):
        raise ValueError("actual build tags must be recorded in image labels")
    return {
        "imageID": info["Id"], "archiveSHA256": archive_hash,
        # Docker's containerd store can synthesize RepoDigests for local builds.
        # Only the later registry publication receipt may fill registryDigest.
        "registryDigest": None, "dockerRepoDigests": info.get("RepoDigests") or [], "buildTags": tags,
        "platform": info["Os"] + "/" + info["Architecture"], "revision": revision,
        # Retain the existing consumers' wire contract during additive rollout.
        "image_id": info["Id"], "commit": revision,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--archive", required=True, type=Path)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--out", required=True, type=Path)
    args = parser.parse_args()
    value = identity(docker_json("image", "inspect", args.tag)[0], args.revision, file_hash(args.archive))
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(value, indent=2) + "\n")


if __name__ == "__main__":
    main()
