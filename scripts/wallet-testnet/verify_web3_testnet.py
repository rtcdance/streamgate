#!/usr/bin/env python3
"""Read-only Sepolia acceptance verifier and collaboration entry (plan §10).

This tool never signs, broadcasts, claims faucet funds or approves wallet
transactions. It runs the strict network preflight, re-verifies the deployment
manifest against the chain when one is recorded, and checks H5 readiness when
an origin is provided. Everything the user must perform (faucet claim, wallet
connection and signatures, broadcasts) is reported as an explicit BLOCKED item
owned by the user — never skipped, never simulated.

Status semantics (plan §10.8):
  PASS WITH BOUNDARIES  all fixed CW/TN original evidence verified
  NETWORK_READY   read-only preflight passed; no wallet acceptance implied
  TESTNET_BLOCKED environment or user inputs missing (exit 2)
  FAIL            an automatable contract is violated (exit 1)
"""
import argparse
import hashlib
import json
import os
import subprocess
import sys
import tempfile
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
REPO_ROOT = SCRIPT_DIR.parents[1]
sys.dont_write_bytecode = True

USER_GATED = [
    {'item': 'wallet_connection_and_signatures', 'owner': 'user', 'detail': 'real wallet connects A (controller) and signs SIWE messages; no automation may approve'},
    {'item': 'contract_deployment_broadcasts', 'owner': 'user', 'detail': 'N/C deployment via wallet or encrypted keystore; canonical receipts feed the manifest'},
    {'item': 'mint_and_transfer_broadcasts', 'owner': 'user', 'detail': 'fixture mints and the A->B / C.execute transfers under the §10.4 budget'},
]


def first_non_empty(*values):
    for value in values:
        if value.strip():
            return value.strip()
    return ''


def sepolia_env():
    """Map the profile inputs onto the tool env, mirroring the Go loader.

    The legacy diagnostic variable maps to the primary endpoint; the two
    spellings must never disagree. Values are forwarded to child processes and
    never printed.
    """
    primary = first_non_empty(os.environ.get('SEPOLIA_RPC_URL', ''), os.environ.get('SEPOLIA_RPC', ''))
    fallback = os.environ.get('SEPOLIA_RPC_FALLBACK_URL', '').strip()
    legacy = os.environ.get('SEPOLIA_RPC', '').strip()
    if legacy and os.environ.get('SEPOLIA_RPC_URL', '').strip() and legacy != os.environ.get('SEPOLIA_RPC_URL', '').strip():
        raise ValueError('SEPOLIA_RPC_URL and legacy SEPOLIA_RPC disagree; both must describe the same endpoint')
    return {
        'W3_SEPOLIA_RPC_PRIMARY': primary,
        'W3_SEPOLIA_RPC_FALLBACK': fallback,
    }


def run_tool(command, env_extra):
    env = os.environ.copy()
    env.update(env_extra)
    with tempfile.TemporaryDirectory() as directory:
        report_path = Path(directory) / 'tool-report.json'
        completed = subprocess.run(command + ['--output', str(report_path)], env=env, capture_output=True, text=True)
        report = None
        try:
            report = json.loads(report_path.read_text())
        except (OSError, ValueError):
            pass
        return completed.returncode, completed.stdout.strip(), completed.stderr.strip(), report


def stage_preflight(address):
    try:
        env = sepolia_env()
    except ValueError as error:
        return {'status': 'FAIL', 'reason': str(error)}
    code, out, err, report = run_tool(
        [sys.executable, str(SCRIPT_DIR / 'network_preflight.py'), '--address', address], env)
    status = 'PASS' if code == 0 else ('TESTNET_BLOCKED' if code == 2 else 'FAIL')
    return {'status': status, 'exit': code, 'stdout': out, 'stderr': err[-2000:], 'report': report}


