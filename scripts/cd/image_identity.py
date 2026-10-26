#!/usr/bin/env python3
"""Write the CI image identity without conflating its three digest types."""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import tarfile

from candidate_manifest import docker_json, file_hash


BINDING_FIELDS = ("imageID", "archiveSHA256", "buildTags", "platform", "revision")


def _config_digest(path):
    name = path.rsplit("/", 1)[-1].removesuffix(".json")
    if not re.fullmatch(r"[0-9a-f]{64}", name):
        raise ValueError(f"unexpected archive config path {path}")
    return "sha256:" + name


def archive_digests(path):
    """Return the (config, manifest) digests of the single image in an archive.

    Docker's classic image store reports the config digest as an image's ID
    while the containerd store reports the OCI manifest digest. Both are
    recorded in the archive, so binding to them names the same image on either
    store instead of only on the daemon that happened to build it. Files that
    are not docker-save tars (unit fixtures) yield None and keep the store ID.
    """
    try:
        with tarfile.open(path) as archive:
            names = set(archive.getnames())
            if "manifest.json" not in names:
                return None
            configs = {}
            for entry in json.load(archive.extractfile("manifest.json")):
                digest = _config_digest(entry["Config"])
                for tag in entry.get("RepoTags") or []:
                    configs[tag.rsplit("/", 1)[-1]] = digest
            manifests = {}
            if "index.json" in names:
                for entry in json.load(archive.extractfile("index.json")).get("manifests", []):
                    name = (entry.get("annotations") or {}).get("io.containerd.image.name")
                    if name:
                        manifests[name.rsplit("/", 1)[-1]] = entry["digest"]
    except (tarfile.TarError, OSError):
        return None
    except (KeyError, TypeError, json.JSONDecodeError) as exc:
        raise ValueError(f"archive manifest is malformed: {exc}") from exc
    if not configs:
        raise ValueError("archive records no tagged image")
    if len(set(configs.values())) != 1:
        raise ValueError("archive must contain exactly one image")
    tag, config = next(iter(configs.items()))
    manifest = manifests.get(tag)
    if manifest is not None and not re.fullmatch(r"sha256:[0-9a-f]{64}", str(manifest)):
        raise ValueError("archive records an invalid manifest digest")
    return config, manifest


def verify_identity(expected, observed):
    # A reused tag or a second archive must never inherit a scanned image's
    # publication evidence merely by carrying the same source revision.
    for field in (*BINDING_FIELDS, "registryDigest"):
        if field not in expected or expected[field] != observed[field]:
            raise ValueError(f"recorded {field} differs from the loaded image/archive")
    if expected.get("image_id", observed["imageID"]) != observed["imageID"] or expected.get("commit", observed["revision"]) != observed["revision"]:
        raise ValueError("legacy identity aliases conflict with the loaded image")


def push_digest(output):
    digests = re.findall(r"\bdigest:\s*(sha256:[0-9a-f]{64})(?=\s|$)", output)
    if len(digests) != 1:
        raise ValueError("push output must contain exactly one registry digest")
    return digests[0]


def publication_record(value, service, repository, push_log, ci_run, ci_attempt):
    if (not re.fullmatch(r"[a-z][a-z0-9-]*", service)
            or not re.fullmatch(r"[a-z0-9][a-z0-9._:/-]*", repository)
            or repository.endswith("/")
            or not re.fullmatch(r"[1-9][0-9]*", ci_run)
            or not re.fullmatch(r"[1-9][0-9]*", ci_attempt)):
        raise ValueError("publication requires a service, registry repository and exact CI run/attempt")
    if push_log.name != f"{service}.push.log" or push_log.with_suffix(".exit").read_text().strip() != "0":
        raise ValueError("publication requires the service's successful raw docker push exit")
    digest = push_digest(push_log.read_text())
    return {
        "schemaVersion": 1, "service": service, "candidateSHA": value["revision"],
        **{field: value[field] for field in BINDING_FIELDS},
        "registryDigest": digest, "registryReference": f"{repository}/{service}@{digest}",
        "ciRun": ci_run, "ciAttempt": ci_attempt,
        "publishedAt": datetime.now(timezone.utc).isoformat(),
        "pushExitCode": 0, "pushLog": push_log.name, "pushLogSHA256": file_hash(push_log),
    }


def identity(info, revision, archive_hash, digests=None):
    labels = info["Config"].get("Labels") or {}
    if labels.get("org.opencontainers.image.revision") != revision:
        raise ValueError("image revision does not match candidate SHA")
    tags = labels.get("com.streamgate.build.tags", "").split(",")
    if not all(tags):
        raise ValueError("actual build tags must be recorded in image labels")
    # The archive is the only identity both stores agree on: a classic store
    # reports its config digest as the image ID, a containerd store its OCI
    # manifest digest. Keep the store ID beside it for same-daemon checks.
    content, manifest = digests if digests else (info["Id"], None)
    if info["Id"] not in {digest for digest in (content, manifest) if digest}:
        raise ValueError("loaded image identity is not a digest of this archive")
    return {
        "imageID": content, "daemonImageID": info["Id"], "archiveSHA256": archive_hash,
        # Docker's containerd store can synthesize RepoDigests for local builds.
        # Only the later registry publication receipt may fill registryDigest.
        "registryDigest": None, "dockerRepoDigests": info.get("RepoDigests") or [], "buildTags": tags,
        "platform": info["Os"] + "/" + info["Architecture"], "revision": revision,
        # Retain the existing consumers' wire contract during additive rollout.
        "image_id": content, "commit": revision,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--archive", required=True, type=Path)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--out", required=True, type=Path)
    parser.add_argument("--verify", action="store_true", help="verify the existing build metadata instead of overwriting it")
    parser.add_argument("--publication-log", type=Path)
    parser.add_argument("--publication-out", type=Path)
    parser.add_argument("--service")
    parser.add_argument("--registry-repository")
    parser.add_argument("--ci-run")
    parser.add_argument("--ci-attempt")
    args = parser.parse_args()
    value = identity(docker_json("image", "inspect", args.tag)[0], args.revision, file_hash(args.archive),
                     archive_digests(args.archive))
    publication_args = (args.publication_log, args.publication_out, args.service, args.registry_repository, args.ci_run, args.ci_attempt)
    if any(publication_args) and (not args.verify or not all(publication_args)):
        parser.error("publication requires --verify and all publication/CI arguments")
    if args.verify:
        verify_identity(json.loads(args.out.read_text()), value)
        if args.publication_log:
            receipt = publication_record(value, args.service, args.registry_repository, args.publication_log, args.ci_run, args.ci_attempt)
            args.publication_out.parent.mkdir(parents=True, exist_ok=True)
            args.publication_out.write_text(json.dumps(receipt, indent=2) + "\n")
        print(f"verified imageID={value['imageID']} archiveSHA256={value['archiveSHA256']} revision={value['revision']}")
        return
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(value, indent=2) + "\n")


if __name__ == "__main__":
    main()
