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
import time
import uuid

SCHEMA = 1
DIGEST_RE = re.compile(r"^sha256:[0-9a-f]{64}$")
SAFE_NAME_RE = re.compile(r"^[A-Za-z0-9._-]+$")
REGISTRY_ATTEMPTS = 3
REGISTRY_BACKOFF_SECONDS = 5
REGISTRY_TIMEOUT_SECONDS = 900


class TransportError(Exception):
    pass


class RegistryTimeoutError(TransportError):
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


def validate_name(name):
    if not isinstance(name, str) or name in (".", "..") or not SAFE_NAME_RE.match(name):
        raise TransportError(f"invalid artifact name: {name!r}")
    return name


def _glob_regex(pattern):
    # Artifact globbing: '*' stays inside one path segment, '**' crosses
    # directories, and '**/' also matches zero directories. Exclusions are
    # matched against the layout-relative path, exactly like the negated
    # patterns actions/upload-artifact accepts.
    pieces = []
    index = 0
    while index < len(pattern):
        if pattern.startswith("**/", index):
            pieces.append("(?:.*/)?")
            index += 3
        elif pattern.startswith("**", index):
            pieces.append(".*")
            index += 2
        elif pattern[index] == "*":
            pieces.append("[^/]*")
            index += 1
        elif pattern[index] == "?":
            pieces.append("[^/]")
            index += 1
        else:
            pieces.append(re.escape(pattern[index]))
            index += 1
    return re.compile("^" + "".join(pieces) + "$")


def is_excluded(relative, patterns):
    return any(pattern.match(relative) for pattern in patterns)


def is_hidden(relative):
    return any(part.startswith(".") for part in relative.split("/"))


def write_github_output(path, values):
    if not path:
        return
    with open(path, "a", encoding="utf-8") as stream:
        for key, value in values.items():
            stream.write(f"{key}={value}\n")


def _file_entry(full, root):
    info = os.lstat(full)
    if stat.S_ISLNK(info.st_mode):
        raise TransportError(f"symlink is not allowed: {full}")
    if not stat.S_ISREG(info.st_mode):
        raise TransportError(f"special file is not allowed: {full}")
    relative = os.path.relpath(full, root).replace(os.sep, "/")
    validate_entry_path(relative)
    return {
        "path": relative,
        "mode": stat.S_IMODE(info.st_mode),
        "size": info.st_size,
        "sha256": file_hash(full),
    }


def _walk_files(directory):
    for current, directories, files in os.walk(directory, followlinks=False):
        directories.sort()
        for name in sorted(directories):
            full = os.path.join(current, name)
            if os.path.islink(full):
                raise TransportError(f"symlink is not allowed: {full}")
        for name in sorted(files):
            yield os.path.join(current, name)


def collect_selection(specs, excludes=(), include_hidden=False):
    # The matched set is the authorization boundary; the layout root only
    # decides relative names and never widens what gets packaged. Exclusions and
    # the hidden-file default mirror actions/upload-artifact, and an unmatched
    # spec is not fatal on its own: the missing-files policy applies to the
    # total selection, exactly like the action.
    matched = []
    selection = []
    for spec in specs:
        hits = sorted(glob.glob(spec, recursive=True))
        selection.append({"spec": spec, "matches": len(hits)})
        matched.extend(os.path.abspath(hit) for hit in hits)
    unique = sorted(set(matched))
    if not unique:
        raise FileNotFoundError("no files matched: " + ", ".join(specs))
    for path in unique:
        if os.path.islink(path):
            raise TransportError(f"symlink is not allowed: {path}")
        if not (os.path.isfile(path) or os.path.isdir(path)):
            raise TransportError(f"unsupported source: {path}")
    if len(unique) == 1 and os.path.isdir(unique[0]):
        layout_root = unique[0]
    else:
        layout_root = os.path.commonpath(unique)
        if os.path.isfile(layout_root):
            layout_root = os.path.dirname(layout_root)
    patterns = [_glob_regex(pattern) for pattern in excludes]
    entries = {}
    excluded = 0
    hidden = 0
    for path in unique:
        files = _walk_files(path) if os.path.isdir(path) else [path]
        for full in files:
            # Filter before hashing: excluded bytes are never read.
            relative = os.path.relpath(full, layout_root).replace(os.sep, "/")
            validate_entry_path(relative)
            if is_excluded(relative, patterns):
                excluded += 1
                continue
            if not include_hidden and is_hidden(relative):
                hidden += 1
                continue
            entry = _file_entry(full, layout_root)
            entries[entry["path"]] = entry
    if not entries:
        raise FileNotFoundError("no regular files matched: " + ", ".join(specs))
    report = {
        "specs": selection,
        "excluded": excluded,
        "hiddenExcluded": hidden,
        "includeHidden": include_hidden,
    }
    return layout_root, [entries[key] for key in sorted(entries)], report


