#!/usr/bin/env python3
"""Collect public control observations from a verified, existing local stack."""
import argparse
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import uuid

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('controls_launcher', Path(__file__).with_name('launch_stack.py'))
launch = importlib.util.module_from_spec(spec); spec.loader.exec_module(launch)
prepare = launch.prepare
PINNED = (*launch.PINNED, 'scripts/wallet-testnet/observe_controls.py',
          'scripts/h5-demo-acceptance/wallet-sepolia-controls.mjs',
          'scripts/h5-demo-acceptance/lib/wallet_file_rpc.mjs',
          'scripts/h5-demo-acceptance/lib/wallet_public_controls.mjs',
          'scripts/h5-demo-acceptance/lib/wallet_sepolia_contract.mjs',
          'scripts/h5-demo-acceptance/lib/wallet_sepolia_evidence.mjs',
          'scripts/cd/assert_sepolia_browser.mjs', 'scripts/cd/assert_public_controls.py')


def require(ok, reason):
    if not ok: raise ValueError(reason)


def private_json(path, value):
    with open(path, 'x', opener=lambda name, flags: os.open(name, flags, 0o600)) as stream:
        json.dump(value, stream, indent=2); stream.write('\n')


def pinned(args):
    for path in PINNED:
        try: frozen = prepare.git(args.source_root, 'show', args.candidate + ':' + path)
        except subprocess.CalledProcessError: raise prepare.deployment.BlockedError('collector must belong to the frozen candidate') from None
        require((prepare.ROOT / path).read_bytes() == frozen, 'collector/source changed after candidate freeze')


def context(args, prepared, identities):
    require(args.mode in ('monolith', 'api-gateway') and args.phase in ('eoa', 'contract'), 'explicit public mode and role required')
    wallet, nft = (prepare.deployment.address(value) for value in (args.wallet, args.nft))
    require(wallet != prepare.deployment.address(args.controller), 'contract wallet must differ from its controller')
    front = 'monolith-h5' if args.mode == 'monolith' else 'gateway-h5'
    ports = prepared['config']['services'][front]['ports']
    require(len(ports) == 1 and ports[0]['host_ip'] == '127.0.0.1', 'one loopback H5 origin required')
    origin = 'http://localhost:' + str(ports[0]['published'])
    subject = wallet if args.phase == 'contract' else prepare.deployment.address(args.controller)
    command = ['node', str(prepare.ROOT / 'scripts/cd/assert_sepolia_browser.mjs'), '--candidate', args.candidate,
               '--mode', args.mode, '--phase', args.phase, '--controller', prepare.deployment.address(args.controller),
               '--subject', subject, '--nft', nft, '--content', args.content, '--origin', origin,
               '--directory', str(args.phase_directory), '--exit-file', str(args.phase_exit)]
    checked = subprocess.run(command, capture_output=True, timeout=60)
    require(checked.returncode == 0, 'original wallet phase has not passed independent verification')
    phase = json.loads((args.phase_directory / 'result.json').read_text())
    gate = ('TN04' if args.phase == 'contract' else 'TN03') + '_PLAY'
    binding = next(row['state']['claims'] for row in phase['gates'] if row['id'] == gate)
    runtime = identities[args.mode]
    require(runtime['startedAtMs'] <= min(row['startedAt'] for row in phase['gates']), 'runtime was restarted after the original admission')
    config = prepared['config']; data, egress = (config['networks'][key]['name'] for key in ('data', 'rpc-egress'))
    detail = json.loads(launch.wallet_runtime.command(['docker', 'inspect', runtime['containerID']]))[0]
    require(set(detail['NetworkSettings']['Networks']) == {data, egress}, 'application network scope changed')
    io = args.output / 'requests'; io.mkdir(mode=0o700)
    helper = prepared['directory'] / 'wallet-client/walletgrpc-linux'
    require(all(',' not in str(path) for path in (helper, io)), 'bind path contains a Docker mount delimiter')
    name = prepared['project'] + '-observer-' + uuid.uuid4().hex[:12]
    command = ['docker', 'run', '--rm', '--name', name, '--platform', 'linux/amd64', '--pull', 'never',
               '--network', data, '--label', 'com.streamgate.w3.observer=' + prepared['project'],
               '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges', '--pids-limit', '64',
               '--cpus', '0.25', '--memory', '128m', '--user', str(os.getuid()) + ':' + str(os.getgid()),
               '-e', 'GOMAXPROCS=1', '--mount', 'type=bind,src=' + str(helper) + ',dst=/walletgrpc,readonly',
               '--mount', 'type=bind,src=' + str(io) + ',dst=/requests', '--entrypoint', '/walletgrpc',
               '-i', runtime['imageID'], '--target', args.mode + ':9090', '--serve', '--request-dir', '/requests']
    return dict(schemaVersion=1, candidateSHA=args.candidate, project=prepared['project'], wallet=wallet, recipient=prepare.deployment.address(args.recipient), stackDirectory=str(prepared['directory']),
                container=runtime['containerID'], image=runtime['imageID'], dataNetwork=data, egressNetwork=egress,
                egressAliases=detail['NetworkSettings']['Networks'][egress].get('Aliases') or [],
                observerName=name, requestDirectory=str(io), command=command, sessionBinding=binding, oracle=str(args.oracle.resolve()), oracleExit=str(args.oracle_exit.resolve()),
                environment=dict(W3_MODE=args.mode, W3_PHASE=args.phase, W3_CANDIDATE_SHA=args.candidate,
                    W3_H5_BASE=origin, W3_CDP_ENDPOINT=args.cdp, W3_CONTROLLER=prepare.deployment.address(args.controller),
                    W3_SUBJECT_C=wallet, W3_NFT=nft, W3_CONTENT_ID=args.content, W3_REPORT_DIR=str(args.output),
                    W3_USER_GATE_TIMEOUT_MS=str(args.wait_seconds * 1000)))


