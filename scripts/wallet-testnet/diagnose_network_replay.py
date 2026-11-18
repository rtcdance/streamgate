#!/usr/bin/env python3
"""Localise a public-network gate FAIL without weakening the frozen verifier.

verify_web3_extension.py collapses every exception in the gate into one FAIL
reason so that credentials embedded in parser messages never reach a report.
This tool replays the same steps, with the verifier's own module instances,
over one original preflight report and prints which step failed, the exception
type and the stack frames. It never prints exception messages, endpoint URLs or
response values, and it refuses all network access: the replay is offline.
Exit 0 = every step passed, 1 = a step failed, 2 = inputs unusable.
"""
import argparse
from pathlib import Path
import sys
import traceback
from unittest.mock import patch
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts/cd'))
sys.dont_write_bytecode = True


def frames(error):
    return [f'{Path(f.filename).name}:{f.lineno}:{f.name}' for f in traceback.extract_tb(error.__traceback__)]


def offline(*_args, **_kwargs):
    raise AssertionError('network access refused: the diagnosis replays original bytes only')


def diagnose(report, original_exit, controller, verifier, out=print):
    phases, oracle = verifier.phases, verifier.oracle
    state = {}

    def step(name, function):
        try:
            value = function()
        except Exception as error:  # report the type and location, never the message
            out(f'FAIL {name}: {type(error).__module__}.{type(error).__name__}')
            for frame in frames(error):
                out(f'  at {frame}')
            return False
        out(f'PASS {name}' + (f' ({value})' if isinstance(value, str) and value else ''))
        return True

    def boundary():
        verifier.require(report.get('schemaVersion') == 1 and report.get('scope') == 'ENVIRONMENT_READ_ONLY' and
                         report.get('completeAcceptance') is False and report.get('walletControlVerified') is False and
                         report.get('deploymentVerified') is False, 'boundary fields')
        verifier.require(oracle.deployment.address(report.get('address')) == oracle.deployment.address(controller), 'controller')
        return f"status={report.get('status')}"

    def process_exit():
        verifier.require(original_exit == 0, 'original exit')

    def endpoints():
        state['endpoints'] = verifier.public_endpoints()
        records = report['providers']
        verifier.require(isinstance(records, list) and len(records) == 2, 'two streams')
        state['records'] = records
        return 'slots=' + ','.join(str(r.get('slot')) for r in records)

    def window():
        records = state['records']
        state['started'] = phases.timestamp(report['at'])
        state['finished'] = max(phases.timestamp(e['completedAt']) for r in records for e in r['events'])
        span = state['finished'] - state['started']
        verifier.require(span <= 1200, 'unbounded')
        return f'span={span:.1f}s events=' + ','.join(str(len(r['events'])) for r in records)

    def stream(index):
        def build():
            pair = state.setdefault('pair', [None, None])
            pair[index] = phases.OriginalResponses(state['records'][index], state['endpoints'][index], state['started'], state['finished'])
        return build

    def qualify():
        state['result'] = oracle.network.qualify(state['pair'], oracle.deployment.address(controller), oracle.network.MAX_FEE_WEI)

    def fields():
        differing = sorted(key for key, value in state['result'].items() if report.get(key) != value)
        verifier.require(not differing, 'fields differ')
        return ''

    def complete(index):
        def check():
            state['pair'][index].complete()
            return f"consumed={state['pair'][index].cursor}"
        return check

    steps = [('boundary', boundary), ('original-exit', process_exit), ('endpoints', endpoints), ('window', window),
             ('stream-0', stream(0)), ('stream-1', stream(1)), ('qualify', qualify), ('fields', fields),
             ('complete-0', complete(0)), ('complete-1', complete(1))]
    with patch.object(urllib.request, 'urlopen', offline):
        for name, function in steps:
            if not step(name, function):
                if name == 'fields':
                    differing = sorted(k for k, v in state['result'].items() if report.get(k) != v)
                    out('  differing keys: ' + ', '.join(differing))
                return 1
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--report', type=Path, required=True, help='original *.network.json')
    parser.add_argument('--exit-file', type=Path, required=True, help='original *.network.exit')
    parser.add_argument('--controller', required=True)
    args = parser.parse_args()
    import json
    try:
        report = json.loads(args.report.read_text())
        original_exit = int(args.exit_file.read_text().strip())
    except (OSError, ValueError) as error:
        print(f'UNUSABLE input: {type(error).__name__}')
        return 2
    import verify_web3_extension as verifier
    return diagnose(report, original_exit, args.controller, verifier)


if __name__ == '__main__':
    raise SystemExit(main())
