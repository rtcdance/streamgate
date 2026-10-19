#!/usr/bin/env python3
"""Capture expected segment digests through storage, never the playback HTTP path."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import uuid


def capture(container, content_id, generation, quality, run=subprocess.run):
    if str(uuid.UUID(content_id)) != content_id or not isinstance(generation, int) or generation < 1:
        raise ValueError("a canonical content UUID and published generation are required")
    if not re.fullmatch(r"(?:[1-9][0-9]{1,3}x[1-9][0-9]{1,3}|(?:360|480|720|1080)p)", quality):
        raise ValueError("invalid fixed rendition")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}", container):
        raise ValueError("invalid storage container")
    prefix = f"streams/{content_id}/g{generation}/{quality}/"

    def command(*args):
        return run(["docker", "exec", container, *args], check=True, capture_output=True, timeout=30).stdout

    # Credentials remain inside the dedicated MinIO container and are not emitted.
    command("sh", "-ec", 'mc alias set local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null')
    listing = command("mc", "ls", "--recursive", "--json", "local/streamgate/" + prefix)
    names = set()
    for line in listing.splitlines():
        row = json.loads(line)
        if row.get("status") == "error":
            raise ValueError("storage listing failed")
        key = row.get("key", "")
        if key.endswith("/"):
            continue
        name = key.removeprefix(prefix)
        if not re.fullmatch(r"[A-Za-z0-9_.-]+\.(?:m3u8|ts)", name) or ".." in name:
            raise ValueError("unexpected published object path")
        if name in names:
            raise ValueError("duplicate published object")
        names.add(name)
    playlists = sorted(name for name in names if name.endswith(".m3u8"))
    if len(playlists) != 1:
        raise ValueError("exactly one stored rendition playlist is required")
    playlist = command("mc", "cat", "local/streamgate/" + prefix + playlists[0]).decode("utf-8")
    if not playlist.startswith("#EXTM3U") or "#EXTINF:" not in playlist:
        raise ValueError("stored rendition playlist has no media durations")
    uris = [line.strip() for line in playlist.splitlines() if line.strip() and not line.startswith("#")]
    hashes, objects = {}, []
    for name in dict.fromkeys(uris):
        if not re.fullmatch(r"[A-Za-z0-9_.-]+\.ts", name) or ".." in name or name not in names:
            raise ValueError("stored playlist references missing or unsafe media")
        body = command("mc", "cat", "local/streamgate/" + prefix + name)
        if not body:
            raise ValueError("empty fixture segment")
        digest = hashlib.sha256(body).hexdigest()
        hashes[f"/api/v1/streaming/{content_id}/segment/{name}"] = digest
        objects.append({"objectKey": prefix + name, "sha256": digest, "bytes": len(body)})
    if not hashes:
        raise ValueError("fixture has no referenced media")
    return hashes, objects


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--container", required=True)
    parser.add_argument("--content-id", required=True)
    parser.add_argument("--generation", required=True, type=int)
    parser.add_argument("--quality", required=True)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    hashes, objects = capture(args.container, args.content_id, args.generation, args.quality)
    args.output.write_text(json.dumps(hashes, sort_keys=True, indent=2) + "\n")
    args.output.chmod(0o600)
    evidence = {"contentID": args.content_id, "generation": args.generation, "quality": args.quality,
                "source": "MinIO published generation", "objects": objects,
                "hashFileSHA256": hashlib.sha256(args.output.read_bytes()).hexdigest()}
    args.output.with_suffix(".provenance.json").write_text(json.dumps(evidence, sort_keys=True, indent=2) + "\n")
    print(f"captured {len(hashes)} independent fixture segment hashes")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError) as exc:
        raise SystemExit(f"fixture capture failed: {exc}")
