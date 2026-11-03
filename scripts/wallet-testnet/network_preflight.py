#!/usr/bin/env python3
"""Read-only Sepolia network qualification, never wallet-control or deployment acceptance."""
import argparse
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import time
import urllib.error
import urllib.parse
import urllib.request

CHAIN_ID = 11155111
GENESIS = '0x25a5cc106eea7138acab33231d7160d69cb777ee0c2c553fcddf5138993e6dd9'
MAX_FEE_WEI = 20_000_000_000_000_000
READ_METHODS = {'eth_chainId', 'eth_getBlockByNumber', 'eth_getCode', 'eth_getBalance', 'eth_call'}


def quantity(value):
    if not isinstance(value, str) or not re.fullmatch(r'0x[0-9a-fA-F]+', value):
        raise ValueError('invalid RPC quantity')
    return int(value, 16)


def header(value, expected=None):
    if not isinstance(value, dict) or not re.fullmatch(r'0x[0-9a-fA-F]{64}', value.get('hash', '')):
        raise ValueError('missing or malformed block header')
    height = quantity(value.get('number'))
    if expected is not None and height != expected:
        raise ValueError('RPC substituted a different block')
    return {'number': height, 'hash': value['hash'].lower()}


class ReadOnlyRPC:
    def __init__(self, endpoint, slot):
        try:
            parsed = urllib.parse.urlparse(endpoint)
            _ = parsed.port
        except ValueError:
            raise ValueError('invalid HTTPS endpoint') from None
        if parsed.scheme != 'https' or not parsed.hostname or parsed.username or parsed.password:
            raise ValueError('an explicit HTTPS endpoint is required')
        self.endpoint, self.slot, self.host = endpoint, slot, parsed.hostname.lower()
        self.last_call = 0
        self.events = []

    def call(self, method, params):
        if method not in READ_METHODS:
            raise ValueError('non-read-only RPC refused')
        # Each operator stays below two RPC messages per second, with no
        # transparent retries that could multiply free-tier usage.
        delay = 0.6 - (time.monotonic() - self.last_call)
        if delay > 0:
            time.sleep(delay)
        self.last_call = time.monotonic()
        number = len(self.events) + 1
        event = {'id': number, 'method': method, 'params': params, 'at': datetime.now(timezone.utc).isoformat()}
        self.events.append(event)
        body = json.dumps({'jsonrpc': '2.0', 'id': number, 'method': method, 'params': params}).encode()
        request = urllib.request.Request(self.endpoint, data=body, headers={'Content-Type': 'application/json', 'User-Agent': 'StreamGate-readonly-preflight/1.0'})
        try:
            with urllib.request.urlopen(request, timeout=8) as response:
                data = response.read(1024 * 1024 + 1)
            if len(data) > 1024 * 1024:
                raise ValueError('RPC response budget exceeded')
            value = json.loads(data)
            if not isinstance(value, dict) or value.get('id') != number or value.get('jsonrpc') != '2.0':
                raise ValueError('invalid RPC envelope')
            if value.get('error'):
                error_value = value['error']
                if not isinstance(error_value, dict) or not isinstance(error_value.get('code'), int):
                    raise ValueError('invalid RPC error envelope')
                event['rpcErrorCode'] = error_value['code']
                raise ValueError(f'RPC {method} rejected with code {event["rpcErrorCode"]}')
            if 'result' not in value:
                raise ValueError('RPC result missing')
            event['result'] = value['result']
            return value['result']
        except urllib.error.HTTPError as error:
            event['httpStatus'] = error.code
            raise ValueError(f'RPC slot {self.slot} HTTP {error.code}') from None
        except (urllib.error.URLError, OSError):
            event['transportError'] = True
            raise ValueError(f'RPC slot {self.slot} unavailable') from None


