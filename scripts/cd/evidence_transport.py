#!/usr/bin/env python3
"""Store and restore exact artifact bytes through an OCI registry image."""
import argparse
import glob
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import uuid

SCHEMA = 1
DIGEST_RE = re.compile(r"^sha256:[0-9a-f]{64}$")
SAFE_NAME_RE = re.compile(r"^[A-Za-z0-9._-]+$")


class TransportError(Exception):
    pass


def file_hash(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def bundle_hash(entries):
    ordered = sorted(entries, key=lambda item: item.get("path", ""))
    canonical = json.dumps(ordered, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(canonical).hexdigest()


def validate_entry_path(relative):
    # Artifact paths are untrusted input: reject absolute paths, traversal and
    # platform separators before they can reach a destination directory.
    if not relative or relative in (".", ".."):
        raise TransportError(f"invalid entry path: {relative!r}")
    if relative.startswith("/") or relative.startswith("\\"):
        raise TransportError(f"absolute entry path: {relative!r}")
    if "\\" in relative or "\x00" in relative:
        raise TransportError(f"non-portable entry path: {relative!r}")
    parts = relative.split("/")
    if any(part in ("", ".", "..") for part in parts):
        raise TransportError(f"escaping entry path: {relative!r}")
    return relative


def collect_root(specs):
    matched = []
    for spec in specs:
        hits = sorted(glob.glob(spec, recursive=True))
        matched.extend(hits)
    if not matched:
        raise FileNotFoundError("no files matched: " + ", ".join(specs))
    absolute = [os.path.abspath(item) for item in matched]
    if len(absolute) == 1 and os.path.isdir(absolute[0]):
        return absolute[0]
    common = os.path.commonpath(absolute)
    if os.path.isfile(common):
        return os.path.dirname(common)
    return common


def collect_entries(root):
    root = os.path.abspath(root)
    entries = []
    for current, directories, files in os.walk(root, followlinks=False):
        directories.sort()
        for name in sorted(directories):
            full = os.path.join(current, name)
            if os.path.islink(full):
                raise TransportError(f"symlink is not allowed: {full}")
        for name in sorted(files):
            full = os.path.join(current, name)
            info = os.lstat(full)
            if stat.S_ISLNK(info.st_mode):
                raise TransportError(f"symlink is not allowed: {full}")
            if not stat.S_ISREG(info.st_mode):
                raise TransportError(f"special file is not allowed: {full}")
            relative = os.path.relpath(full, root).replace(os.sep, "/")
            validate_entry_path(relative)
            entries.append({
                "path": relative,
                "mode": stat.S_IMODE(info.st_mode),
                "size": info.st_size,
                "sha256": file_hash(full),
            })
    if not entries:
        raise FileNotFoundError(f"no regular files under {root}")
    entries.sort(key=lambda item: item["path"])
    return entries


def write_context(root, entries, manifest, context):
    payload = context / "payload"
    payload.mkdir(parents=True)
    for entry in entries:
        source = Path(root) / entry["path"]
        target = payload / entry["path"]
        target.parent.mkdir(parents=True, exist_ok=True)
        try:
            os.link(source, target)
        except OSError:
            shutil.copy2(source, target)
    (context / "transport.json").write_text(json.dumps(manifest, sort_keys=True, indent=2) + "\n")
    (context / "Dockerfile").write_text(
        "FROM scratch\nCOPY payload/ /payload/\nCOPY transport.json /transport.json\n"
    )


def run_docker(*args, capture=True):
    result = subprocess.run(["docker", *args], text=True, capture_output=capture)
    if result.returncode:
        raise TransportError(f"docker {' '.join(args)} failed: {(result.stderr or result.stdout).strip()}")
    return result.stdout


def push_image(context, image, tag, source_url, sha, name, bundle):
    reference = f"{image}:{tag}"
    labels = [
        f"org.opencontainers.image.source={source_url}",
        f"org.opencontainers.image.revision={sha}",
        f"io.streamgate.evidence.name={name}",
        f"io.streamgate.evidence.bundle={bundle}",
    ]
    command = ["build", "-t", reference]
    for label in labels:
        command.extend(["--label", label])
    command.append(str(context))
    run_docker(*command)
    run_docker("push", reference)
    repo_digests = json.loads(run_docker("image", "inspect", reference, "--format", "{{json .RepoDigests}}"))
    if not repo_digests:
        raise TransportError("pushed image has no repo digest")
    digest = repo_digests[0].rsplit("@", 1)[-1]
    if not DIGEST_RE.match(digest):
        raise TransportError(f"unexpected digest: {digest}")
    return reference, digest


def verify_manifest(manifest, expect_name, expect_sha, expect_run, expect_attempt):
    if manifest.get("schemaVersion") != SCHEMA:
        raise TransportError("unsupported transport schema")
    if expect_name is not None and manifest.get("name") != expect_name:
        raise TransportError(f"artifact name mismatch: {manifest.get('name')!r}")
    if expect_sha is not None and manifest.get("sourceSHA") != expect_sha:
        raise TransportError("source SHA mismatch")
    if expect_run is not None and str(manifest.get("runID")) != str(expect_run):
        raise TransportError("producer run mismatch")
    if expect_attempt is not None and str(manifest.get("runAttempt")) != str(expect_attempt):
        raise TransportError("producer attempt mismatch")
    entries = manifest.get("entries")
    if not isinstance(entries, list) or not entries:
        raise TransportError("manifest has no entries")
    seen = set()
    for entry in entries:
        validate_entry_path(entry.get("path", ""))
        if entry["path"] in seen:
            raise TransportError(f"duplicate entry: {entry['path']}")
        seen.add(entry["path"])
    if bundle_hash(entries) != manifest.get("bundleSHA256"):
        raise TransportError("bundle hash mismatch")
    return entries


def verify_tree(root, entries):
    expected = {entry["path"]: entry for entry in entries}
    actual = set()
    for current, directories, files in os.walk(root, followlinks=False):
        for name in directories + files:
            full = os.path.join(current, name)
            if os.path.islink(full):
                raise TransportError(f"symlink in restored tree: {full}")
        for name in files:
            full = os.path.join(current, name)
            relative = os.path.relpath(full, root).replace(os.sep, "/")
            actual.add(relative)
            entry = expected.get(relative)
            if entry is None:
                raise TransportError(f"unexpected restored file: {relative}")
            info = os.lstat(full)
            if not stat.S_ISREG(info.st_mode):
                raise TransportError(f"restored entry is not a regular file: {relative}")
            if info.st_size != entry["size"]:
                raise TransportError(f"size mismatch: {relative}")
            if file_hash(full) != entry["sha256"]:
                raise TransportError(f"SHA256 mismatch: {relative}")
    missing = set(expected) - actual
    if missing:
        raise TransportError(f"missing restored files: {sorted(missing)}")


def extract(reference, local):
    if not local:
        run_docker("pull", reference)
    container = "sg-evidence-" + uuid.uuid4().hex[:12]
    # Scratch images carry no command; create only records one and never runs it.
    run_docker("create", "--name", container, reference, "/nonexistent")
    work = Path(tempfile.mkdtemp(prefix="sg-evidence-"))
    try:
        run_docker("cp", f"{container}:/transport.json", str(work / "transport.json"))
        payload = work / "payload"
        payload.mkdir()
        run_docker("cp", f"{container}:/payload/.", str(payload))
        manifest = json.loads((work / "transport.json").read_text())
        return manifest, payload, work
    finally:
        subprocess.run(["docker", "rm", "-f", container], capture_output=True, text=True)


def resolve_digest(reference, local):
    if local:
        return None
    if "@sha256:" in reference:
        return reference.rsplit("@", 1)[-1]
    info = json.loads(run_docker("image", "inspect", reference, "--format", "{{json .RepoDigests}}"))
    if not info:
        raise TransportError("pulled image has no repo digest")
    return info[0].rsplit("@", 1)[-1]


def copy_payload(source, destination):
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=True)
    for item in sorted(Path(source).rglob("*")):
        if item.is_dir():
            continue
        relative = item.relative_to(source)
        target = destination / relative
        if target.exists():
            raise TransportError(f"merge collision: {relative}")
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(item, target)


def command_put(args):
    try:
        root = collect_root(args.path)
    except FileNotFoundError as error:
        if args.if_no_files_found == "error":
            raise TransportError(str(error))
        if args.if_no_files_found == "warn":
            print(f"evidence transport: {error}", file=sys.stderr)
        print(json.dumps({"status": "skipped", "reason": str(error)}))
        return 0
    entries = collect_entries(root)
    manifest = {
        "schemaVersion": SCHEMA,
        "name": args.name,
        "sourceSHA": args.source_sha,
        "runID": str(args.run_id),
        "runAttempt": str(args.attempt),
        "workflow": args.workflow,
        "job": args.job,
        "entries": entries,
        "bundleSHA256": bundle_hash(entries),
    }
    context = Path(args.context or tempfile.mkdtemp(prefix="sg-evidence-context-"))
    if context.exists() and any(context.iterdir()):
        raise TransportError(f"context directory is not empty: {context}")
    context.mkdir(parents=True, exist_ok=True)
    write_context(root, entries, manifest, context)
    result = {
        "name": args.name,
        "sourceSHA": args.source_sha,
        "runID": str(args.run_id),
        "runAttempt": str(args.attempt),
        "bundleSHA256": manifest["bundleSHA256"],
        "entries": len(entries),
        "bytes": sum(entry["size"] for entry in entries),
        "context": str(context),
    }
    if args.prepare_only:
        result["status"] = "prepared"
        print(json.dumps(result, sort_keys=True))
        return 0
    reference = f"{args.image}:{args.tag}"
    if args.no_push:
        run_docker("build", "-t", reference, str(context))
        info = json.loads(run_docker("image", "inspect", reference, "--format", "{{json .}}"))
        result.update({"image": reference, "imageID": info["Id"], "digest": None, "status": "built"})
    else:
        reference, digest = push_image(
            context, args.image, args.tag, args.source_url, args.source_sha, args.name, manifest["bundleSHA256"]
        )
        result.update({"image": reference, "digest": digest, "status": "pushed"})
    print(json.dumps(result, sort_keys=True))
    return 0


def command_get(args):
    destination = Path(args.dest)
    if destination.exists() and any(destination.iterdir()) and not args.merge:
        raise TransportError(f"destination is not empty: {destination}")
    digests = []
    restored = 0
    for reference in args.image:
        manifest, payload, work = extract(reference, args.local)
        try:
            entries = verify_manifest(
                manifest, args.expect_name, args.expect_sha, args.expect_run, args.expect_attempt
            )
            verify_tree(payload, entries)
            target = destination if (args.merge or len(args.image) == 1) else destination / manifest["name"]
            copy_payload(payload, target)
            restored += len(entries)
            digests.append({
                "name": manifest["name"],
                "image": reference,
                "digest": resolve_digest(reference, args.local),
                "sourceSHA": manifest["sourceSHA"],
                "runID": manifest["runID"],
                "runAttempt": manifest["runAttempt"],
                "bundleSHA256": manifest["bundleSHA256"],
            })
        finally:
            shutil.rmtree(work, ignore_errors=True)
    print(json.dumps({"status": "restored", "files": restored, "artifacts": digests}, sort_keys=True))
    return 0


def build_parser():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    put = commands.add_parser("put", help="store exact artifact bytes in a registry image")
    put.add_argument("--name", required=True)
    put.add_argument("--path", action="append", required=True)
    put.add_argument("--image", required=True)
    put.add_argument("--tag", required=True)
    put.add_argument("--source-sha", required=True)
    put.add_argument("--run-id", required=True)
    put.add_argument("--attempt", required=True)
    put.add_argument("--workflow", default="")
    put.add_argument("--job", default="")
    put.add_argument("--source-url", default="https://github.com/rtcdance/streamgate")
    put.add_argument("--if-no-files-found", choices=["ignore", "warn", "error"], default="error")
    put.add_argument("--context")
    put.add_argument("--prepare-only", action="store_true")
    put.add_argument("--no-push", action="store_true")
    put.set_defaults(func=command_put)
    get = commands.add_parser("get", help="restore and verify artifact bytes")
    get.add_argument("--image", action="append", required=True)
    get.add_argument("--dest", required=True)
    get.add_argument("--merge", action="store_true")
    get.add_argument("--local", action="store_true")
    get.add_argument("--expect-name")
    get.add_argument("--expect-sha")
    get.add_argument("--expect-run")
    get.add_argument("--expect-attempt")
    get.set_defaults(func=command_get)
    return parser


def main(argv=None):
    args = build_parser().parse_args(argv)
    try:
        name = getattr(args, "name", None)
        if name is not None and not SAFE_NAME_RE.match(name):
            raise TransportError("artifact name must match [A-Za-z0-9._-]+")
        return args.func(args)
    except (TransportError, FileNotFoundError) as error:
        print(f"evidence transport: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
