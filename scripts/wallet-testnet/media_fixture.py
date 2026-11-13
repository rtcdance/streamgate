#!/usr/bin/env python3
"""Seed the one Sepolia media fixture through the public API as controller A.

r7 left every playback cell BLOCKED_FIXTURE_MISSING because no step of the
Sepolia flow produced content. This tool generates the same deterministic clip
as the fullchain acceptance, uploads it with A's own SIWE session, binds the
server-side gating rule to the recorded Sepolia NFT and waits for a real
transcode. It never signs, never broadcasts, never mints, and never reads a
private key: A's access token comes from a 0600 file the owner exported after
signing in on the H5 page. The token is not written to the report or logs.
"""
import argparse
import base64
from datetime import datetime, timezone
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
import urllib.error
import urllib.request
import uuid

CHAIN_ID = 11155111
READY = ('ready', 'published')
FAILED = ('failed', 'error')
ADDRESS = re.compile(r'0x[0-9a-f]{40}')
DURATION_SECONDS = 10


class FixtureError(Exception):
    """The fixture is wrong: a FAIL, not a missing precondition."""


class BlockedError(Exception):
    """A precondition or dependency is unavailable: BLOCKED, never PASS."""


def address(value):
    value = str(value or '').lower()
    if not ADDRESS.fullmatch(value) or value == '0x' + '00' * 20:
        raise FixtureError('a nonzero 20-byte address is required')
    return value


def read_token(path):
    info = os.lstat(path)
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise BlockedError('the token file must be a regular file, not a symlink')
    # The session token is a bearer credential; refuse one other users can read.
    if os.name == 'posix' and stat.S_IMODE(info.st_mode) & 0o077:
        raise BlockedError('the token file must be private (mode 0600)')
    token = Path(path).read_text(encoding='utf-8').strip()
    if token.lower().startswith('bearer '):
        token = token[7:].strip()
    if token.count('.') != 2:
        raise BlockedError('the token file does not hold a JWT access token')
    return token


def token_claims(token):
    # Decoded only to bind the session to A before using it. The server is the
    # authority that verifies the signature on every request.
    payload = token.split('.')[1]
    try:
        return json.loads(base64.urlsafe_b64decode(payload + '=' * (-len(payload) % 4)))
    except (ValueError, json.JSONDecodeError) as error:
        raise BlockedError('the access token payload is unreadable') from error


def bind_session(claims, controller):
    if claims.get('token_type') not in (None, '', 'access'):
        raise FixtureError('the token is not an access token')
    if str(claims.get('wallet_address', '')).lower() != controller:
        raise FixtureError('the session belongs to another wallet; sign in as controller A')
    if claims.get('chain_id') not in (None, CHAIN_ID):
        raise FixtureError('the session was not issued for Ethereum Sepolia')
    if isinstance(claims.get('exp'), (int, float)) and claims['exp'] <= time.time() + 60:
        raise BlockedError('the session expires within a minute; sign in again')


def manifest_nft(path, controller):
    manifest = json.loads(Path(path).read_text(encoding='utf-8'))
    if not isinstance(manifest, dict) or manifest.get('schemaVersion') != 2 or manifest.get('deploymentVerified') is not True:
        raise FixtureError('a successfully recorded schema-v2 deployment manifest is required')
    if manifest.get('chainId') != CHAIN_ID or manifest.get('controller') != controller:
        raise FixtureError('the deployment manifest is for another network or controller')
    nfts = [item for item in manifest.get('contracts') or [] if isinstance(item, dict) and item.get('role') == 'nft']
    if len(nfts) != 1:
        raise FixtureError('the deployment manifest must record exactly one NFT')
    return address(nfts[0].get('address'))


def run(command, **kwargs):
    try:
        return subprocess.run(command, capture_output=True, text=True, check=True, timeout=300, **kwargs)
    except FileNotFoundError as error:
        raise BlockedError(f'required tool {command[0]} is not installed (no skip by design)') from error
    except subprocess.CalledProcessError as error:
        raise FixtureError(f'{command[0]} failed: {(error.stderr or error.stdout).strip()[:200]}') from error