def network_head(rpc):
    if quantity(rpc.call('eth_chainId', [])) != CHAIN_ID:
        raise ValueError('wrong chain ID')
    genesis = header(rpc.call('eth_getBlockByNumber', ['0x0', False]), 0)
    if genesis['hash'] != GENESIS:
        raise ValueError('wrong genesis identity')
    safe = header(rpc.call('eth_getBlockByNumber', ['safe', False]))
    finalized = header(rpc.call('eth_getBlockByNumber', ['finalized', False]))
    if finalized['number'] > safe['number']:
        raise ValueError('finalized head is ahead of safe')
    return {'genesis': genesis, 'safe': safe, 'finalized': finalized}


def pinned_state(rpc, address, height):
    tag = hex(height)
    before = header(rpc.call('eth_getBlockByNumber', [tag, False]), height)
    code = rpc.call('eth_getCode', [address, tag])
    if code != '0x':
        raise ValueError('controller must be a plain EOA, without contract/delegation code')
    balance = quantity(rpc.call('eth_getBalance', [address, tag]))
    # The SHA-256 precompile gives a deterministic historical eth_call oracle.
    # State-changing contract history is checked separately after deployment.
    output = rpc.call('eth_call', [{'to': '0x0000000000000000000000000000000000000002', 'data': '0x000102', 'gas': '0x186a0'}, tag])
    if output != '0x' + hashlib.sha256(bytes.fromhex('000102')).hexdigest():
        raise ValueError('historical eth_call oracle mismatch')
    after = header(rpc.call('eth_getBlockByNumber', [tag, False]), height)
    if before != after:
        raise ValueError('pinned anchor changed during qualification')
    return {'block': before, 'code': code, 'balanceWei': str(balance), 'historicalCall': 'SHA256_PRECOMPILE_PASS'}


def qualify(rpcs, address, max_fee_wei):
    if not re.fullmatch(r'0x[0-9a-fA-F]{40}', address) or int(address, 16) == 0:
        raise ValueError('nonzero EVM controller address required')
    if len(rpcs) != 2 or len({r.host for r in rpcs}) != 2:
        raise ValueError('two distinct configured HTTPS provider hosts required')
    if not 0 < max_fee_wei <= MAX_FEE_WEI:
        raise ValueError('fee ceiling cannot exceed 0.02 Sepolia ETH')
    with ThreadPoolExecutor(max_workers=2) as pool:
        heads = list(pool.map(network_head, rpcs))
        height = min(h['safe']['number'] for h in heads)
        states = list(pool.map(lambda r: pinned_state(r, address, height), rpcs))
    if states[0] != states[1]:
        raise ValueError('providers disagree at the pinned safe anchor')
    if int(states[0]['balanceWei']) * 5 < max_fee_wei * 6:
        raise ValueError('balance does not cover the declared fee ceiling plus 20 percent reserve')
    return {'status': 'NETWORK_READS_PASS', 'chainId': CHAIN_ID, 'genesisHash': GENESIS, 'heads': heads, 'pinnedState': states[0], 'feeCeilingWei': str(max_fee_wei)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--address', required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--max-fee-wei', type=int, default=MAX_FEE_WEI)
    args = parser.parse_args()
    report = {'schemaVersion': 1, 'scope': 'ENVIRONMENT_READ_ONLY', 'walletControlVerified': False, 'deploymentVerified': False, 'completeAcceptance': False, 'at': datetime.now(timezone.utc).isoformat(), 'sourceSHA256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), 'address': args.address}
    rpcs = []
    try:
        for slot, key in enumerate(('W3_SEPOLIA_RPC_PRIMARY', 'W3_SEPOLIA_RPC_FALLBACK')):
            rpcs.append(ReadOnlyRPC(os.environ.get(key, ''), slot))
        report.update(qualify(rpcs, args.address, args.max_fee_wei))
    except (ValueError, OSError, KeyError, TypeError) as error:
        report.update(status='BLOCKED', reason=str(error))
    finally:
        report['providers'] = [{'slot': r.slot, 'endpointSHA256': hashlib.sha256(r.endpoint.encode()).hexdigest(), 'events': r.events} for r in rpcs]
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, indent=2) + '\n')
    print(f'{report["status"]}: read-only network qualification; wallet control and deployment are NOT verified')
    return 0 if report['status'] == 'NETWORK_READS_PASS' else 2


if __name__ == '__main__':
    raise SystemExit(main())