def collect_sources(specs):
    root, entries, _ = collect_selection(specs)
    return root, entries


def collect_entries(root):
    root = os.path.abspath(root)
    entries = [_file_entry(full, root) for full in _walk_files(root)]
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


def run_docker(*args, capture=True, timeout=None):
    try:
        result = subprocess.run(["docker", *args], text=True, capture_output=capture, timeout=timeout)
    except subprocess.TimeoutExpired as error:
        # Killing the CLI bounds the caller, but a remote daemon may continue
        # transferring. Do not start another transfer until it is inspected.
        raise RegistryTimeoutError(
            f"docker {args[0]} timed out after {timeout}s; client terminated; "
            "inspect daemon transfer activity before retrying"
        ) from error
    if result.returncode:
        raise TransportError(f"docker {' '.join(args)} failed: {(result.stderr or result.stdout).strip()}")
    return result.stdout


def run_registry(*args):
    value = os.environ.get("STREAMGATE_EVIDENCE_REGISTRY_TIMEOUT_SECONDS", str(REGISTRY_TIMEOUT_SECONDS))
    if not re.fullmatch(r"[1-9][0-9]*", value) or int(value) > 3600:
        raise TransportError("STREAMGATE_EVIDENCE_REGISTRY_TIMEOUT_SECONDS must be an integer in 1..3600")
    timeout = int(value)
    # Retry only transient failures. Authentication/identity failures cannot be
    # repaired by repeating a command; timeouts may leave daemon work in flight.
    for attempt in range(1, REGISTRY_ATTEMPTS + 1):
        try:
            return run_docker(*args, timeout=timeout)
        except RegistryTimeoutError:
            raise
        except TransportError as error:
            transient = re.search(
                r"\bEOF\b|connection reset|broken pipe|connection refused|"
                r"TLS handshake timeout|i/o timeout|temporary failure|"
                r"temporarily unavailable|unexpected status.*\b(?:429|5\d\d)\b|"
                r"\b(?:502 Bad Gateway|503 Service Unavailable|504 Gateway Timeout)\b",
                str(error), re.IGNORECASE,
            )
            permanent = re.search(
                r"unauthorized|forbidden|authentication required|access denied|"
                r"digest mismatch|manifest unknown|invalid reference|x509",
                str(error), re.IGNORECASE,
            )
            if attempt == REGISTRY_ATTEMPTS or permanent or not transient:
                raise
            print(f"evidence transport: {' '.join(args)} attempt {attempt} failed: {error}",
                  file=sys.stderr)
            time.sleep(REGISTRY_BACKOFF_SECONDS * attempt)


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
    run_registry("push", reference)
    repo_digests = json.loads(run_docker("image", "inspect", reference, "--format", "{{json .RepoDigests}}"))
    if not repo_digests:
        raise TransportError("pushed image has no repo digest")
    digest = repo_digests[0].rsplit("@", 1)[-1]
    if not DIGEST_RE.match(digest):
        raise TransportError(f"unexpected digest: {digest}")
    return reference, digest


def verify_manifest(manifest, expect_name, expect_sha, expect_run, expect_attempt, max_attempt=None):
    if manifest.get("schemaVersion") != SCHEMA:
        raise TransportError("unsupported transport schema")
    validate_name(manifest.get("name"))
    if expect_name is not None and manifest.get("name") != expect_name:
        raise TransportError(f"artifact name mismatch: {manifest.get('name')!r}")
    if expect_sha is not None and manifest.get("sourceSHA") != expect_sha:
        raise TransportError("source SHA mismatch")
    if expect_run is not None and str(manifest.get("runID")) != str(expect_run):
        raise TransportError("producer run mismatch")
    if expect_attempt is not None and str(manifest.get("runAttempt")) != str(expect_attempt):
        raise TransportError("producer attempt mismatch")
    if max_attempt is not None:
        # A failed-job rerun may consume a producer that succeeded in an
        # earlier attempt of the same run; the producer's real attempt is
        # preserved and only bounded, never overwritten by the consumer's.
        attempt = str(manifest.get("runAttempt"))
        if not re.fullmatch(r"[1-9][0-9]*", attempt) or int(attempt) > int(max_attempt):
            raise TransportError(f"producer attempt {attempt} is not within 1..{max_attempt}")
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
        # The transport image is FROM scratch and carries only JSON/payload, so its
        # content is architecture independent, but it is built on the amd64 runner
        # and is therefore single-arch. The acceptance also runs on arm64 macOS,
        # where an unprefixed pull fails with "no matching manifest for
        # linux/arm64/v8", so request the one platform the image actually has.
        run_registry("pull", "--platform", "linux/amd64", reference)
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


