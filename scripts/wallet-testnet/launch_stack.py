#!/usr/bin/env python3
"""Start or reuse one verified local Sepolia stack; no wallet or chain writes."""
import argparse
from contextlib import contextmanager
from datetime import datetime, timezone
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('launch_preparation', Path(__file__).with_name('prepare_stack.py'))
prepare = importlib.util.module_from_spec(spec); spec.loader.exec_module(prepare)
import wallet_runtime

PINNED = ('scripts/wallet-testnet/launch_stack.py', 'scripts/wallet-testnet/prepare_stack.py',
    'scripts/wallet-testnet/network_preflight.py', 'scripts/wallet-testnet/deployment_manifest.py',
    'scripts/cd/wallet_runtime.py', 'scripts/cd/candidate_manifest.py', 'scripts/cd/assert_candidate_evidence.py',
    'scripts/cd/evidence_transport.py', 'scripts/cd/ci_provenance.py', 'scripts/cd/wallet_helper.py', 'config/performance.acceptance.json')


def require(condition, reason):
    if not condition:
        raise ValueError(reason)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


@contextmanager
def project_lock(directory):
    descriptor = os.open(directory / '.launch.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'r+') as lock:
        try: fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError: raise prepare.deployment.BlockedError('an owned launcher is still active; preserve it') from None
        yield


def verify_inputs(args):
    candidate = prepare.deployment.candidate_id(args.candidate)
    controller = prepare.deployment.address(args.controller)
    if not args.recipient: raise prepare.deployment.BlockedError('external recipient B is required')
    recipient = prepare.deployment.address(args.recipient)
    require(recipient != controller, 'recipient B must differ from controller A')
    for path in PINNED:
        try: frozen = prepare.git(args.source_root, 'show', candidate + ':' + path)
        except ValueError: raise prepare.deployment.BlockedError('launcher must be part of the frozen candidate') from None
        require((prepare.ROOT / path).read_bytes() == frozen, 'launcher or verification dependencies changed after candidate freeze')
    run = prepare.ci_run(args.repository, args.ci_run, candidate)
    prepare.verify_producer(args, run, candidate)
    original = prepare.ci_provenance.bind_upload(args.repository, run, json.loads(args.producer.read_text()))
    capsule = args.ci_root.resolve()
    manifest = json.loads((capsule / 'candidate.json').read_text())
    require(manifest['revision'] == candidate, 'CI capsule is from another candidate')
    prepare.assert_candidate_evidence.validate(capsule, args.source_root)
    archive = (capsule / manifest['archive']).resolve()
    require(archive.is_relative_to(capsule), 'candidate archive escaped original CI capsule')
    try: prepare.candidate_manifest.verify(manifest, platform='linux/amd64', archive_path=archive)
    except subprocess.CalledProcessError: raise prepare.deployment.BlockedError('reload the original accepted image archive; no rebuild required') from None
    identity = json.loads(args.build_identity.read_text()); entries = []
    for role in prepare.deployment.ROLES:
        relative = identity['contracts'][role]['artifactPath']; path = (args.build_identity.parent / relative).resolve()
        require(path.is_relative_to(args.build_identity.parent.resolve()), 'original contract artifact escaped its bundle')
        actual, _, _ = prepare.deployment.artifact_identity(path, role, candidate, args.source_root)
        entries.append(dict(role=role, artifact=actual))
    prepare.deployment.compiled_identity(args.build_identity, candidate, entries)
    directory = args.directory.resolve()
    config = json.loads((directory / 'testnet.config.json').read_text())
    require(config.get('candidateSHA') == candidate and config.get('controller') == controller and config.get('recipient') == recipient and config.get('chainId') == 11155111 and
            config.get('ciVerified') == run and config.get('buildIdentitySHA256') == sha(args.build_identity.read_bytes()) and
            config.get('walletControlVerified') is False and config.get('completeAcceptance') is False, 'prepared UI belongs to a different CI/controller/bundle')
    require((directory / 'fixture-build/build-identity.json').read_bytes() == args.build_identity.read_bytes(), 'served contract index differs from original CI')
    for role, entry in identity['contracts'].items():
        original_path = (args.build_identity.parent / entry['artifactPath']).resolve()
        copy_path = (directory / 'fixture-build' / entry['artifactPath']).resolve()
        require(copy_path.is_relative_to(directory / 'fixture-build') and not copy_path.is_symlink() and copy_path.read_bytes() == original_path.read_bytes(), 'served artifact differs from original CI')
    original_helper = prepare.wallet_helper.validate(capsule / 'wallet-build', candidate,
        prepare.git(args.source_root, 'show', candidate + ':test/walletgrpc/main.go'))
    prepared_helper = prepare.wallet_helper.validate(directory / 'wallet-client', candidate,
        prepare.git(args.source_root, 'show', candidate + ':test/walletgrpc/main.go'))
    require(all(a.read_bytes() == b.read_bytes() for a, b in zip(original_helper, prepared_helper)), 'prepared gRPC helper differs from original CI')
    paths = prepare.git(args.source_root, 'ls-tree', '-r', '--name-only', candidate, '--', 'h5-demo').decode().splitlines()
    source = lambda path: prepare.git(args.source_root, 'show', candidate + ':' + path)
    prepared = wallet_runtime.inspect_prepared(directory, candidate, capsule, source, paths, prepare.nginx, original)
    # Loopback ports and bind mounts refer to the machine running this command.
    # A remote daemon would bind its own loopback and read unrelated host paths.
    host = os.environ.get('DOCKER_HOST')
    if not host:
        context = json.loads(wallet_runtime.command(['docker', 'context', 'inspect']))
        host = context[0]['Endpoints']['docker']['Host']
    require(isinstance(host, str) and host.startswith('unix://'), 'local Docker daemon required for loopback ports and prepared bind mounts')
    prepared.update(directory=directory, capsule=capsule, source=source, h5Paths=paths, originalCI=original)
    return prepared


def observed(command, report, env=None):
    result = subprocess.run(command, capture_output=True, env=env)
    # Compose errors and provider messages may include credentials. Preserve
    # original exits and output fingerprints without copying private bytes.
    report['commands'].append(dict(command=command, exit=result.returncode, stdoutSHA256=sha(result.stdout), stderrSHA256=sha(result.stderr)))
    return result


def start(args, prepared, report):
    project, environment, config = (prepared[key] for key in ('project', 'environment', 'config'))
    report['project'] = project
    inspect = lambda: wallet_runtime.inspect_stack(prepared['directory'], args.candidate, prepared['capsule'], prepared['source'],
        prepared['h5Paths'], prepare.nginx, prepared['originalCI'])
    ids = wallet_runtime.command(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project=' + project]).decode().split()
    if ids:
        report['containers'] = inspect()
        report['status'] = 'REUSED_HEALTHY'
        return
    for resource in ('network', 'volume'):
        existing = wallet_runtime.command(['docker', resource, 'ls', '-q', '--filter', 'label=com.docker.compose.project=' + project]).strip()
        if existing:
            raise prepare.deployment.BlockedError('preserve existing project resources; inspect their authoritative state before recovery')
    network_output = args.output.with_suffix('.network.json'); network_exit = args.output.with_suffix('.network.exit')
    require(not network_output.exists() and not network_exit.exists(), 'original preflight evidence already exists')
    env = dict(environment, W3_SEPOLIA_RPC_PRIMARY=prepared['variables']['SEPOLIA_RPC_URL'],
               W3_SEPOLIA_RPC_FALLBACK=prepared['variables']['SEPOLIA_RPC_FALLBACK_URL'])
    result = observed([sys.executable, str(prepare.ROOT / 'scripts/wallet-testnet/network_preflight.py'), '--address', args.controller,
        '--output', str(network_output)], report, env)
    with network_exit.open('x') as stream: stream.write(str(result.returncode) + '\n')
    report.update(networkReport=network_output.name, networkExit=network_exit.name)
    if result.returncode:
        raise prepare.deployment.BlockedError('original network/funds preflight did not pass; no containers started')
    checked = set()
    for service, value in config['services'].items():
        image = value['image']
        if image in checked: continue
        checked.add(image)
        result = observed(['docker', 'image', 'inspect', '--platform', 'linux/amd64', image], report)
        if result.returncode:
            if service in ('monolith', 'api-gateway', 'transcoder'):
                raise prepare.deployment.BlockedError('reload the original accepted app image; no rebuild required')
            require(re.fullmatch(r'[^\s]+@sha256:[0-9a-f]{64}', image), 'infrastructure image must have an immutable digest')
            result = observed(['docker', 'pull', '--platform', 'linux/amd64', image], report)
            if result.returncode: raise prepare.deployment.BlockedError('pinned infrastructure image pull failed; no retry was dispatched')
    command = ['docker', 'compose', '-p', project, '-f', str(prepared['directory'] / 'compose.yml'), '--env-file', '/dev/null',
        'up', '-d', '--no-build', '--pull', 'never', '--wait', '--wait-timeout', '180']
    result = observed(command, report, environment)
    if result.returncode:
        raise prepare.deployment.BlockedError('startup did not reach healthy state; preserve the owned project for diagnosis')
    report['containers'] = inspect()
    report['status'] = 'STARTED_HEALTHY'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('candidate', 'controller', 'repository', 'ci-run'): parser.add_argument('--' + name, required=True)
    parser.add_argument('--recipient', default=os.environ.get('W3_SEPOLIA_RECIPIENT', ''))
    for name in ('producer', 'producer-root', 'ci-root', 'build-identity', 'directory', 'output'): parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--source-root', type=Path, default=prepare.ROOT)
    args = parser.parse_args()
    report = dict(schemaVersion=1, scope='LOCAL_SEPOLIA_STACK_STARTUP', candidateSHA=args.candidate, status='BLOCKED', commands=[],
        startedAt=datetime.now(timezone.utc).isoformat(), completeAcceptance=False, walletControlVerified=False, sourceSHA256=sha(Path(__file__).read_bytes()))
    if args.output.exists():
        print('FAIL: output already exists; original startup evidence preserved'); return 1
    try:
        prepared = verify_inputs(args)
        args.output.parent.mkdir(parents=True, exist_ok=True)
        with project_lock(prepared['directory']):
            start(args, prepared, report)
        code = 0
    except (FileNotFoundError, prepare.ci_provenance.UnavailableError, prepare.deployment.BlockedError, subprocess.TimeoutExpired):
        report.update(status='BLOCKED', reason='original evidence, local runtime or external dependency unavailable'); code = 2
    except (OSError, ValueError, KeyError, TypeError, IndexError, subprocess.SubprocessError, prepare.evidence_transport.TransportError):
        report.update(status='FAIL', reason='candidate/preparation integrity or startup command validation failed'); code = 1
    report['finishedAt'] = datetime.now(timezone.utc).isoformat()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open('x') as stream: stream.write(json.dumps(report, indent=2) + '\n')
    print(report['status'] + ': local stack only; wallet control and full acceptance=false')
    return code


if __name__ == '__main__':
    raise SystemExit(main())
