#!/usr/bin/env python3
"""Write the per-SHA candidate manifest (TRANSITION_PLAN.md §7).

The plan requires a structured manifest so every later job (scan, smoke,
deploy, promotion) can talk about "the candidate" by immutable identity instead
of by a tag that could point anywhere. build.yml already records one
`meta/<service>.json` per built image; this collects them into the single
`candidate-manifest.json` the plan describes:

    {candidateSHA, buildTags, platform, images: {service: imageDigest}, ...}

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

Exit codes: 0 = manifest written; 1 = a meta file is missing/malformed or no
image was found (a manifest that describes nothing must not be mistaken for a
verified candidate).
"""

import argparse
import json
import os
import sys


def load_images(meta_dir):
    images = {}
    problems = []
    if not os.path.isdir(meta_dir):
        return images, [f"{meta_dir} is not a directory"]
    for name in sorted(os.listdir(meta_dir)):
        if not name.endswith(".json"):
            continue
        service = name[: -len(".json")]
        path = os.path.join(meta_dir, name)
        try:
            with open(path) as fh:
                data = json.load(fh)
        except (OSError, ValueError) as exc:
            problems.append(f"{path}: {exc}")
            continue
        image_id = data.get("image_id")
        commit = data.get("commit")
        if not image_id:
            problems.append(f"{path}: no image_id recorded")
            continue
        if not commit:
            problems.append(f"{path}: no commit recorded")
            continue
        images[service] = {"imageDigest": image_id, "commit": commit}
    return images, problems


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--meta-dir", required=True, help="directory holding the per-service meta/*.json files")
    ap.add_argument("--sha", required=True, help="the candidate commit SHA every image must carry")
    ap.add_argument("--out", required=True)
    ap.add_argument("--build-tags", default="")
    ap.add_argument("--platform", default="")
    ap.add_argument("--environment", default="")
    ap.add_argument("--started-at", dest="started_at", default="")
    ap.add_argument("--acceptance-json", default="", help="optional JSON file with the acceptance-side fields")
    args = ap.parse_args()

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
    wrong = {svc: v["commit"] for svc, v in images.items() if v["commit"] != args.sha}
    if wrong:
        for svc, commit in sorted(wrong.items()):
            print(f"::error::{svc} was built from {commit}, not {args.sha}", file=sys.stderr)
        return 1

    acceptance = None
    if args.acceptance_json:
        with open(args.acceptance_json) as fh:
            acceptance = json.load(fh)

    manifest = {
        "candidateSHA": args.sha,
        "revision": args.sha,
        "buildTags": args.build_tags or None,
        "platform": args.platform or None,
        "environment": args.environment or None,
        "startedAt": args.started_at or None,
        "images": images,
        # Null unless the stage that produced them supplied them — never filled
        # with a plausible-looking substitute.
        "acceptance": acceptance,
    }

    with open(args.out, "w") as fh:
        json.dump(manifest, fh, indent=2, sort_keys=True)
        fh.write("\n")

    print(f"candidate manifest: {args.out} — {len(images)} image(s) at {args.sha}")
    for svc, v in sorted(images.items()):
        print(f"  {svc}: {v['imageDigest'][:19]}…")
    return 0


if __name__ == "__main__":
    sys.exit(main())