def ensure_safe_directory(path):
    # Only the platform mount root (the first component) may be a symlink
    # (/var, /tmp on macOS). Every component below it is opened with O_NOFOLLOW,
    # so a symlink at the declared destination or any descendant cannot redirect
    # a write. The returned descriptor is the trust anchor for all restores.
    absolute = os.path.abspath(path)
    if os.path.islink(absolute):
        raise TransportError(f"destination is a symlink: {absolute}")
    parts = Path(absolute).parts
    if len(parts) < 2:
        raise TransportError("refusing to restore into the filesystem root")
    anchor = os.path.realpath(parts[0] + parts[1])
    descriptor = os.open(anchor, os.O_RDONLY | os.O_DIRECTORY)
    try:
        for part in parts[2:]:
            try:
                os.mkdir(part, 0o755, dir_fd=descriptor)
            except FileExistsError:
                pass
            next_descriptor = os.open(
                part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=descriptor
            )
            os.close(descriptor)
            descriptor = next_descriptor
        return descriptor
    except OSError as error:
        os.close(descriptor)
        raise TransportError(f"unsafe destination {absolute}: {error}") from error
    except BaseException:
        os.close(descriptor)
        raise


def copy_payload(source, destination=None, root_descriptor=None, subpath=""):
    owns_root = root_descriptor is None
    if owns_root:
        if destination is None:
            raise TransportError("destination or root descriptor is required")
        root_descriptor = ensure_safe_directory(destination)
    try:
        base_descriptor = os.dup(root_descriptor)
        try:
            for part in [piece for piece in subpath.split("/") if piece]:
                validate_name(part)
                try:
                    os.mkdir(part, 0o755, dir_fd=base_descriptor)
                except FileExistsError:
                    pass
                next_descriptor = os.open(
                    part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=base_descriptor
                )
                os.close(base_descriptor)
                base_descriptor = next_descriptor
            for item in sorted(Path(source).rglob("*")):
                info = os.lstat(item)
                if stat.S_ISLNK(info.st_mode):
                    raise TransportError(f"symlink is not allowed: {item}")
                if stat.S_ISDIR(info.st_mode):
                    continue
                if not stat.S_ISREG(info.st_mode):
                    raise TransportError(f"special file is not allowed: {item}")
                relative = item.relative_to(source).as_posix()
                validate_entry_path(relative)
                parts = relative.split("/")
                parent_descriptor = os.dup(base_descriptor)
                try:
                    for part in parts[:-1]:
                        try:
                            os.mkdir(part, 0o755, dir_fd=parent_descriptor)
                        except FileExistsError:
                            pass
                        next_descriptor = os.open(
                            part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent_descriptor
                        )
                        os.close(parent_descriptor)
                        parent_descriptor = next_descriptor
                    try:
                        file_descriptor = os.open(
                            parts[-1],
                            os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                            0o644,
                            dir_fd=parent_descriptor,
                        )
                    except FileExistsError as error:
                        raise TransportError(f"merge collision: {relative}") from error
                    try:
                        with item.open("rb") as stream:
                            for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                                view = memoryview(chunk)
                                while view:
                                    written = os.write(file_descriptor, view)
                                    view = view[written:]
                    finally:
                        os.close(file_descriptor)
                except OSError as error:
                    raise TransportError(f"unsafe destination for {relative}: {error}") from error
                finally:
                    os.close(parent_descriptor)
        finally:
            os.close(base_descriptor)
    finally:
        if owns_root:
            os.close(root_descriptor)


