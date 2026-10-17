#!/usr/bin/env python3
"""Fail the overall entry until every candidate acceptance gate has evidence."""
import json
from pathlib import Path
import sys

from candidate_manifest import file_hash


def validate(root):
    root = root.resolve()
    candidate = json.loads((root / "candidate.json").read_text())
    index = json.loads((root / "candidate-gates.json").read_text())
    if index.get("candidateSHA") != candidate["revision"]:
        raise ValueError("acceptance index does not identify this candidate")
    image_ids = {entry["imageID"] for entry in candidate["images"].values()}
    gates = index["gates"]
    if len(gates) != 9 or {gate["id"] for gate in gates} != {f"M{i}" for i in range(9)}:
        raise ValueError("M0-M8 must each be present exactly once")
    for gate in gates:
        if gate.get("status") != "PASS" or gate.get("candidateSHA") != candidate["revision"]:
            raise ValueError(f"{gate['id']}: incomplete, skipped, failed or wrong candidate")
        if gate.get("imageID") not in image_ids:
            raise ValueError(f"{gate['id']}: wrong candidate image")
        if not gate.get("command") or not gate.get("scenarios") or not gate.get("artifacts"):
            raise ValueError(f"{gate['id']}: missing commands, scenarios or raw evidence")
        for artifact in gate["artifacts"]:
            path = (root / artifact["path"]).resolve()
            if not path.is_relative_to(root):
                raise ValueError("artifact escapes this run's directory")
            if path.stat().st_size == 0:
                raise ValueError(f"{gate['id']}: empty raw evidence")
            digest = file_hash(path)
            if digest != artifact["sha256"]:
                raise ValueError(f"{gate['id']}: evidence hash mismatch")


def main():
    try:
        validate(Path(sys.argv[1]))
    except (OSError, ValueError, KeyError, TypeError, IndexError) as exc:
        print(f"OVERALL ACCEPTANCE FAILURE: {exc}", file=sys.stderr)
        return 1
    print("All M0-M8 candidate evidence records are present and consistent")
    return 0


if __name__ == "__main__":
    sys.exit(main())