def merge_main():
    parser = argparse.ArgumentParser(description='Merge original successful public role observations; full acceptance remains false')
    for key in ('candidate', 'controller', 'recipient', 'wallet', 'nft', 'content', 'origin', 'mode'): parser.add_argument('--' + key, required=True)
    for key in ('eoa', 'contract', 'output'): parser.add_argument('--' + key, required=True, type=Path)
    parser.add_argument('--source-root', type=Path, default=prepare.ROOT)
    args = parser.parse_args(sys.argv[2:])
    if args.output.exists(): print('FAIL: original merged output preserved'); return 1
    try:
        pinned(args)
        import assert_public_controls as gate
        config = dict(candidateSHA=args.candidate, controller=prepare.deployment.address(args.controller), wallet=prepare.deployment.address(args.wallet),
                      nft=prepare.deployment.address(args.nft), content=args.content, origin=args.origin, mode=args.mode, sessionBindings={}, transferStartedAt={})
        report = {key: value for key, value in config.items() if key not in ('sessionBindings', 'transferStartedAt')}
        report.update(schemaVersion=1, component='SEPOLIA_PUBLIC_CONTROLS', chainId=11155111, completeAcceptance=False, publicWalletSignoff=False, transfers={}, credentialRotations={})
        private = dict(credentials={}, responses={}); oracles = {}
        for role in ('eoa', 'contract'):
            directory = getattr(args, role)
            require(not directory.is_symlink(), 'original collector directory required')

            def raw(name):
                path = directory / name
                require(path.is_file() and not path.is_symlink() and path.stat().st_size <= 32 * 1024 * 1024, 'bounded original collector file required')
                return path.read_bytes()

            require(raw('collector.exit').strip() == b'0', 'original collector failed')
            original = json.loads(raw('collector.json')); value = json.loads(raw('result.json')); ctx = json.loads(raw('context.private.json'))
            require(original.get('scope') == 'SEPOLIA_CONTROL_COLLECTOR' and original.get('candidateSHA') == args.candidate and
                    original.get('status') == 'CONTROL_PHASE_OBSERVED' and original.get('originalNodeExit') == 0 and original.get('restoreExit') == 0,
                    'original collector/restore did not finish successfully')
            require(value.get('status') == 'CONTROL_PHASE_OBSERVED' and value.get('component') == 'SEPOLIA_PUBLIC_CONTROL_PHASE' and
                    value.get('phase') == role and value.get('clientExit', {}).get('code') == 0 and value['clientExit'].get('signal') is None and
                    value.get('completeAcceptance') is False and value.get('publicWalletSignoff') is False and
                    all(value.get(key) == expected for key, expected in report.items() if key in config and key != 'sessionBindings') and
                    ctx.get('recipient') == prepare.deployment.address(args.recipient), 'collector fragment belongs to a different candidate/fixture/role')
            original_private = raw('controls.private.json')
            require(hashlib.sha256(original_private).hexdigest() == value.get('privateSHA256'), 'original private collector bytes changed')
            data = json.loads(original_private)
            runtime_path = Path(ctx['stackDirectory']) / 'stack.env.json'
            require(runtime_path.is_file() and not runtime_path.is_symlink() and runtime_path.stat().st_mode & 0o077 == 0,
                    'private prepared runtime input required')
            runtime = json.loads(runtime_path.read_text()); require(runtime.get('W3_CANDIDATE_SHA') == args.candidate, 'runtime signing key belongs to another candidate')
            secret = runtime['W3_JWT_SECRET']
            require('jwtSecret' not in config or config['jwtSecret'] == secret, 'role observations belong to different runtime signing keys')
            config['jwtSecret'] = secret
            require(set(data) == {'credentials', 'responses'}, 'unexpected collector private layout')
            for kind in private:
                for key, content in data[kind].items():
                    require(key not in private[kind] or private[kind][key] == content, 'private observation collision')
                    private[kind][key] = content
            report['transfers'][role] = value['transfer']; config['sessionBindings'][role] = ctx['sessionBinding']
            report['credentialRotations'][role] = value.get('credentialRotations', [])
            oracle_exit = Path(ctx['oracleExit'])
            require(oracle_exit.is_file() and not oracle_exit.is_symlink() and oracle_exit.stat().st_size <= 16 and oracle_exit.read_text().strip() == '0',
                    'original oracle process did not exit successfully')
            oracle = Path(ctx['oracle']); require(oracle.is_file() and not oracle.is_symlink(), 'original oracle file required')
            oracle_raw = oracle.read_bytes(); chain = json.loads(oracle_raw)
            require(chain.get('phase') == role + '-transferred' and chain.get('recipient') == prepare.deployment.address(args.recipient), 'oracle phase or recipient changed')
            oracles[role + '-transferred'] = dict(sha256=hashlib.sha256(oracle_raw).hexdigest(), report=chain)
            config['transferStartedAt'][role] = chain['lastOperationCreatedAtMs']
            if role == 'contract':
                report['dependencyOutage'] = value['dependencyOutage']; report['reload'] = value['reload']
        raw_private = (json.dumps(private, indent=2) + '\n').encode()
        report['privateSHA256'] = hashlib.sha256(raw_private).hexdigest()
        gate.validate(report, raw_private, config, oracles, 0)
        args.output.mkdir(parents=True, mode=0o700)
        private_json(args.output / 'controls.private.json', private); private_json(args.output / 'result.json', report)
        (args.output / 'controls.exit').write_text('0\n')
        print('CONTROL_COMPONENT_VERIFIED: full acceptance=false; original default CI and independent public-chain replay remain required')
        return 0
    except (FileNotFoundError, prepare.deployment.BlockedError):
        print('BLOCKED: original same-candidate collector inputs unavailable'); return 2
    except (OSError, ValueError, KeyError, TypeError, IndexError, subprocess.SubprocessError):
        print('FAIL: original collector provenance or public control semantics failed'); return 1


