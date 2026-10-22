#!/usr/bin/env python3
"""Write the per-SHA candidate manifest (TRANSITION_PLAN.md §7).

The plan requires a structured manifest so every later job (scan, smoke,
deploy, promotion) can talk about "the candidate" by immutable identity instead
of by a tag that could point anywhere. build.yml already records one
`meta/<service>.json` per built image; this collects them into the single
`candidate-manifest.json` the plan describes:

    {candidateSHA, buildTags, platform,
     images: {service: {imageID, archiveSHA256, registryDigest, ...}}, ...}

Scope note: this manifest covers the CANDIDATE IMAGES — the part CI can state
deterministically for one SHA. The acceptance-side fields the plan lists
(fixtureHash, contractChainID, contractAddress, contractCodeHash, abiHash,
results) are produced by the chain/perf/acceptance stages, not by the image
build, and live in those stages' own artifacts. They are recorded here only
when the caller supplies them; otherwise they are explicitly null rather than
silently omitted or invented.

Usage:
  write_candidate_manifest.py --meta-dir artifacts/meta --sha "$GITHUB_SHA" \
      --build-tags '...' --platform linux/amd64 --environment ci \
      --started-at "$STARTED_AT" --out candidate-manifest.json

Every expected build artifact and its separate identity fields are mandatory.
Archives are hashed again before the combined manifest is written.
"""

import argparse
import json
from pathlib import Path
import re
import sys

from candidate_manifest import file_hash
from image_identity import BINDING_FIELDS, push_digest


REQUIRED_SERVICES = {
    "monolith", "transcoder", "api-gateway", "auth", "cache",
    "metadata", "monitor", "streaming", "upload", "worker",
}
IDENTITY_FIELDS = ("imageID", "archiveSHA256", "registryDigest", "buildTags", "platform", "revision")


def load_images(meta_dir, required_services=REQUIRED_SERVICES):
    images = {}
    problems = []
    root = Path(meta_dir)
    if not root.is_dir():
        return images, [f"{meta_dir} is not a directory"]
    paths = {path.stem: path for path in root.glob("*.json")}
    missing = required_services - paths.keys()
    unexpected = paths.keys() - required_services
    if missing:
        problems.append(f"missing candidate metadata: {sorted(missing)}")
    if unexpected:
        problems.append(f"unexpected candidate metadata: {sorted(unexpected)}")
    for service in sorted(required_services & paths.keys()):
        path = paths[service]
        try:
            data = json.loads(path.read_text())
            if not isinstance(data, dict) or any(field not in data for field in IDENTITY_FIELDS):
                raise ValueError("separate image/archive/registry identities and build facts are required")
            image_id = data["imageID"]
            revision = data["revision"]
            if not re.fullmatch(r"sha256:[0-9a-f]{64}", str(image_id)):
                raise ValueError("invalid imageID")
            if not re.fullmatch(r"[0-9a-f]{40}", str(revision)):
                raise ValueError("invalid revision")
            if not re.fullmatch(r"[0-9a-f]{64}", str(data["archiveSHA256"])):
                raise ValueError("invalid archiveSHA256")
            digest = data["registryDigest"]
            if digest is not None and not re.fullmatch(r"sha256:[0-9a-f]{64}", str(digest)):
                raise ValueError("registryDigest must be an explicit null or a registry digest")
            if data.get("image_id", image_id) != image_id or data.get("commit", revision) != revision:
                raise ValueError("legacy identity aliases conflict with the canonical fields")
            if (not isinstance(data["buildTags"], list) or not data["buildTags"]
                    or any(not isinstance(tag, str) or not tag for tag in data["buildTags"])
                    or not isinstance(data["platform"], str) or not data["platform"]):
                raise ValueError("actual build tags and platform are required")
            archive = root.parent / f"candidate-{service}.tar"
            if not archive.is_file() or archive.stat().st_size == 0 or file_hash(archive) != data["archiveSHA256"]:
                raise ValueError("candidate archive is missing, empty or differs from archiveSHA256")
            images[service] = {field: data[field] for field in IDENTITY_FIELDS}
            images[service]["archive"] = archive.name
        except (OSError, ValueError, TypeError, KeyError) as exc:
            problems.append(f"{path}: {exc}")
    return images, problems


