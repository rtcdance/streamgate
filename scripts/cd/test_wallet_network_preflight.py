import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock
import urllib.error

sys.dont_write_bytecode = True
SCRIPT = Path(__file__).resolve().parent.parent / 'wallet-testnet/network_preflight.py'
spec = importlib.util.spec_from_file_location('wallet_network_preflight', SCRIPT)
network = importlib.util.module_from_spec(spec)
spec.loader.exec_module(network)
ADDRESS = '0x732E24a0BcAb6fA41b1C9148BCF1135587bC76c1'


class RPCFixture:
    def __init__(self, host, fault=''):
        self.host, self.fault = host, fault
        self.calls = []
        self.pinned_reads = 0

    def call(self, method, params):
        self.calls.append((method, params))
        if method not in network.READ_METHODS:
            raise AssertionError('write attempted')
        if method == 'eth_chainId':
            return '0x1' if self.fault == 'chain' else hex(network.CHAIN_ID)
        if method == 'eth_getBlockByNumber':
            tag = params[0]
            if tag == '0x0':
                if self.fault == 'pruned':
                    raise ValueError('history unavailable')
                return {'number': '0x0', 'hash': '0x' + '00' * 32 if self.fault == 'genesis' else network.GENESIS}
            if tag == 'safe':
                return None if self.fault == 'no-safe' else {'number': '0x64', 'hash': '0x' + '11' * 32}
            if tag == 'finalized':
                return {'number': '0x65' if self.fault == 'finalized' else '0x60', 'hash': '0x' + '22' * 32}
            self.pinned_reads += 1
            h = '33' if self.fault == 'disagree' or (self.fault == 'reorg' and self.pinned_reads > 1) else '11'
            return {'number': tag, 'hash': '0x' + h * 32}
        if method == 'eth_getCode':
            return '0xef0100' + '11' * 20 if self.fault == 'code' else '0x'
        if method == 'eth_getBalance':
            return hex(1 if self.fault == 'funds' else 10**18)
        if method == 'eth_call':
            return '0x' if self.fault == 'call' else '0x' + network.hashlib.sha256(bytes.fromhex('000102')).hexdigest()
        raise AssertionError(method)


class NetworkPreflightTests(unittest.TestCase):
    def test_coherent_network_is_only_network_pass(self):
        rpcs = [RPCFixture('primary.example'), RPCFixture('fallback.example')]
        report = network.qualify(rpcs, ADDRESS, network.MAX_FEE_WEI)
        self.assertEqual(report['status'], 'NETWORK_READS_PASS')
        self.assertEqual(report['pinnedState']['block']['number'], 100)
        self.assertTrue(all(method in network.READ_METHODS for r in rpcs for method, _ in r.calls))
        for rpc in rpcs:
            for method, params in rpc.calls:
                if method in ('eth_getCode', 'eth_getBalance', 'eth_call'):
                    self.assertEqual(params[-1], '0x64')

    def test_network_and_state_faults_refuse(self):
        for fault in ('chain', 'genesis', 'pruned', 'no-safe', 'finalized', 'disagree', 'code', 'funds', 'call', 'reorg'):
            with self.subTest(fault=fault), self.assertRaises(ValueError):
                network.qualify([RPCFixture('primary.example'), RPCFixture('fallback.example', fault)], ADDRESS, network.MAX_FEE_WEI)

    def test_configuration_rejected_before_rpc(self):
        for address, fee, same_host in [(ADDRESS, 0, False), (ADDRESS, network.MAX_FEE_WEI + 1, False), ('0x0', 1, False), (ADDRESS, 1, True)]:
            rpcs = [RPCFixture('primary.example'), RPCFixture('primary.example' if same_host else 'fallback.example')]
            with self.subTest(address=address, fee=fee, same_host=same_host), self.assertRaises(ValueError):
                network.qualify(rpcs, address, fee)
            self.assertEqual(sum(len(r.calls) for r in rpcs), 0)

    def test_no_broadcast_and_https_required(self):
        for url in ('http://anvil:8545', '', 'https://user:secret@example.com', 'https://example.com:secret'):
            with self.subTest(url=url), self.assertRaises(ValueError):
                network.ReadOnlyRPC(url, 0)
        rpc = network.ReadOnlyRPC('https://example.com', 0)
        with self.assertRaises(ValueError):
            rpc.call('eth_sendRawTransaction', ['0x1234'])
        self.assertEqual(rpc.events, [])

    def test_missing_rpc_cli_exits_blocked_without_claiming_control(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / 'result.json'
            env = os.environ.copy()
            env.update(W3_SEPOLIA_RPC_PRIMARY='', W3_SEPOLIA_RPC_FALLBACK='')
            result = subprocess.run([sys.executable, str(SCRIPT), '--address', ADDRESS, '--output', str(output)], env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 2)
            report = json.loads(output.read_text())
            self.assertEqual(report['status'], 'BLOCKED')
            self.assertFalse(report['walletControlVerified'])
            self.assertFalse(report['deploymentVerified'])
            self.assertFalse(report['completeAcceptance'])

    def test_provider_outage_has_distinct_sanitized_dependency_error(self):
        errors = [urllib.error.HTTPError('https://secret.example/credential', 503, 'secret-marker', {}, None),
                  urllib.error.URLError('secret-marker')]
        for error in errors:
            rpc = network.ReadOnlyRPC('https://example.com', 0)
            with mock.patch.object(network.urllib.request, 'urlopen', side_effect=error), self.assertRaises(network.RPCUnavailable) as caught:
                rpc.call('eth_chainId', [])
            self.assertNotIn('secret', str(caught.exception)); self.assertEqual(1, len(rpc.events))
        response = mock.MagicMock()
        response.__enter__.return_value.read.return_value = json.dumps(dict(jsonrpc='2.0', id=1, error=dict(code=-32005, message='secret-marker'))).encode()
        rpc = network.ReadOnlyRPC('https://example.com', 0)
        with mock.patch.object(network.urllib.request, 'urlopen', return_value=response), self.assertRaises(network.RPCUnavailable) as caught:
            rpc.call('eth_chainId', [])
        self.assertNotIn('secret', str(caught.exception)); self.assertEqual(-32005, rpc.events[0]['rpcErrorCode'])


if __name__ == '__main__':
    unittest.main()