def restore(ctx):
    # Parent guard also runs if Node is interrupted or killed. Never restart an
    # app or touch another project to recover this intentionally bounded fault.
    value = json.loads(launch.wallet_runtime.command(['docker', 'inspect', ctx['container']]))[0]
    labels = value['Config']['Labels']
    require(value['Id'] == ctx['container'] and value['Image'] == ctx['image'] and value['State']['Running'] and
            labels.get('com.streamgate.w3.candidate') == ctx['candidateSHA'] and labels.get('com.streamgate.w3.testnet') == ctx['project'] and
            ctx['dataNetwork'] in value['NetworkSettings']['Networks'], 'fault recovery scope changed; preserve runtime')
    if ctx['egressNetwork'] not in value['NetworkSettings']['Networks']:
        aliases = [part for alias in ctx['egressAliases'] for part in ('--alias', alias)]
        launch.wallet_runtime.command(['docker', 'network', 'connect', *aliases, ctx['egressNetwork'], ctx['container']])
    existing = launch.wallet_runtime.command(['docker', 'ps', '-aq', '--filter', 'name=^/' + ctx['observerName'] + '$']).decode().split()
    if existing:
        observer = json.loads(launch.wallet_runtime.command(['docker', 'inspect', *existing]))
        require(len(observer) == 1 and observer[0]['Config']['Labels'].get('com.streamgate.w3.observer') == ctx['project'], 'client cleanup scope changed')
        launch.wallet_runtime.command(['docker', 'stop', '--time', '5', existing[0]])