def stage_manifest(manifest_path, candidate, controller, build_identity):
    if not candidate or not controller or build_identity is None:
        return {'status': 'TESTNET_BLOCKED', 'reason': 'external candidate SHA, controller and CI compilation index are required; manifest self-identities are not authority'}
    try:
        env = sepolia_env()
    except ValueError as error:
        return {'status': 'FAIL', 'reason': str(error)}
    code, out, err, report = run_tool(
        [sys.executable, str(SCRIPT_DIR / 'deployment_manifest.py'), 'verify', '--manifest', str(manifest_path),
         '--candidate', candidate, '--controller', controller, '--build-identity', str(build_identity)], env)
    status = 'PASS' if code == 0 else ('TESTNET_BLOCKED' if code == 2 else 'FAIL')
    return {'status': status, 'exit': code, 'stdout': out, 'stderr': err[-2000:], 'report': report}


def stage_h5(origin):
    entry = {'status': 'PASS', 'checks': []}
    try:
        with urllib.request.urlopen(origin.rstrip('/') + '/', timeout=10) as response:
            body = response.read(512 * 1024).decode('utf-8', 'replace')
            entry['checks'].append({'id': 'H5_ORIGIN_REACHABLE', 'ok': response.status == 200, 'httpStatus': response.status})
        entry['checks'].append({'id': 'H5_APP_BUNDLE_PRESENT', 'ok': 'streamgate' in body.lower() or '<div id="app' in body.lower()})
    except Exception as error:  # noqa: BLE001 - reported, never fatal to the process
        entry['status'] = 'TESTNET_BLOCKED'
        entry['reason'] = f'H5 origin unreachable: {type(error).__name__}'
        return entry
    if not all(check['ok'] for check in entry['checks']):
        entry['status'] = 'FAIL'
    return entry


def default_output_path():
    stamp = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    return REPO_ROOT / f'.verify-artifacts/w3-extension/testnet/{stamp}-verify-web3-testnet.json'


