"""Real LOCAL Anvil decoder regression. Never public Sepolia acceptance."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import unittest
import urllib.request

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('local_oracle_decoder', ROOT / 'scripts/wallet-testnet/transaction_oracle.py')
tool = importlib.util.module_from_spec(spec); spec.loader.exec_module(tool)


class LocalReader:
    def __init__(self, endpoint): self.endpoint = endpoint

    def request(self, method, params):
        body = json.dumps(dict(jsonrpc='2.0', id=1, method=method, params=params)).encode()
        with urllib.request.urlopen(urllib.request.Request(self.endpoint, data=body, headers={'Content-Type': 'application/json'}), timeout=5) as response:
            value = json.load(response)
        if value.get('error'): raise ValueError('local fixture RPC failed: ' + str(value['error'].get('code')))
        return value['result']

    def call(self, method, params):
        if method not in tool.deployment.DeploymentRPC.allowed_methods: raise ValueError('decoder attempted a write')
        return self.request(method, params)

    def fixture_write(self, method, params):
        # This is a separate test-only writer. The endpoint belongs to the
        # child below and must still report 31337 before EACH unlocked write.
        if method not in {'eth_sendTransaction', 'anvil_mine'} or self.request('eth_chainId', []) != hex(31337):
            raise ValueError('local fixture write refused')
        return self.request(method, params)


class RealAnvilDecoder(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if not shutil.which('anvil') or not shutil.which('cast'): raise RuntimeError('pinned Anvil/Foundry is required, never SKIP')
        cls.temporary = tempfile.TemporaryDirectory(prefix='sg-oracle-anvil-'); cls.addClassCleanup(cls.temporary.cleanup)
        cls.output = Path(os.environ.get('W3_ORACLE_ARTIFACT_DIR', cls.temporary.name))
        cls.output.mkdir(parents=True, exist_ok=True)
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0)); port = listener.getsockname()[1]
        log = open(Path(cls.temporary.name) / 'anvil.log', 'w'); cls.addClassCleanup(log.close)
        cls.process = subprocess.Popen(['anvil', '--host', '127.0.0.1', '--port', str(port), '--chain-id', '31337', '--silent'],
            stdout=log, stderr=subprocess.STDOUT, env=dict(os.environ, RAYON_NUM_THREADS='1', TOKIO_WORKER_THREADS='1'))
        cls.addClassCleanup(cls.stop_child)
        cls.reader = LocalReader('http://127.0.0.1:' + str(port))
        deadline = time.monotonic() + 15
        while True:
            if cls.process.poll() is not None: raise RuntimeError('owned Anvil child exited before readiness')
            try:
                if cls.reader.call('eth_chainId', []) == hex(31337): break
            except OSError: pass
            if time.monotonic() > deadline: raise RuntimeError('owned Anvil child readiness timeout')
            time.sleep(.1)
        cls.A, cls.B = cls.reader.request('eth_accounts', [])[:2]

    @classmethod
    def stop_child(cls):
        if cls.process.poll() is None:
            cls.process.terminate()
            try: cls.process.wait(timeout=5)
            except subprocess.TimeoutExpired: cls.process.kill(); cls.process.wait(timeout=5)

    def submit_local(self, name, data, target=None):
        self.assertIsNone(self.process.poll())
        intent = {'from': self.A, 'chainId': hex(31337), 'data': '0x' + data.hex(), 'value': '0x0',
            'nonce': self.reader.request('eth_getTransactionCount', [self.A, 'pending'])}
        if target: intent['to'] = target
        gas = tool.network.quantity(self.reader.request('eth_estimateGas', [intent]))
        head = self.reader.call('eth_getBlockByNumber', ['latest', False]); tip = 10**9
        intent.update(gas=hex((gas * 6 + 4) // 5), maxFeePerGas=hex(2 * tool.network.quantity(head['baseFeePerGas']) + tip), maxPriorityFeePerGas=hex(tip))
        # The LOCAL fixture also saves its unsigned intent before broadcasting.
        (self.output / (name + '.intent.json')).write_text(json.dumps(intent, indent=2) + '\n')
        tx_hash = self.reader.fixture_write('eth_sendTransaction', [intent])
        pending, deadline = [], time.monotonic() + 10
        while True:
            receipt = self.reader.call('eth_getTransactionReceipt', [tx_hash])
            pending.append(dict(atMs=int(time.time() * 1000), receiptAvailable=receipt is not None))
            if receipt is not None: break
            if time.monotonic() >= deadline: raise RuntimeError('original local transaction receipt timeout; never rebroadcast')
            time.sleep(.05)
        (self.output / (name + '.broadcast.json')).write_text(json.dumps(dict(hash=tx_hash, originalBroadcastCount=1, receiptPolls=pending), indent=2) + '\n')
        fact = tool.transaction_facts(self.reader, tx_hash, self.A, target, data, intent, expected_chain=31337)
        (self.output / (name + '.chain.json')).write_text(json.dumps(fact, indent=2) + '\n')
        return fact

    def test_six_real_local_transactions_and_four_safe_holdings(self):
        contracts = {}
        archive = ROOT / 'contracts/out'
        for role, name in tool.deployment.ROLES.items():
            path = archive / (name + '.sol') / (name + '.json')
            value = json.loads(path.read_text())
            contracts[role] = dict(creation=bytes.fromhex(value['bytecode']['object'][2:]), runtime=bytes.fromhex(value['deployedBytecode']['object'][2:]),
                artifactSHA256=hashlib.sha256(path.read_bytes()).hexdigest())
        facts, states, tokens = [], [], {}
        for name in tool.OPS:
            target = None
            if name == 'deploy-nft': data = contracts['nft']['creation']
            elif name == 'deploy-wallet': data = contracts['wallet']['creation'] + tool.account_word(self.A)
            elif name.startswith('mint-'):
                target = contracts['nft']['address']; data = tool.selector('mint(address)') + tool.account_word(self.A if name == 'mint-a' else contracts['wallet']['address'])
            else:
                owner = self.A if name == 'transfer-a' else contracts['wallet']['address']; token = tokens['mint-a' if name == 'transfer-a' else 'mint-c']
                target = contracts['nft']['address'] if name == 'transfer-a' else contracts['wallet']['address']
                data = tool.transfer_data(owner, self.B, token)
                if name == 'transfer-c': data = tool.execute_data(contracts['nft']['address'], data)
            fact = self.submit_local(name, data, target); receipt = fact['receipt']; facts.append(fact)
            if name.startswith('deploy-'):
                contracts['nft' if name == 'deploy-nft' else 'wallet']['address'] = receipt['contractAddress']
            elif name.startswith('mint-'):
                tokens[name] = tool.nft_transfer(receipt, contracts['nft']['address'], tool.ZERO, self.A if name == 'mint-a' else contracts['wallet']['address'])
            else:
                self.assertEqual(token, tool.nft_transfer(receipt, contracts['nft']['address'], owner, self.B, token))
            if name not in ('deploy-nft', 'deploy-wallet'):
                self.reader.fixture_write('anvil_mine', ['0x40'])
                safe = tool.network.header(self.reader.call('eth_getBlockByNumber', ['safe', False]))
                self.assertGreaterEqual(safe['number'], fact['block']['number'])
                state = tool.state_at(self.reader, dict(contracts=contracts, controller=self.A), self.B, tokens, safe)
                expected = {'mint-a': ('1', '0', '0'), 'transfer-a': ('0', '0', '1'), 'mint-c': ('0', '1', '1'), 'transfer-c': ('0', '0', '2')}[name]
                self.assertEqual(expected, tuple(state['balances'][role] for role in ('A', 'C', 'B')))
                states.append(dict(operation=name, anchor=safe, state=state))
        self.assertEqual(6, len(facts)); self.assertEqual(4, len(states)); self.assertEqual(2, len(set(tokens.values())))
        report = dict(component='LOCAL_ANVIL_ORACLE_DECODER', chainId=31337, completeAcceptance=False, publicNetworkVerified=False,
                      operations=facts, states=states, contracts={role: {key: value for key, value in entry.items() if key not in ('creation', 'runtime')} for role, entry in contracts.items()})
        (self.output / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
        print('LOCAL_ORACLE_DECODER: 6 canonical local transactions, 4 safe states; public acceptance=false')

    def test_public_transport_refuses_the_local_fixture(self):
        with self.assertRaisesRegex(ValueError, 'HTTPS'): tool.deployment.DeploymentRPC(self.reader.endpoint, 0)
        with self.assertRaisesRegex(ValueError, 'decoder attempted a write'): self.reader.call('eth_sendTransaction', [])


if __name__ == '__main__':
    unittest.main()