def main():
    if sys.argv[1:2] == ['merge']: return merge_main()
    parser = argparse.ArgumentParser(description=__doc__)
    for key in ('candidate', 'controller', 'recipient', 'repository', 'ci-run', 'wallet', 'nft', 'content', 'cdp'):
        parser.add_argument('--' + key, required=True)
    for key in ('producer', 'producer-root', 'ci-root', 'build-identity', 'directory', 'output', 'phase-directory', 'phase-exit', 'oracle', 'oracle-exit'):
        parser.add_argument('--' + key, required=True, type=Path)
    parser.add_argument('--mode', choices=('monolith', 'api-gateway'), required=True)
    parser.add_argument('--phase', choices=('eoa', 'contract'), required=True)
    parser.add_argument('--wait-seconds', type=int, default=1800)
    parser.add_argument('--source-root', type=Path, default=prepare.ROOT)
    args = parser.parse_args(); ctx = None; code = 2; lock = None
    if args.output.exists(): print('FAIL: choose a fresh collector directory; originals preserved'); return 1
    args.output = args.output.absolute(); args.output.mkdir(parents=True, mode=0o700)
    summary = dict(scope='SEPOLIA_CONTROL_COLLECTOR', candidateSHA=args.candidate, status='BLOCKED', completeAcceptance=False)
    try:
        require(1 <= args.wait_seconds <= 1800 and re.fullmatch('[0-9a-f]{40}', args.candidate), 'bounded wait and full candidate required')
        pinned(args)
        prepared = launch.verify_inputs(args)
        lock = os.open(prepared['directory'] / ('.observe-' + args.mode + '.lock'), os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        identities = launch.wallet_runtime.inspect_stack(prepared['directory'], args.candidate, prepared['capsule'], prepared['source'],
                                                        prepared['h5Paths'], prepare.nginx, prepared['originalCI'])
        ctx = context(args, prepared, identities); path = args.output / 'context.private.json'; private_json(path, ctx)
        environment = {key: value for key, value in os.environ.items() if not key.startswith(('W3_', 'SEPOLIA_', 'STREAMGATE_', 'WEB3_', 'DEBUG', 'PWDEBUG'))}
        with open(args.output / 'collector.private.log', 'x', opener=lambda name, flags: os.open(name, flags, 0o600)) as log:
            # stdout contains only READY/status; Node's detailed errors and any
            # dependency diagnostics remain in this private original log.
            result = subprocess.run(['node', str(prepare.ROOT / 'scripts/h5-demo-acceptance/wallet-sepolia-controls.mjs'), str(path)],
                                    stdout=log, stderr=subprocess.STDOUT, env=environment)
        code = result.returncode
        summary['originalNodeExit'] = code
        if code == 0:
            observed = json.loads((args.output / 'result.json').read_text())
            require(observed.get('status') == 'CONTROL_PHASE_OBSERVED' and observed.get('clientExit', {}).get('code') == 0 and
                    observed.get('completeAcceptance') is False, 'collector returned a false successful exit')
            summary['status'] = 'CONTROL_PHASE_OBSERVED'
        else: summary['status'] = 'BLOCKED' if code == 2 else 'FAIL'
    except (BlockingIOError, FileNotFoundError, prepare.ci_provenance.UnavailableError, prepare.deployment.BlockedError, subprocess.TimeoutExpired):
        summary['status'] = 'BLOCKED'; code = 2
    except (OSError, ValueError, KeyError, TypeError, StopIteration, IndexError, subprocess.SubprocessError, prepare.evidence_transport.TransportError):
        summary['status'] = 'FAIL'; code = 1
    finally:
        if ctx:
            try: restore(ctx); summary['restoreExit'] = 0
            except (OSError, ValueError, KeyError, subprocess.SubprocessError): summary['restoreExit'] = 1; summary['status'] = 'FAIL'; code = 1
        if lock is not None: os.close(lock)
    private_json(args.output / 'collector.json', summary)
    (args.output / 'collector.exit').write_text(str(code) + '\n')
    print(summary['status'] + ': see original private collector files; full acceptance=false')
    return code


if __name__ == '__main__':
    raise SystemExit(main())