def extension_stage(args):
    if not args.candidate or not args.address or not args.recipient:
        return 2, None, None
    code, _, _, result = run_tool([sys.executable, str(REPO_ROOT / 'scripts/cd/verify_web3_extension.py'),
        '--candidate', args.candidate, '--controller', args.address, '--recipient', args.recipient, '--evidence', str(args.evidence)], sepolia_env())
    if code == 0:
        required = {f'CW-{i:02}' for i in range(1, 17)} | {f'TN-{i:02}' for i in range(1, 9)}
        if not isinstance(result, dict) or result.get('scope') != 'W3_EXTENSION_FIXED_MATRIX' or result.get('candidateSHA') != args.candidate or \
                str(result.get('controller', '')).lower() != args.address.lower() or result.get('status') != 'PASS WITH BOUNDARIES' or result.get('completeAcceptance') is not True or \
                str(result.get('recipient', '')).lower() != args.recipient.lower() or \
                set(result.get('cases', {})) != required or any(row.get('status') != 'PASS' for row in result['cases'].values()) or \
                not result.get('components') or any(row.get('status') != 'PASS' for row in result['components'].values()):
            return 1, None, code
    return code if code in (0, 1, 2) else 1, result, code


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--address', default=os.environ.get('W3_SEPOLIA_ADDRESS', ''), help='controller address A (0x…)')
    parser.add_argument('--manifest', type=Path, default=os.environ.get('W3_DEPLOYMENT_MANIFEST') or None, help='deployment manifest to re-verify')
    parser.add_argument('--candidate', default=os.environ.get('W3_CANDIDATE_SHA', ''), help='externally selected frozen candidate SHA')
    parser.add_argument('--build-identity', type=Path, default=os.environ.get('W3_BUILD_IDENTITY') or None, help='original candidate CI compilation index')
    parser.add_argument('--h5-origin', default=os.environ.get('W3_H5_ORIGIN', ''), help='H5 origin to readiness-check (read-only)')
    parser.add_argument('--stage', choices=['preflight'], default=None, help='run a single stage instead of the full entry')
    parser.add_argument('--output', type=Path, default=None)
    parser.add_argument('--evidence', type=Path, default=os.environ.get('W3_EVIDENCE_DIR') or None, help='original fixed-matrix evidence directory; enables full closeout')
    parser.add_argument('--recipient', default=os.environ.get('W3_SEPOLIA_RECIPIENT', ''), help='externally selected recipient B')
    args = parser.parse_args(argv)

    report = {
        'schemaVersion': 1,
        'scope': 'ENVIRONMENT_READ_ONLY',
        'walletControlVerified': False,
        'deploymentVerified': False,
        'completeAcceptance': False,
        'at': datetime.now(timezone.utc).isoformat(),
        'tool': 'verify_web3_testnet.py',
        'toolSHA256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        'stages': {},
        'userGated': [dict(item, status='BLOCKED') for item in USER_GATED],
        'faucet': {'provider': 'Google Cloud Ethereum Sepolia', 'status': 'NOT_REQUESTED',
                   'required': False, 'reason': 'funding is checked from the pinned on-chain balance with the fee reserve'},
    }

    if args.evidence is not None and args.stage is None:
        output = args.output or default_output_path()
        if output.exists():
            print('FAIL: output already exists; original evidence preserved'); return 1
        try: code, result, original_exit = extension_stage(args)
        except (ValueError, OSError, TypeError, KeyError): code, result, original_exit = 1, None, None
        if code == 0:
            report = result
            report['entryOriginalExit'] = original_exit
        else:
            report.update(status='FAIL' if code == 1 else 'TESTNET_BLOCKED')
            report['stages']['extension'] = {'status': report['status'], 'exit': original_exit, 'report': result}
        output.parent.mkdir(parents=True, exist_ok=True)
        with output.open('x') as stream: stream.write(json.dumps(report, indent=2) + '\n')
        print(report['status'] + ': fixed CW/TN evidence; no wallet approvals performed')
        return code

    if args.stage == 'preflight':
        if not args.address:
            report['status'] = 'TESTNET_BLOCKED'
            report['stages']['preflight'] = {'status': 'TESTNET_BLOCKED', 'reason': 'controller address A not provided'}
            output = args.output or default_output_path()
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_text(json.dumps(report, indent=2) + '\n')
            print(f"BLOCKED: --address (or W3_SEPOLIA_ADDRESS) is required for the preflight stage; report written to {output}")
            return 2
        report['stages']['preflight'] = stage_preflight(args.address)
    else:
        if not args.address:
            report['stages']['preflight'] = {'status': 'TESTNET_BLOCKED', 'reason': 'controller address A not provided'}
        else:
            report['stages']['preflight'] = stage_preflight(args.address)
        if args.manifest is None:
            report['stages']['manifest'] = {'status': 'TESTNET_BLOCKED', 'reason': 'no deployment manifest recorded yet; user broadcasts pending'}
        else:
            report['stages']['manifest'] = stage_manifest(args.manifest, args.candidate, args.address, args.build_identity)
        if args.h5_origin:
            report['stages']['h5'] = stage_h5(args.h5_origin)
        else:
            report['stages']['h5'] = {'status': 'TESTNET_BLOCKED', 'reason': 'no H5 origin provided'}

    statuses = {stage['status'] for stage in report['stages'].values()}
    if 'FAIL' in statuses:
        report['status'] = 'FAIL'
        code = 1
    elif args.stage == 'preflight' and statuses == {'PASS'}:
        report['status'] = 'NETWORK_READY'
        code = 0
    else:
        report['status'] = 'TESTNET_BLOCKED'
        code = 2

    output = args.output
    if output is None:
        output = default_output_path()
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2) + '\n')
    print(f"{report['status']}: report written to {output}")
    return code


if __name__ == '__main__':
    raise SystemExit(main())