def command_put(args):
    try:
        root, entries, selection = collect_selection(
            args.path, excludes=args.exclude, include_hidden=args.include_hidden
        )
    except FileNotFoundError as error:
        if args.if_no_files_found == "error":
            raise TransportError(str(error))
        if args.if_no_files_found == "warn":
            print(f"evidence transport: {error}", file=sys.stderr)
        write_github_output(args.github_output, {"status": "skipped", "digest": "", "tag": "", "files": "0"})
        print(json.dumps({"status": "skipped", "reason": str(error)}))
        return 0
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
        "selection": selection,
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
        "selection": selection,
    }
    if args.prepare_only:
        result["status"] = "prepared"
        write_github_output(args.github_output, {"status": "prepared", "digest": "", "tag": args.tag,
                                                 "files": str(len(entries)), "bytes": str(result["bytes"])})
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
    write_github_output(args.github_output, {
        "status": result["status"],
        "digest": result.get("digest") or "",
        "image": reference,
        "tag": args.tag,
        "files": str(len(entries)),
        "bytes": str(result["bytes"]),
    })
    print(json.dumps(result, sort_keys=True))
    return 0


def split_names(value):
    if not value:
        return []
    return [item.strip() for item in value.split(",") if item.strip()]


def command_get(args):
    destination = Path(args.dest)
    if destination.exists() and any(destination.iterdir()) and not args.merge:
        raise TransportError(f"destination is not empty: {destination}")
    if args.require_digest_refs:
        for reference in args.image:
            if "@sha256:" not in reference or not DIGEST_RE.match(reference.rsplit("@", 1)[-1]):
                raise TransportError(f"image reference is not digest-pinned: {reference}")
    expected_names = split_names(args.expect_names)
    allowed_extra = split_names(args.allow_extra_names)
    for name in expected_names + allowed_extra:
        validate_name(name)
    # Phase 1: pull, verify and stage EVERY artifact before writing anything, so
    # a wrong fan-in binding cannot leave a half-restored tree behind.
    staged = []
    try:
        for reference in args.image:
            manifest, payload, work = extract(reference, args.local)
            staged.append({"manifest": manifest, "payload": payload, "work": work, "reference": reference})
            entries = verify_manifest(
                manifest, args.expect_name, args.expect_sha, args.expect_run,
                args.expect_attempt, max_attempt=args.max_attempt,
            )
            verify_tree(payload, entries)
            staged[-1]["entries"] = entries
            staged[-1]["digest"] = resolve_digest(reference, args.local)
        names = [validate_name(item["manifest"]["name"]) for item in staged]
        if len(set(names)) != len(names):
            raise TransportError(f"duplicate artifact names: {sorted(names)}")
        if expected_names:
            missing = sorted(set(expected_names) - set(names))
            if missing:
                raise TransportError(f"missing expected artifacts: {missing}")
            unexpected = sorted(set(names) - set(expected_names) - set(allowed_extra))
            if unexpected:
                raise TransportError(f"artifacts outside the declared set: {unexpected}")
        # Phase 2: anchor the declared root once; every artifact target is
        # created below this descriptor with O_NOFOLLOW, so a symlinked --dest
        # can never be re-trusted.
        root_descriptor = ensure_safe_directory(destination)
        digests = []
        restored = 0
        try:
            for item in staged:
                manifest = item["manifest"]
                subpath = "" if (args.merge or len(staged) == 1) else manifest["name"]
                copy_payload(item["payload"], root_descriptor=root_descriptor, subpath=subpath)
                restored += len(item["entries"])
                digests.append({
                    "name": manifest["name"],
                    "image": item["reference"],
                    "digest": item["digest"],
                    "sourceSHA": manifest["sourceSHA"],
                    "runID": manifest["runID"],
                    "runAttempt": manifest["runAttempt"],
                    "bundleSHA256": manifest["bundleSHA256"],
                })
        finally:
            os.close(root_descriptor)
    finally:
        for item in staged:
            shutil.rmtree(item["work"], ignore_errors=True)
    write_github_output(args.github_output, {
        "status": "restored",
        "files": str(restored),
        "names": ",".join(item["name"] for item in digests),
        "attempts": ",".join(item["runAttempt"] for item in digests),
    })
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
    put.add_argument("--exclude", action="append", default=[])
    put.add_argument("--include-hidden", action="store_true")
    put.add_argument("--github-output")
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
    get.add_argument("--max-attempt")
    get.add_argument("--expect-names", help="comma-separated names every restore must contain")
    get.add_argument("--allow-extra-names", help="comma-separated names the restore may also contain")
    get.add_argument("--require-digest-refs", action="store_true")
    get.add_argument("--github-output")
    get.set_defaults(func=command_get)
    return parser


def main(argv=None):
    args = build_parser().parse_args(argv)
    try:
        name = getattr(args, "name", None)
        if name is not None:
            validate_name(name)
        return args.func(args)
    except (TransportError, FileNotFoundError) as error:
        print(f"evidence transport: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
