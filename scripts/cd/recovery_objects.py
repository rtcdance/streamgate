#!/usr/bin/env python3
"""Back up and verify logical MinIO objects; raw backups stay in a private directory."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile


def object_key(key):
    if (not isinstance(key, str) or not key or len(key.encode()) > 1024
            or any(ord(char) < 32 or ord(char) == 127 for char in key) or "\\" in key
            or any(part in ("", ".", "..") for part in key.split("/"))
            or key.split("/")[0] == ".minio.sys"):
        raise ValueError("unsafe logical object key")
    return key


def listing(store):
    rows = store.objects()
    seen = set()
    for row in rows:
        key = object_key(row["key"])
        if key in seen or type(row["bytes"]) is not int or row["bytes"] < 0:
            raise ValueError("duplicate object or invalid size")
        seen.add(key)
    return sorted(rows, key=lambda row: row["key"])


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def capture(store, directory):
    rows = listing(store)
    if not rows:
        raise ValueError("empty object snapshot")
    result = []
    for index, row in enumerate(rows):
        path = directory / f"{index:06d}.bin"
        descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        os.close(descriptor)
        store.download(row["key"], path)
        if path.stat().st_size != row["bytes"]:
            raise ValueError("object size changed during backup")
        result.append({**row, "sha256": digest(path), "file": path.name})
    if listing(store) != rows:
        raise ValueError("object inventory changed during backup")
    return result


def public_inventory(rows):
    return [{key: row[key] for key in ("key", "bytes", "sha256")} for row in rows]


def backup(store, directory):
    directory.mkdir(mode=0o700)
    rows = capture(store, directory)
    path = directory / "manifest.json"
    with path.open("x") as stream:
        path.chmod(0o600)
        json.dump({"bucket": "streamgate", "objects": rows}, stream, sort_keys=True)
    return public_inventory(rows)


def load_backup(directory):
    manifest = directory / "manifest.json"
    if manifest.is_symlink():
        raise ValueError("backup manifest may not be a symlink")
    data = json.loads(manifest.read_text())
    if set(data) != {"bucket", "objects"} or data["bucket"] != "streamgate" or not data["objects"]:
        raise ValueError("invalid logical backup manifest")
    rows = data["objects"]
    names, keys = {"manifest.json"}, set()
    for index, row in enumerate(rows):
        if set(row) != {"key", "bytes", "sha256", "file"}:
            raise ValueError("invalid object record")
        key = object_key(row["key"])
        if key in keys or row["file"] != f"{index:06d}.bin":
            raise ValueError("duplicate key or unsafe backup filename")
        keys.add(key); names.add(row["file"])
        if type(row["bytes"]) is not int or row["bytes"] < 0 or not re.fullmatch(r"[0-9a-f]{64}", row["sha256"]):
            raise ValueError("invalid object identity")
        path = directory / row["file"]
        if path.is_symlink() or not path.is_file() or path.stat().st_size != row["bytes"] or digest(path) != row["sha256"]:
            raise ValueError("missing or corrupt backup object")
    if {path.name for path in directory.iterdir()} != names:
        raise ValueError("unlisted file in backup")
    return rows


def restore(store, directory):
    rows = load_backup(directory)
    store.prepare_empty()
    for row in rows:
        store.upload(row["key"], directory / row["file"])


def inventory(store):
    with tempfile.TemporaryDirectory(prefix="streamgate-recovery-objects-") as directory:
        return public_inventory(capture(store, Path(directory)))


class DockerStore:
    def __init__(self, container):
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}", container):
            raise ValueError("invalid storage container")
        self.container = container
        self.command("sh", "-ec", 'mc alias set recovery http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null')

    def command(self, *args, **kwargs):
        return subprocess.run(["docker", "exec", "-i", self.container, *args],
                              check=True, timeout=60, stderr=subprocess.PIPE, **kwargs)

    def objects(self):
        raw = self.command("mc", "ls", "--recursive", "--json", "recovery/streamgate/", stdout=subprocess.PIPE).stdout
        rows = []
        for line in raw.splitlines():
            row = json.loads(line)
            if row.get("status") != "success":
                raise ValueError("logical object listing failed")
            if row.get("type") == "folder":
                continue
            rows.append({"key": row["key"], "bytes": row["size"]})
        return rows

    def download(self, key, path):
        with path.open("wb") as output:
            self.command("mc", "cat", "recovery/streamgate/" + object_key(key), stdout=output)

    def prepare_empty(self):
        self.command("mc", "mb", "--ignore-existing", "recovery/streamgate", stdout=subprocess.PIPE)
        if listing(self):
            raise ValueError("target logical bucket is not empty")

    def upload(self, key, path):
        with path.open("rb") as source:
            self.command("mc", "pipe", "recovery/streamgate/" + object_key(key), stdin=source, stdout=subprocess.PIPE)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("backup", "restore", "inventory"))
    parser.add_argument("--container", required=True)
    parser.add_argument("--directory", type=Path)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    store = DockerStore(args.container)
    if args.mode == "restore":
        if args.directory is None: parser.error("--directory is required")
        restore(store, args.directory)
        return
    if args.output is None: parser.error("--output is required")
    if args.mode == "backup":
        if args.directory is None: parser.error("--directory is required")
        rows = backup(store, args.directory)
    else:
        rows = inventory(store)
    args.output.write_text(json.dumps(rows, sort_keys=True, indent=2) + "\n")
    print(f"captured {len(rows)} logical object identities")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, TypeError, OSError, subprocess.SubprocessError) as exc:
        # Raw tool stderr or command output may contain credentials or object bytes.
        raise SystemExit(f"logical object recovery failed ({type(exc).__name__})") from None
