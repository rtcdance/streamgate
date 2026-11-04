#!/usr/bin/env python3
"""Read-only, fixed-matrix closeout of plan section 10 from original evidence.

extension.json selects paths, never declares success or supplies a required list.
All paths are relative to the evidence directory. Missing evidence is BLOCKED;
failed, foreign, skipped or altered evidence is FAIL. No writes to CI or chain.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from types import SimpleNamespace

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
import assert_candidate_evidence
import assert_public_controls
import assert_required_tests
import assert_wallet_browser
import ci_provenance
import evidence_transport
import wallet_helper
import wallet_runtime

spec = importlib.util.spec_from_file_location('extension_phase_evidence', ROOT / 'scripts/wallet-testnet/phase_evidence.py')
phases = importlib.util.module_from_spec(spec); spec.loader.exec_module(phases)
oracle = phases.oracle
spec = importlib.util.spec_from_file_location('extension_stack_preparation', ROOT / 'scripts/wallet-testnet/prepare_stack.py')
preparation = importlib.util.module_from_spec(spec); spec.loader.exec_module(preparation)
CASES = tuple([f'CW-{i:02}' for i in range(1, 17)] + [f'TN-{i:02}' for i in range(1, 9)])
MODES = ('monolith', 'api-gateway')
SUITES = {'wallet-unit-tests': 'unit-tests', 'wallet-account-tests': 'anvil-tests',
          'wallet-session-tests': 'wallet-session-tests', 'wallet-session-durability-tests': 'wallet-session-durability',
          'wallet-postgres-restore-tests': 'wallet-postgres-restore'}
FROZEN = ('scripts/cd/verify_web3_extension.py', 'scripts/cd/assert_public_controls.py', 'scripts/cd/assert_candidate_evidence.py',
    'scripts/cd/assert_wallet_browser.py', 'scripts/cd/assert_required_tests.py', 'scripts/cd/ci_provenance.py',
    'scripts/cd/evidence_transport.py', 'scripts/cd/wallet_helper.py', 'scripts/cd/wallet_runtime.py', 'scripts/cd/assert_sepolia_browser.mjs',
    'scripts/cd/required-tests.json', 'scripts/wallet-testnet/phase_evidence.py', 'scripts/wallet-testnet/transaction_oracle.py',
    'scripts/wallet-testnet/deployment_manifest.py', 'scripts/wallet-testnet/network_preflight.py', 'scripts/wallet-testnet/prepare_stack.py', 'scripts/wallet-testnet/launch_stack.py', 'scripts/wallet-testnet/verify_web3_testnet.py',
    'scripts/h5-demo-acceptance/lib/wallet_sepolia_evidence.mjs', 'scripts/h5-demo-acceptance/lib/wallet_sepolia_contract.mjs',
    'scripts/wallet-testnet/observe_controls.py', 'scripts/h5-demo-acceptance/wallet-sepolia-controls.mjs',
    'scripts/h5-demo-acceptance/lib/wallet_file_rpc.mjs', 'scripts/h5-demo-acceptance/lib/wallet_public_controls.mjs',
    'h5-demo/vendor/ethers.umd.min.js', 'config/performance.acceptance.json')


def require(ok, reason):
    if not ok:
        raise ValueError(reason)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def public_endpoints():
    if not all(os.environ.get(key) for key in ('W3_SEPOLIA_RPC_PRIMARY', 'W3_SEPOLIA_RPC_FALLBACK')):
        raise oracle.deployment.BlockedError('two explicit public RPC inputs required')
    return oracle.deployment.providers()


class Evidence:
    def __init__(self, root):
        self.root = root.resolve()

    def path(self, relative):
        require(isinstance(relative, str) and relative and not Path(relative).is_absolute() and '..' not in Path(relative).parts, 'relative contained evidence path required')
        path = self.root / relative
        require(path.resolve().is_relative_to(self.root) and not any(parent.is_symlink() for parent in [path, *path.parents] if parent != self.root.parent), 'evidence path escaped its directory or uses a symlink')
        if not path.exists():
            raise FileNotFoundError(relative)
        return path

    def raw(self, relative, limit=32 * 1024 * 1024):
        path = self.path(relative)
        require(path.is_file() and path.stat().st_size <= limit, 'evidence file missing or exceeds its read budget')
        return path.read_bytes()

    def json(self, relative):
        return json.loads(self.raw(relative))

    def exit(self, relative):
        value = self.raw(relative, 32).strip()
        require(re.fullmatch(b'[0-9]{1,3}', value), 'missing or malformed original process exit')
        return int(value)


def git(source, candidate, path):
    result = subprocess.run(['git', '-C', str(source), 'show', candidate + ':' + path], capture_output=True)
    if result.returncode:
        raise FileNotFoundError('verifier inputs are absent from the selected frozen candidate')
    return result.stdout


def pg_control(value, candidate):
    require(value.get('scope') == 'CW_SCRATCH_POSTGRES_RESTORE_CONTROL' and value.get('candidateSHA') == candidate and
            value.get('database') == 'streamgate_durability' and value.get('completeAcceptance') is False and value.get('ok') is True and
            value.get('oldEpoch') != value.get('newEpoch') and re.fullmatch('[0-9a-f-]{36}', str(value.get('oldEpoch'))) and
            re.fullmatch('[0-9a-f-]{36}', str(value.get('newEpoch'))) and re.fullmatch('[0-9a-f]{64}', str(value.get('dumpSHA256'))) and
            type(value.get('dumpBytes')) is int and value['dumpBytes'] > 0, 'real scratch Postgres recovery evidence is incomplete')
    require([step.get('name') for step in value.get('steps', [])] == ['before', 'dump', 'restore', 'restored', 'rotate', 'rotated'] and
            all(type(step.get('exit')) is int and step['exit'] == 0 and not step.get('timedOut') for step in value['steps']), 'original dump/restore/epoch commands did not all exit zero')


def segment_cost(value, candidate):
    require(value.get('candidateSHA') == candidate and value.get('evidenceContext') == 'CANDIDATE', 'foreign or development RPC cost evidence')
    segment_cost_component(value)


def segment_cost_component(value):
    """Semantics only; this diagnostic entry does not accept a candidate."""
    require(value.get('schemaVersion') == 1 and value.get('chainId') == 31337 and value.get('rpcCallFormat') == 'SELECTOR_AND_INPUT_DIGEST' and
            value.get('scope') == 'REAL_CW_SEGMENT_RPC' and value.get('originalExit') == 0, 'real candidate segment/RPC evidence required')
    require(re.fullmatch('0x[0-9a-fA-F]{40}', str(value.get('wallet'))) and int(value['wallet'], 16), 'deployed C required for cost measurement')
    def proofs(events):
        require(isinstance(events, list), 'original RPC events missing')
        count = 0
        for event in events:
            require(event.get('upstreamStatus') == 200 and event.get('method') and isinstance(event.get('params'), list) and 'result' in event, 'RPC observation did not complete')
            if event['method'] == 'eth_call':
                data = event['params'][0].get('data', event['params'][0].get('input', ''))
                require(event.get('inputRedacted') is True and re.fullmatch('[0-9a-f]{64}', str(event.get('inputSHA256'))) and
                        type(event.get('inputBytes')) is int and 4 <= event['inputBytes'] <= 8292 and re.fullmatch('0x[0-9a-f]{8}', data), 'RPC calldata measurement lacks redacted original size/hash')
                if data == '0x1626ba7e':
                    call = event['params'][0]
                    require(str(call.get('to', '')).lower() == value['wallet'].lower() and 0 < int(call.get('gas', '0x0'), 16) <= 500000 and
                            len(event['params']) == 2 and re.fullmatch('0x[0-9a-f]+', event['params'][1]) and
                            event['result'] == '0x1626ba7e' + '00' * 28, 'RPC proof call is unpinned, over budget, from another C or not accepted')
                    count += 1
        return count
    require(proofs(value.get('admissionRPC')) >= 1 and value.get('segmentRPC') == [] and proofs(value.get('refreshRPC')) >= 1,
            'actual 1271 admission/refresh calls or zero per-segment proof cost not established')
    segments = value.get('segments')
    lease = value['lease']
    require(all(type(lease.get(key)) is int for key in ('startedAtMs', 'finishedAtMs', 'capabilityExpiresAtMs', 'accessExpiresAtMs')) and
            0 < lease['startedAtMs'] < lease['finishedAtMs'] < lease['capabilityExpiresAtMs'] <= lease['accessExpiresAtMs'] <= lease['startedAtMs'] + 60000 and
            re.fullmatch('[0-9a-f]{64}', str(lease.get('capabilitySHA256'))), 'segment cost was measured outside the original bounded credential lease')
    require(isinstance(segments, list) and len(segments) >= 100 and all(item.get('status') == 200 and item.get('subject') == value.get('wallet') and
            type(item.get('bytes')) is int and item['bytes'] > 0 and item.get('credentialSHA256') == lease['capabilitySHA256'] and
            re.fullmatch('[0-9a-f]{64}', str(item.get('sha256'))) for item in segments), '100 actual successful C media deliveries required')
    require(all(sum(item.get('transport') == mode for item in segments) >= 100 for mode in ('http', 'grpc')),
            '100 real HTTP and 100 real gRPC deliveries required')


def local_wallet(capsule, candidate, mode):
    directory = capsule / 'wallet-browser' / ('cw-' + mode)
    for name in ('cw-browser.exit', 'cw-fixture.exit', 'cw-grpc-build.exit'):
        require((directory / name).read_text().strip() == '0', 'original local wallet process failed')
    assert_wallet_browser.validate(json.loads((directory / 'result.json').read_text()), json.loads((directory / 'wallet-fixture.json').read_text()),
        json.loads((directory / 'content.json').read_text()), candidate, mode, 0)


def verify(args):
    require(re.fullmatch('[0-9a-f]{40}', args.candidate), 'full candidate SHA required')
    evidence = Evidence(args.evidence); groups = {}; cached = {}

    def check(name, function, dependencies=()):
        if any(groups[dep]['status'] != 'PASS' for dep in dependencies):
            groups[name] = dict(status='BLOCKED', reason='prerequisite evidence did not pass', dependencies=list(dependencies)); return
        try:
            function(); groups[name] = dict(status='PASS')
        except (FileNotFoundError, ci_provenance.UnavailableError, oracle.deployment.BlockedError, oracle.network.RPCUnavailable, subprocess.TimeoutExpired):
            groups[name] = dict(status='BLOCKED', reason='required original evidence or external dependency unavailable')
        except (OSError, ValueError, KeyError, TypeError, IndexError, AttributeError, subprocess.SubprocessError, evidence_transport.TransportError):
            # Parsing/crypto libraries may embed credentials in exception text.
            # Keep the case/component name; do not echo private input values.
            groups[name] = dict(status='FAIL', reason='original evidence failed identity, semantics, integrity or process-exit validation')

    def source():
        for path in FROZEN:
            require((ROOT / path).read_bytes() == git(args.source_root, args.candidate, path), 'verifier source changed after candidate freeze')
        cached['selection'] = evidence.json('extension.json')
        selection = cached['selection']
        require(selection.get('schemaVersion') == 1 and selection.get('candidateSHA') == args.candidate and
                not any(key in selection for key in ('required', 'status', 'passed', 'completeAcceptance')), 'selection manifest cannot declare acceptance')
        if not getattr(args, 'controller', '') or not getattr(args, 'recipient', ''):
            raise oracle.deployment.BlockedError('external controller A and recipient B required')
        cached['controller'] = oracle.deployment.address(args.controller)
        cached['recipient'] = oracle.deployment.address(args.recipient)
        require(cached['recipient'] != cached['controller'] and oracle.deployment.address(selection['recipient']) == cached['recipient'], 'evidence selected another recipient')
        require(oracle.deployment.address(selection['controller']) == cached['controller'], 'evidence selected another controller')

    check('source', source)

    def ci():
        selection = cached['selection']; producer = evidence.json(selection['producer'])
        run = ci_provenance.default_run(selection['repository'], selection['ciRunID'], args.candidate)
        require(producer.get('workflow') == 'CI' and producer.get('job') == 'acceptance', 'original default acceptance producer required')
        entries = evidence_transport.verify_manifest(producer, 'ci-acceptance-' + args.candidate, args.candidate, run['id'], None, max_attempt=run['run_attempt'])
        root = evidence.path(selection['producerRoot']); capsule = evidence.path(selection['ciRoot'])
        require(capsule.is_relative_to(root), 'CI capsule escaped original producer')
        evidence_transport.verify_tree(root, entries)
        cached['originalCI'] = ci_provenance.bind_upload(selection['repository'], run, producer)
        require(json.loads((capsule / 'candidate.json').read_text())['revision'] == args.candidate, 'foreign acceptance candidate')
        assert_candidate_evidence.validate(capsule, args.source_root)
        cached['capsule'] = capsule
        wallet_helper.validate(capsule / 'wallet-build', args.candidate, git(args.source_root, args.candidate, 'test/walletgrpc/main.go'))

    check('ci', ci, ('source',))

    def runtime():
        files = subprocess.run(['git', '-C', str(args.source_root), 'ls-tree', '-r', '--name-only', args.candidate, '--', 'h5-demo'], capture_output=True, check=True).stdout.decode().splitlines()
        cached['runtime'] = wallet_runtime.inspect_stack(evidence.path(cached['selection']['stackDirectory']), args.candidate,
            cached['capsule'], lambda path: git(args.source_root, args.candidate, path), files, preparation.nginx, cached['originalCI'])
        runtime_input = evidence.json(cached['selection']['stackDirectory'] + '/stack.env.json')
        cached['jwtSecret'] = runtime_input['W3_JWT_SECRET']
        require(isinstance(cached['jwtSecret'], str) and len(cached['jwtSecret']) >= 32, 'verified runtime signing key required')

    check('public-runtime', runtime, ('ci',))

    def network():
        selection = cached['selection']; report = evidence.json(selection['networkReport'])
        require(report.get('schemaVersion') == 1 and report.get('scope') == 'ENVIRONMENT_READ_ONLY' and
                report.get('sourceSHA256') == sha(git(args.source_root, args.candidate, 'scripts/wallet-testnet/network_preflight.py')) and
                oracle.deployment.address(report.get('address')) == cached['controller'] and report.get('completeAcceptance') is False and
                report.get('walletControlVerified') is False and report.get('deploymentVerified') is False, 'original network preflight identity/boundary mismatch')
        original_exit = evidence.exit(selection['networkExit'])
        if original_exit == 2 and report.get('status') == 'BLOCKED':
            raise oracle.deployment.BlockedError('original network/funds qualification remains blocked')
        require(original_exit == 0, 'original network qualification process failed')
        endpoints = public_endpoints(); records = report['providers']
        require(isinstance(records, list) and len(records) == 2, 'two original network streams required')
        started = phases.timestamp(report['at'])
        finished = max(phases.timestamp(event['completedAt']) for record in records for event in record['events'])
        require(finished - started <= 1200, 'network preflight was unbounded')
        pair = [phases.OriginalResponses(record, endpoint, started, finished) for record, endpoint in zip(records, endpoints)]
        result = oracle.network.qualify(pair, cached['controller'], oracle.network.MAX_FEE_WEI)
        require(all(report.get(key) == value for key, value in result.items()), 'network preflight differs from original RPC bytes')
        for rpc in pair: rpc.complete()
        cached['networkFinishedAt'] = finished * 1000

    check('public-network', network, ('ci',))

    def suites():
        manifest = json.loads(git(args.source_root, args.candidate, 'scripts/cd/required-tests.json'))['suites']
        for suite, stem in SUITES.items():
            required = manifest[suite]
            require(len(required) >= (57 if suite == 'wallet-unit-tests' else 1), 'candidate required suite is incomplete')
            exit_value = (cached['capsule'] / (stem + '.exit')).read_text().strip()
            require(exit_value == '0', 'original required suite process failed')
            errors, _ = assert_required_tests.validate(cached['capsule'] / (stem + '.json'), required, 0)
            require(not errors, 'required test/subtest missing, failed or skipped')
        pg_control(json.loads((cached['capsule'] / 'wallet-postgres-restore-control.json').read_text()), args.candidate)
        log = re.sub(r'\x1b\[[0-9;]*m', '', (cached['capsule'] / 'logs/chain.log').read_text())
        tests = set(re.findall(r'function\s+(test\w+)\s*\(', git(args.source_root, args.candidate, 'contracts/test/Study1271Wallet.t.sol').decode()))
        require(len(tests) >= 20, 'wallet contract suite lost required cases')
        section = re.search(r'Ran \d+ tests for [^\n]*Study1271Wallet.t.sol:Study1271WalletTest\n(.*?Suite result:[^\n]*)', log, re.S)
        require(section and tests <= set(re.findall(r'\[PASS\]\s+(test\w+)\(', section[1])) and
                re.search(r'Suite result: ok\. \d+ passed; 0 failed; 0 skipped', section[1]), 'original full wallet Forge suite did not pass')

    check('cw-suites', suites, ('ci',))
    def segment():
        value = json.loads((cached['capsule'] / 'wallet-segment-rpc.json').read_text())
        require(value.get('sourceSHA256') == sha(git(args.source_root, args.candidate, 'test/integration/web3/contract_wallet_media_cost_anvil_test.go')), 'cost measurement source differs from candidate')
        segment_cost(value, args.candidate)
    check('cw-segment-cost', segment, ('ci',))

    for mode in MODES:
        def local(mode=mode):
            local_wallet(cached['capsule'], args.candidate, mode)
        check('local-' + mode, local, ('ci',))

    def chain():
        selection = cached['selection']; endpoints = public_endpoints()
        inputs_by_phase = {}; reports = {}; tool = sha(git(args.source_root, args.candidate, 'scripts/wallet-testnet/transaction_oracle.py'))
        for phase in oracle.PHASES:
            entry = selection['chainPhases'][phase]
            parameters = SimpleNamespace(candidate=args.candidate, controller=cached['controller'], ledger=evidence.path(entry['ledger']),
                manifest=evidence.path(selection['deploymentManifest']), build_identity=evidence.path(selection['buildIdentity']), source_root=args.source_root)
            inputs = oracle.read_inputs(parameters)
            require(oracle.deployment.address(inputs['ledger']['recipient']) == cached['recipient'], 'public transfer went to another recipient')
            require(cached['networkFinishedAt'] <= inputs['ledger']['operations'][0]['createdAt'], 'network/funds preflight did not precede the original wallet writes')
            require(parameters.build_identity.is_relative_to(cached['capsule']), 'public contracts were not compiled by original same-C CI')
            report = evidence.json(entry['report']); phases.replay(report, inputs, phase, endpoints, evidence.exit(entry['exit']), tool)
            inputs_by_phase[phase] = (inputs, report)
            reports[phase] = dict(report=report, sha256=sha(evidence.raw(entry['report'])))
        cached['chain'] = phases.recheck_history(inputs_by_phase, endpoints); cached['oracles'] = reports
        cached['transferStartedAt'] = {role: next(op['createdAt'] for op in inputs_by_phase[role + '-transferred'][0]['ledger']['operations'] if op['id'] == operation)
                                      for role, operation in [('eoa', 'transfer-a'), ('contract', 'transfer-c')]}
        pending = selection['pending']; parameters.ledger = evidence.path(pending['ledger'])
        inputs = oracle.read_inputs(parameters)
        phases.replay(evidence.json(pending['report']), inputs, pending['phase'], endpoints, evidence.exit(pending['exit']), tool, pending=True)

    check('public-chain', chain, ('ci', 'public-network', 'public-runtime'))

    for mode in MODES:
        def browser(mode=mode):
            row = cached['selection']['browserModes'][mode]
            bindings = {}
            for phase in ('eoa', 'contract', 'negative'):
                item = row[phase]; directory = evidence.path(item['directory']); original_exit = evidence.path(item['exit'])
                require(directory.is_dir(), 'original browser directory missing')
                command = ['node', str(ROOT / 'scripts/cd/assert_sepolia_browser.mjs'), '--candidate', args.candidate, '--mode', mode,
                    '--phase', phase, '--controller', cached['controller'], '--subject', cached['chain']['wallet'] if phase == 'contract' else cached['controller'],
                    '--nft', cached['chain']['nft'], '--content', row['content'], '--origin', row['origin'], '--directory', str(directory), '--exit-file', str(original_exit)]
                result = subprocess.run(command, capture_output=True, timeout=60)
                require(result.returncode == 0, 'original browser phase failed independent semantics')
                if phase != 'negative':
                    private_proofs = evidence.json(str(directory.relative_to(evidence.root) / 'siwe-proofs.private.json'))
                    tokens = [credential['token'] for proof in private_proofs for credential in proof['credentials']]
                    assert_public_controls.authenticate_credentials(tokens, cached['jwtSecret'])
                    observed = evidence.json(str(directory.relative_to(evidence.root) / 'result.json'))
                    require(cached['runtime'][mode]['startedAtMs'] <= min(gate['startedAt'] for gate in observed['gates']),
                            'current public container started after the original wallet observations')
                    gate_id = ('TN04' if phase == 'contract' else 'TN03') + '_PLAY'
                    bindings[phase] = next(gate['state']['claims'] for gate in observed['gates'] if gate['id'] == gate_id)
            cached['bindings-' + mode] = bindings
        check('public-browser-' + mode, browser, ('public-chain',))

        def controls(mode=mode):
            row = cached['selection']['browserModes'][mode]; item = row['controls']
            config = dict(candidateSHA=args.candidate, mode=mode, controller=cached['controller'], wallet=cached['chain']['wallet'],
                nft=cached['chain']['nft'], content=row['content'], origin=row['origin'], sessionBindings=cached['bindings-' + mode], transferStartedAt=cached['transferStartedAt'], jwtSecret=cached['jwtSecret'])
            assert_public_controls.validate(evidence.json(item['report']), evidence.raw(item['private']), config, cached['oracles'], evidence.exit(item['exit']))
        check('public-controls-' + mode, controls, ('public-browser-' + mode,))

    dependencies = {case: ['ci', 'cw-suites'] for case in CASES}
    for case in ('CW-01', 'CW-09', 'CW-10', 'CW-13', 'CW-14', 'CW-16'):
        dependencies[case] += ['local-' + mode for mode in MODES]
    dependencies['CW-15'].append('cw-segment-cost')
    for case in ('TN-01', 'TN-02', 'TN-07'):
        dependencies[case].append('public-chain')
    for case in ('TN-03', 'TN-04', 'TN-05', 'TN-06'):
        dependencies[case] += ['public-controls-' + mode for mode in MODES]
    dependencies['TN-08'].append('source')
    cases = {}
    for case, names in dependencies.items():
        states = [groups[name]['status'] for name in names]
        cases[case] = dict(status='FAIL' if 'FAIL' in states else 'BLOCKED' if 'BLOCKED' in states else 'PASS', components=names)
    complete = all(item['status'] == 'PASS' for item in [*groups.values(), *cases.values()])
    failed = any(item['status'] == 'FAIL' for item in [*groups.values(), *cases.values()])
    return dict(schemaVersion=1, scope='W3_EXTENSION_FIXED_MATRIX', candidateSHA=args.candidate,
        controller=str(getattr(args, 'controller', '')).lower() if re.fullmatch('0x[0-9a-fA-F]{40}', str(getattr(args, 'controller', ''))) else None,
        recipient=str(getattr(args, 'recipient', '')).lower() if re.fullmatch('0x[0-9a-fA-F]{40}', str(getattr(args, 'recipient', ''))) else None,
        status='PASS WITH BOUNDARIES' if complete else 'FAIL' if failed else 'BLOCKED', completeAcceptance=complete,
        boundaries=['local Docker/CI and Ethereum Sepolia only', 'Study1271Wallet demo, no production-wallet audit or arbitrary-wallet compatibility'],
        cases=cases, components=groups), 0 if complete else 1 if failed else 2


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', required=True); parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--controller', default=os.environ.get('W3_SEPOLIA_ADDRESS', ''), help='externally selected controller A; evidence cannot supply this authority')
    parser.add_argument('--recipient', default=os.environ.get('W3_SEPOLIA_RECIPIENT', ''), help='externally selected recipient B')
    parser.add_argument('--source-root', type=Path, default=ROOT); parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    if args.output and args.output.exists():
        print('FAIL: output already exists; original evidence preserved', file=sys.stderr); return 1
    try:
        report, code = verify(args)
        raw = json.dumps(report, indent=2) + '\n'
        if args.output:
            with args.output.open('x') as stream: stream.write(raw)
        print(raw, end=''); return code
    except (OSError, ValueError, KeyError, TypeError):
        print('FAIL: invalid extension invocation or output already exists; original evidence preserved', file=sys.stderr); return 1


if __name__ == '__main__':
    raise SystemExit(main())
