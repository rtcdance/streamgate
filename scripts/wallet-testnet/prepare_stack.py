#!/usr/bin/env python3
"""Prepare a local Sepolia stack from a verified same-SHA CI capsule; never broadcast."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import sys
import uuid

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts/cd'))
import candidate_manifest
import assert_candidate_evidence
import evidence_transport
import ci_provenance
import wallet_helper

spec = importlib.util.spec_from_file_location('deployment', Path(__file__).with_name('deployment_manifest.py'))
deployment = importlib.util.module_from_spec(spec); spec.loader.exec_module(deployment)


def git(root, *arguments):
    result = subprocess.run(['git', '-C', str(root), *arguments], capture_output=True)
    if result.returncode: raise ValueError('the frozen source candidate is unavailable')
    return result.stdout


def ci_run(repository, run_id, candidate):
    return ci_provenance.default_run(repository, run_id, candidate)


def nginx(upstream):
    if upstream not in ('monolith', 'api-gateway'): raise ValueError('unknown mode')
    return '''worker_processes 1;
error_log /dev/stderr notice;
pid /tmp/nginx.pid;
events { worker_connections 512; }
http {
  include /etc/nginx/mime.types;
  default_type application/octet-stream;
  access_log /dev/stdout;
  client_body_temp_path /tmp/client_body;
  proxy_temp_path /tmp/proxy;
  fastcgi_temp_path /tmp/fastcgi;
  uwsgi_temp_path /tmp/uwsgi;
  scgi_temp_path /tmp/scgi;
  server {
    listen 8080;
    root /usr/share/nginx/html;
    index index.html;
    client_max_body_size 100m;
    resolver 127.0.0.11 valid=5s;
    set $backend ''' + upstream + ''';
    location /api/ { proxy_pass http://$backend:8080; proxy_set_header Host $http_host; }
    location = /health { proxy_pass http://$backend:8080; }
    location = /testnet.config.json { alias /testnet.config.json; add_header Cache-Control "no-store"; }
    location /fixture-build/ { alias /fixture-build/; add_header Cache-Control "no-store"; }
    location / { try_files $uri $uri/ /index.html; add_header Cache-Control "no-store"; }
  }
}
'''


def prepare(args):
    candidate = deployment.candidate_id(args.candidate)
    controller = deployment.address(args.controller)
    if not getattr(args, 'recipient', ''): raise deployment.BlockedError('external recipient B is required')
    recipient = deployment.address(args.recipient)
    if recipient == controller: raise ValueError('recipient B must differ from controller A')
    rpc = {key: os.environ.get(key, '') for key in ('SEPOLIA_RPC_URL', 'SEPOLIA_RPC_FALLBACK_URL')}
    if len({deployment.network.ReadOnlyRPC(value, key).host for key, value in rpc.items()}) != 2:
        raise ValueError('two distinct provider hosts required')
    capsule = args.ci_root.resolve()
    document = json.loads((capsule / 'candidate.json').read_text())
    if document.get('revision') != candidate: raise ValueError('CI capsule belongs to another candidate')
    assert_candidate_evidence.validate(capsule, args.source_root)
    archive = (capsule / document['archive']).resolve()
    if not archive.is_relative_to(capsule): raise ValueError('candidate archive escaped the CI capsule')
    candidate_manifest.verify(document, platform='linux/amd64', archive_path=archive)
    run = ci_run(args.repository, args.ci_run, candidate)
    verify_producer(args, run, candidate)
    original_ci = ci_provenance.bind_upload(args.repository, run, json.loads(args.producer.read_text()))
    helper_files = wallet_helper.validate(capsule / 'wallet-build', candidate,
        git(args.source_root, 'show', candidate + ':test/walletgrpc/main.go'))
    tracked = git(args.source_root, 'ls-tree', '-r', '--name-only', candidate, '--', 'h5-demo').decode().splitlines()
    if not tracked: raise ValueError('candidate H5 source is absent')
    compose = git(args.source_root, 'show', candidate + ':deploy/docker/compose.w3sepolia.yml')
    for path in ('config/config.w3sepolia.yaml', 'config/config.w3sepolia-worker.yaml', 'h5-demo/testnet/index.html'):
        git(args.source_root, 'cat-file', '-e', candidate + ':' + path)
    identity = json.loads(args.build_identity.read_text())
    entries = []
    for role in deployment.ROLES:
        item = identity.get('contracts', {}).get(role, {})
        path = (args.build_identity.parent / item.get('artifactPath', '')).resolve()
        if not path.is_relative_to(args.build_identity.parent.resolve()): raise ValueError('contract artifact escaped its original bundle')
        actual, _, _ = deployment.artifact_identity(path, role, candidate, args.source_root)
        entries.append({'role': role, 'artifact': actual})
    deployment.compiled_identity(args.build_identity, candidate, entries)
    if args.directory.exists(): raise ValueError('preparation directory already exists; preserve it and choose a new one')
    args.directory.mkdir(parents=True, mode=0o700)
    directory = args.directory.resolve()
    helper_directory = directory / 'wallet-client'; helper_directory.mkdir(mode=0o700)
    for helper in helper_files:
        target = helper_directory / helper.name
        shutil.copyfile(helper, target); target.chmod(0o755 if helper.suffix != '.json' else 0o644)
    frontend = directory / 'h5'; frontend.mkdir(mode=0o755)
    image = document['images']['monolith'].get('daemonImageID') or document['images']['monolith']['imageID']
    created = subprocess.run(['docker', 'create', '--network', 'none', '--label', 'com.streamgate.w3.prepare=' + candidate, image], capture_output=True, text=True)
    if created.returncode: raise ValueError('candidate H5 extraction container could not be created')
    container = created.stdout.strip()
    try:
        copied = subprocess.run(['docker', 'cp', container + ':/app/h5-demo/.', str(frontend)], capture_output=True)
        if copied.returncode: raise ValueError('original image H5 extraction failed')
    finally:
        subprocess.run(['docker', 'rm', '-v', container], check=True, capture_output=True)
    expected = {path.removeprefix('h5-demo/'): hashlib.sha256(git(args.source_root, 'show', candidate + ':' + path)).hexdigest() for path in tracked}
    actual = {}
    for path in frontend.rglob('*'):
        if path.is_symlink(): raise ValueError('candidate H5 contains a symlink')
        if path.is_file():
            actual[str(path.relative_to(frontend))] = hashlib.sha256(path.read_bytes()).hexdigest(); path.chmod(0o644)
        else: path.chmod(0o755)
    if actual != expected: raise ValueError('image H5 differs from the frozen source candidate')
    bundle = directory / 'fixture-build'; bundle.mkdir(mode=0o755)
    shutil.copyfile(args.build_identity, bundle / 'build-identity.json')
    for role, item in identity['contracts'].items():
        relative = Path(item['artifactPath']); target = bundle / relative
        target.parent.mkdir(parents=True, exist_ok=True); shutil.copyfile(args.build_identity.parent / relative, target)
    (directory / 'compose.yml').write_bytes(compose)
    for mode in ('monolith', 'api-gateway'): (directory / (mode + '.nginx.conf')).write_text(nginx(mode))
    config = {'schemaVersion': 1, 'candidateSHA': candidate, 'controller': controller, 'recipient': recipient, 'chainId': 11155111, 'ciVerified': run,
        'transactionOrigin': 'http://localhost:18500',
        'buildIdentitySHA256': hashlib.sha256(args.build_identity.read_bytes()).hexdigest(), 'maxTransactions': 8, 'maxFeeWei': '20000000000000000',
        'walletControlVerified': False, 'completeAcceptance': False}
    (directory / 'testnet.config.json').write_text(json.dumps(config, indent=2) + '\n'); (directory / 'testnet.config.json').chmod(0o644)
    env = {'W3_PROJECT': 'sg-w3-' + candidate[:8] + '-' + uuid.uuid4().hex[:8], 'W3_CANDIDATE_SHA': candidate,
        'W3_JWT_SECRET': secrets.token_hex(32), **rpc,
        'W3_H5_DIR': str(frontend), 'W3_CONTRACT_BUNDLE_DIR': str(bundle), 'W3_TESTNET_CONFIG': str(directory / 'testnet.config.json'),
        'W3_MONOLITH_NGINX_CONFIG': str(directory / 'monolith.nginx.conf'), 'W3_GATEWAY_NGINX_CONFIG': str(directory / 'api-gateway.nginx.conf')}
    for role, key in (('monolith', 'W3_MONOLITH_IMAGE'), ('api-gateway', 'W3_API_GATEWAY_IMAGE'), ('transcoder', 'W3_TRANSCODER_IMAGE')):
        env[key] = document['images'][role].get('daemonImageID') or document['images'][role]['imageID']
    with open(directory / 'stack.env.json', 'w', opener=lambda p, flags: os.open(p, flags, 0o600)) as stream:
        json.dump(env, stream, indent=2); stream.write('\n')
    (directory / 'preparation.json').write_text(json.dumps({'candidateSHA': candidate, 'ciRun': run, 'originalCI': original_ci, 'sourceH5SHA256': expected,
        'walletClientSHA256': hashlib.sha256(helper_files[0].read_bytes()).hexdigest(),
        'candidateArchiveSHA256': document['archiveSHA256'], 'buildIdentitySHA256': config['buildIdentitySHA256'], 'completeAcceptance': False}, indent=2) + '\n')
    print('PREPARED: same-C images, H5 and original contract artifacts; no app start, signatures or broadcasts')


def verify_producer(args, run, candidate):
    producer = json.loads(args.producer.read_text())
    if producer.get('workflow') != 'CI' or producer.get('job') != 'acceptance':
        raise ValueError('the original default CI acceptance job must produce this capsule')
    # Failed-job reruns may legitimately consume an earlier successful producer
    # of this same run. Preserve its attempt; verify every original payload byte.
    entries = evidence_transport.verify_manifest(producer, 'ci-acceptance-' + candidate,
        candidate, run['id'], None, max_attempt=run['run_attempt'])
    root = args.producer_root.resolve()
    if not args.ci_root.resolve().is_relative_to(root) or not args.build_identity.resolve().is_relative_to(args.ci_root.resolve()):
        raise ValueError('candidate capsule and original build index must be inside the verified producer payload')
    evidence_transport.verify_tree(root, entries)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', required=True)
    parser.add_argument('--controller', required=True)
    parser.add_argument('--recipient', default=os.environ.get('W3_SEPOLIA_RECIPIENT', ''))
    parser.add_argument('--repository', required=True)
    parser.add_argument('--ci-run', required=True)
    parser.add_argument('--producer', type=Path, required=True)
    parser.add_argument('--producer-root', type=Path, required=True)
    parser.add_argument('--ci-root', type=Path, required=True)
    parser.add_argument('--build-identity', type=Path, required=True)
    parser.add_argument('--source-root', type=Path, default=ROOT)
    parser.add_argument('--directory', type=Path, required=True)
    try: prepare(parser.parse_args())
    except (ValueError, OSError, KeyError, TypeError, subprocess.SubprocessError, evidence_transport.TransportError) as error:
        print('BLOCKED: ' + str(error)); return 2
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
