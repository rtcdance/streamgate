#!/usr/bin/env python3
"""Read-only Sepolia acceptance verifier and collaboration entry (plan §10).

This tool never signs, broadcasts, claims faucet funds or approves wallet
transactions. It runs the strict network preflight, re-verifies the deployment
manifest against the chain when one is recorded, and checks H5 readiness when
an origin is provided. Everything the user must perform (faucet claim, wallet
connection and signatures, broadcasts) is reported as an explicit BLOCKED item
owned by the user — never skipped, never simulated.

Status semantics (plan §10.8):
  LOCAL_PASS      every automatable check passed; user-gated items remain
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
    {'item': 'sepolia_faucet_claim', 'owner': 'user', 'detail': 'Google Cloud Web3 faucet with the user account; on-chain balance is the only proof'},
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
    code, out, err, report = run_tool(
        [sys.executable, str(SCRIPT_DIR / 'network_preflight.py'), '--address', address], sepolia_env())
    status = 'PASS' if code == 0 else ('TESTNET_BLOCKED' if code == 2 else 'FAIL')
    return {'status': status, 'exit': code, 'stdout': out, 'stderr': err[-2000:], 'report': report}


def stage_manifest(manifest_path):
    code, out, err, report = run_tool(
        [sys.executable, str(SCRIPT_DIR / 'deployment_manifest.py'), 'verify', '--manifest', str(manifest_path)], sepolia_env())
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


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--address', default=os.environ.get('W3_SEPOLIA_ADDRESS', ''), help='controller address A (0x…)')
    parser.add_argument('--manifest', type=Path, default=None, help='deployment manifest to re-verify')
    parser.add_argument('--h5-origin', default='', help='H5 origin to readiness-check (read-only)')
    parser.add_argument('--stage', choices=['preflight'], default=None, help='run a single stage instead of the full entry')
    parser.add_argument('--output', type=Path, default=None)
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
    }

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
            report['stages']['manifest'] = stage_manifest(args.manifest)
        if args.h5_origin:
            report['stages']['h5'] = stage_h5(args.h5_origin)
        else:
            report['stages']['h5'] = {'status': 'TESTNET_BLOCKED', 'reason': 'no H5 origin provided'}

    statuses = {stage['status'] for stage in report['stages'].values()}
    if 'FAIL' in statuses:
        report['status'] = 'FAIL'
        code = 1
    elif statuses <= {'PASS'}:
        # Every automatable check passed; the user-gated flows stay explicit.
        report['status'] = 'LOCAL_PASS'
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
