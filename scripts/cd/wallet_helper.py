#!/usr/bin/env python3
"""Preserve and verify the original CI-built static wallet gRPC client."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import struct


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def static_amd64(raw):
    # A copied executable must run in the accepted app image without an added
    # loader or libraries; checking just ELF magic would also admit ARM/dynamic.
    if len(raw) < 64 or raw[:7] != b'\x7fELF\x02\x01\x01':
        raise ValueError('static Linux amd64 ELF client required')
    kind, machine, version = struct.unpack_from('<HHI', raw, 16)
    offset = struct.unpack_from('<Q', raw, 32)[0]
    width, count = struct.unpack_from('<HH', raw, 54)
    if kind not in (2, 3) or machine != 62 or version != 1 or width != 56 or not count or offset + width * count > len(raw):
        raise ValueError('invalid Linux amd64 client program headers')
    if any(struct.unpack_from('<I', raw, offset + width * i)[0] == 3 for i in range(count)):
        raise ValueError('wallet client requires a dynamic loader')


def identity(raw, source, candidate):
    if not re.fullmatch('[0-9a-f]{40}', candidate):
        raise ValueError('full frozen candidate required for client export')
    static_amd64(raw)
    return dict(schemaVersion=1, scope='CANDIDATE_HELPER_BUILD', candidateSHA=candidate,
                platform='linux/amd64', binaryPath='walletgrpc-linux', sha256=sha(raw),
                sourceSHA256=sha(source), compilerExit=0,
                buildFlags=['CGO_ENABLED=0', '-trimpath', '-buildvcs=false'], completeAcceptance=False)


def validate(directory, candidate, source):
    files = [directory / name for name in ('walletgrpc-linux', 'walletgrpc.json')]
    if directory.is_symlink() or any(path.is_symlink() or not path.is_file() for path in files):
        raise ValueError('original CI wallet client and metadata required; symlinks refused')
    raw = files[0].read_bytes()
    if json.loads(files[1].read_text()) != identity(raw, source, candidate):
        raise ValueError('original wallet client identity differs from candidate/source/binary')
    return files


def export(binary, directory, candidate, source):
    raw = binary.read_bytes()
    value = identity(raw, source, candidate)
    if directory.is_symlink():
        raise ValueError('client export directory must not be a symlink')
    directory.mkdir(parents=True, exist_ok=True)
    for name, data, mode in [('walletgrpc-linux', raw, 0o755),
                             ('walletgrpc.json', (json.dumps(value, indent=2) + '\n').encode(), 0o644)]:
        path = directory / name
        if path.is_symlink():
            raise ValueError('client export symlink refused')
        if path.exists():
            if path.read_bytes() != data:
                raise ValueError('client export differs from original compile; preserve both runs')
        else:
            with path.open('xb') as stream:
                stream.write(data)
            path.chmod(mode)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--directory', type=Path, required=True)
    parser.add_argument('--candidate', required=True)
    parser.add_argument('--source-root', type=Path, default=Path.cwd())
    args = parser.parse_args()
    try:
        export(args.binary, args.directory, args.candidate, (args.source_root / 'test/walletgrpc/main.go').read_bytes())
    except (OSError, ValueError, struct.error) as error:
        parser.exit(1, 'FAIL: ' + str(error) + '\n')


if __name__ == '__main__':
    main()