def merge_publications(images, directory, repository, ci_run, ci_attempt):
    root = directory.resolve()
    if not repository or not re.fullmatch(r"[1-9][0-9]*", ci_run) or not re.fullmatch(r"[1-9][0-9]*", ci_attempt):
        raise ValueError("exact registry repository, CI run and attempt are required")
    paths = {path.stem: path for path in root.glob("*.json")}
    if set(paths) != REQUIRED_SERVICES:
        raise ValueError("missing or unexpected service publication receipts")
    for service, image in images.items():
        receipt = json.loads(paths[service].read_text())
        producer_attempt = receipt.get("ciAttempt")
        # A failed-job rerun may consume a successful build from an earlier
        # attempt of this run. Its immutable build bindings must still match.
        valid_attempt = (isinstance(producer_attempt, str)
                         and re.fullmatch(r"[1-9][0-9]*", producer_attempt)
                         and int(producer_attempt) <= int(ci_attempt))
        if (receipt.get("schemaVersion") != 1 or receipt.get("service") != service
                or receipt.get("candidateSHA") != image["revision"]
                or receipt.get("ciRun") != ci_run or not valid_attempt
                or type(receipt.get("pushExitCode")) is not int or receipt["pushExitCode"] != 0):
            raise ValueError(f"{service}: publication receipt has a different candidate, run, attempt or failed push")
        if any(receipt.get(field) != image[field] for field in BINDING_FIELDS):
            raise ValueError(f"{service}: publication receipt does not bind this build/archive")
        digest = receipt.get("registryDigest")
        if not re.fullmatch(r"sha256:[0-9a-f]{64}", str(digest)):
            raise ValueError(f"{service}: invalid registry digest")
        reference = f"{repository}/{service}@{digest}"
        if receipt.get("registryReference") != reference or receipt.get("pushLog") != f"{service}.push.log":
            raise ValueError(f"{service}: publication reference or raw log is not bound to this service")
        log = root / f"{service}.push.log"
        if (not log.resolve().is_relative_to(root) or not log.is_file()
                or file_hash(log) != receipt.get("pushLogSHA256")
                or log.with_suffix(".exit").read_text().strip() != "0"
                or push_digest(log.read_text()) != digest):
            raise ValueError(f"{service}: raw push output/exit/digest differs from the receipt")
        image["registryDigest"] = digest
        image["registryReference"] = reference
        image["registryPublicationAttempt"] = producer_attempt


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--meta-dir", required=True, help="directory holding the per-service meta/*.json files")
    ap.add_argument("--sha", required=True, help="the candidate commit SHA every image must carry")
    ap.add_argument("--out", required=True)
    ap.add_argument("--build-tags", required=True)
    ap.add_argument("--platform", required=True)
    ap.add_argument("--environment", default="")
    ap.add_argument("--started-at", dest="started_at", default="")
    ap.add_argument("--acceptance-json", default="", help="optional JSON file with the acceptance-side fields")
    ap.add_argument("--registry-dir", type=Path, help="verified registry retention receipts and raw push logs from this run")
    ap.add_argument("--registry-repository", default="")
    ap.add_argument("--ci-run", default="")
    ap.add_argument("--ci-attempt", default="")
    args = ap.parse_args()
    if not args.registry_dir and any((args.registry_repository, args.ci_run, args.ci_attempt)):
        print("::error::registry publication: receipt directory is required with publication/CI arguments", file=sys.stderr)
        return 1

    build_tags = args.build_tags.split(",")
    if (not re.fullmatch(r"[0-9a-f]{40}", args.sha)
            or build_tags != ["nats_integration"] or not args.platform):
        print("::error::a full candidate SHA, nats_integration build tags and platform are required", file=sys.stderr)
        return 1

    images, problems = load_images(args.meta_dir)
    if problems:
        for problem in problems:
            print(f"::error::{problem}", file=sys.stderr)
        return 1
    if not images:
        print(f"::error::no candidate image metadata found under {args.meta_dir}", file=sys.stderr)
        return 1

    # Every image must belong to THIS candidate: a manifest that mixes SHAs would
    # let a job scan or deploy one build while believing it verified another.
    wrong = {svc: v["revision"] for svc, v in images.items() if v["revision"] != args.sha}
    if wrong:
        for svc, commit in sorted(wrong.items()):
            print(f"::error::{svc} was built from {commit}, not {args.sha}", file=sys.stderr)
        return 1
    for service, entry in images.items():
        if entry["buildTags"] != build_tags or entry["platform"] != args.platform:
            print(f"::error::{service}: actual build tags/platform differ from the requested candidate", file=sys.stderr)
            return 1
        if entry["registryDigest"] is not None:
            print(f"::error::{service}: build metadata cannot substitute for a registry publication receipt", file=sys.stderr)
            return 1

    registry_publication = {"status": "NOT_RECORDED"}
    if args.registry_dir:
        try:
            merge_publications(images, args.registry_dir, args.registry_repository, args.ci_run, args.ci_attempt)
        except (OSError, ValueError, KeyError, TypeError, AttributeError) as exc:
            print(f"::error::registry publication: {exc}", file=sys.stderr)
            return 1
        registry_publication = {"status": "RECORDED", "ciRun": args.ci_run, "ciAttempt": args.ci_attempt}

    acceptance = None
    if args.acceptance_json:
        with open(args.acceptance_json) as fh:
            acceptance = json.load(fh)

    manifest = {
        "schemaVersion": 2,
        "candidateSHA": args.sha,
        "revision": args.sha,
        "buildTags": build_tags,
        "platform": args.platform,
        "environment": args.environment or None,
        "startedAt": args.started_at or None,
        "images": images,
        "registryPublication": registry_publication,
        # Null unless the stage that produced them supplied them — never filled
        # with a plausible-looking substitute.
        "acceptance": acceptance,
    }

    with open(args.out, "w") as fh:
        json.dump(manifest, fh, indent=2, sort_keys=True)
        fh.write("\n")

    print(f"candidate manifest: {args.out} — {len(images)} image(s) at {args.sha}")
    for svc, v in sorted(images.items()):
        print(f"  {svc}: imageID={v['imageID']} archiveSHA256={v['archiveSHA256']} registryDigest={v['registryDigest']}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