def encoder():
    available = run(['ffmpeg', '-hide_banner', '-encoders']).stdout
    if 'h264_videotoolbox' in available:
        return 'h264_videotoolbox', ['-b:v', '500k']
    if re.search(r'\slibx264\s', available):
        return 'libx264', ['-preset', 'veryfast', '-b:v', '500k']
    raise BlockedError('no H.264 encoder in this ffmpeg build (need h264_videotoolbox or libx264)')


def generate_media(directory):
    # Same recipe as scripts/fullchain-acceptance.sh: 10s testsrc 640x360@30
    # plus a 440 Hz sine, H.264 + AAC. A real, decodable file, not a placeholder.
    name, arguments = encoder()
    media = Path(directory) / 'sg-sepolia-fixture.mp4'
    run(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-y',
         '-f', 'lavfi', '-i', f'testsrc=size=640x360:rate=30:duration={DURATION_SECONDS}',
         '-f', 'lavfi', '-i', f'sine=frequency=440:sample_rate=44100:duration={DURATION_SECONDS}',
         '-c:v', name, *arguments, '-pix_fmt', 'yuv420p', '-c:a', 'aac', '-b:a', '96k', '-shortest', str(media)])
    return media, name


def describe_media(media, encoder_name):
    if not media.is_file() or media.stat().st_size == 0:
        raise FixtureError('ffmpeg produced no fixture')
    run(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-i', str(media), '-f', 'null', '-'])
    probe = json.loads(run(['ffprobe', '-v', 'error', '-show_entries', 'format=duration:stream=codec_type,codec_name,width,height',
                            '-of', 'json', str(media)]).stdout)
    streams = {item.get('codec_type'): item for item in probe.get('streams', [])}
    if 'video' not in streams or 'audio' not in streams:
        raise FixtureError('the fixture must carry one video and one audio stream')
    return {
        'sha256': hashlib.sha256(media.read_bytes()).hexdigest(), 'bytes': media.stat().st_size,
        'encoder': encoder_name, 'videoCodec': streams['video'].get('codec_name'),
        'audioCodec': streams['audio'].get('codec_name'),
        'resolution': f"{streams['video'].get('width')}x{streams['video'].get('height')}",
        'durationSeconds': float(probe.get('format', {}).get('duration') or 0),
        'ffmpeg': run(['ffmpeg', '-version']).stdout.splitlines()[0],
    }


class API:
    def __init__(self, base_url, token, opener=None):
        if not re.fullmatch(r'https?://[^/\s]+', base_url.rstrip('/')):
            raise FixtureError('the base URL must be a scheme and host only')
        self.base = base_url.rstrip('/')
        self.token = token
        self.open = opener or urllib.request.urlopen

    def call(self, method, path, body=None, content_type='application/json'):
        request = urllib.request.Request(self.base + path, data=body, method=method)
        request.add_header('Authorization', 'Bearer ' + self.token)
        if body is not None:
            request.add_header('Content-Type', content_type)
        try:
            with self.open(request, timeout=120) as response:
                return response.status, json.loads(response.read() or b'{}')
        except urllib.error.HTTPError as error:
            detail = error.read()[:200].decode('utf-8', 'replace')
            if error.code in (401, 403):
                raise FixtureError(f'{method} {path} was refused ({error.code}): {detail}') from error
            raise BlockedError(f'{method} {path} returned {error.code}: {detail}') from error
        except (urllib.error.URLError, OSError) as error:
            raise BlockedError(f'{method} {path} is unreachable: {error}') from error

    def upload(self, media):
        boundary = 'sg-' + uuid.uuid4().hex
        body = b''.join([
            f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="sg-sepolia-fixture.mp4"\r\n'
            'Content-Type: video/mp4\r\n\r\n'.encode(), media.read_bytes(), f'\r\n--{boundary}--\r\n'.encode()])
        _, response = self.call('POST', '/api/v1/upload', body, f'multipart/form-data; boundary={boundary}')
        upload_id = response.get('upload_id')
        if not upload_id or response.get('status') != 'completed':
            raise FixtureError(f'upload did not complete: {json.dumps(response)[:160]}')
        return str(upload_id)

    def complete(self, upload_id):
        _, response = self.call('POST', f'/api/v1/upload/{upload_id}/complete-upload')
        if not response.get('content_id'):
            raise FixtureError(f'complete-upload returned no content_id: {json.dumps(response)[:160]}')
        return str(response['content_id'])

    def gate(self, content_id, nft):
        body = json.dumps({'contract_address': nft, 'chain_id': CHAIN_ID, 'standard': 'erc721', 'min_balance': 1}).encode()
        _, response = self.call('POST', f'/api/v1/content/{content_id}/gating-rules', body)
        if not response.get('id'):
            raise FixtureError(f'the gating rule was not created: {json.dumps(response)[:160]}')
        return str(response['id'])

    def wait_ready(self, content_id, seconds, sleep=time.sleep, clock=time.monotonic):
        deadline, last = clock() + seconds, None
        while True:
            _, response = self.call('GET', f'/api/v1/content/{content_id}')
            last = str(response.get('status', ''))
            if last in READY:
                return last
            if last in FAILED:
                raise FixtureError(f'transcoding failed (content status {last})')
            if clock() >= deadline:
                raise BlockedError(f'content was still {last or "unknown"} after {seconds}s')
            sleep(5)


def write_report(output, report, status, reason=None):
    report.update(status=status, fixtureReady=status == 'PASS', completeAcceptance=False)
    if reason:
        report['reason'] = reason
    output.parent.mkdir(parents=True, exist_ok=True)
    # A new file per run: a rerun must never overwrite the previous evidence.
    with open(output, 'x', encoding='utf-8') as stream:
        stream.write(json.dumps(report, indent=2) + '\n')
    print(f'{status}: Sepolia media fixture; no signatures or broadcasts' + (f' ({reason})' if reason else ''))
    return {'PASS': 0, 'BLOCKED': 2}.get(status, 1)


def build(args, api_factory=API):
    report = {'schemaVersion': 1, 'scope': 'SEPOLIA_MEDIA_FIXTURE', 'chainId': CHAIN_ID,
              'at': datetime.now(timezone.utc).isoformat(), 'baseURL': args.base_url}
    workdir = None
    try:
        controller = address(args.controller)
        report['controller'] = controller
        token = read_token(args.token_file)
        bind_session(token_claims(token), controller)
        nft = manifest_nft(args.manifest, controller)
        report['nft'] = nft
        if args.media:
            media, encoder_name = Path(args.media), 'provided'
        else:
            workdir = tempfile.mkdtemp(prefix='sg-sepolia-media-')
            media, encoder_name = generate_media(workdir)
        report['media'] = describe_media(media, encoder_name)
        api = api_factory(args.base_url, token)
        report['uploadID'] = api.upload(media)
        report['contentID'] = api.complete(report['uploadID'])
        report['ruleID'] = api.gate(report['contentID'], nft)
        report['contentStatus'] = api.wait_ready(report['contentID'], args.ready_seconds)
        status, reason = 'PASS', None
    except FixtureError as error:
        status, reason = 'FAIL', str(error)
    except (BlockedError, OSError, ValueError) as error:
        status, reason = 'BLOCKED', str(error)
    finally:
        if workdir:
            shutil.rmtree(workdir, ignore_errors=True)
    return write_report(args.output, report, status, reason)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument('--base-url', required=True, help='monolith or api-gateway origin, e.g. http://localhost:18080')
    parser.add_argument('--controller', required=True, help='controller A; the session must belong to it')
    parser.add_argument('--token-file', required=True, type=Path, help="0600 file holding A's access token")
    parser.add_argument('--manifest', required=True, type=Path, help='recorded schema-v2 deployment manifest')
    parser.add_argument('--output', required=True, type=Path, help='new report path; never overwritten')
    parser.add_argument('--media', type=Path, help='use this clip instead of generating the standard one')
    parser.add_argument('--ready-seconds', type=int, default=600)
    args = parser.parse_args(argv)
    if args.ready_seconds <= 0:
        parser.error('--ready-seconds must be positive')
    if args.output.exists():
        parser.error('--output already exists; each run writes a new report')
    return build(args)


if __name__ == '__main__':
    sys.exit(main())
