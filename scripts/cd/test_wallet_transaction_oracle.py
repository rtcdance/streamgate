"""Adversarial protocol controls; fake providers never count as public evidence."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock
from datetime import datetime, timezone

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('wallet_transaction_oracle', Path(__file__).resolve().parents[1] / 'wallet-testnet/transaction_oracle.py')
tool = importlib.util.module_from_spec(spec); spec.loader.exec_module(tool)
A, B, N, C = ('0x' + digit * 40 for digit in ('1', '2', '3', '4'))
SHA = 'a' * 40


def block(height): return {'number': hex(height), 'hash': '0x' + format(height, '064x')}


def fixture(phase):
    inputs = dict(candidate=SHA, controller=A, ledgerSHA256='b' * 64, manifestSHA256='c' * 64,
                  compilation={'candidateSHA': SHA, 'sha256': 'd' * 64}, contracts={
        'nft': dict(address=N, creation=b'\x60\x01', runtime=b'\x60\x03', creationTxHash='0x' + format(1, '064x')),
        'wallet': dict(address=C, creation=b'\x60\x02', runtime=b'\x60\x04', creationTxHash='0x' + format(2, '064x'))})
    ledger = dict(schemaVersion=1, candidateSHA=SHA, controller=A, chainId=tool.network.CHAIN_ID, recipient=B,
                  completeAcceptance=False, publicWalletSignoff=False, operations=[])
    transactions, receipts = {}, {}
    for index, name in enumerate(tool.OPS[:tool.PHASES[phase]]):
        target, token, sender, recipient = None, None, None, None
        if name == 'deploy-nft': data = inputs['contracts']['nft']['creation']
        elif name == 'deploy-wallet': data = inputs['contracts']['wallet']['creation'] + tool.account_word(A)
        elif name.startswith('mint-'):
            target, token, sender, recipient = N, 7 if name == 'mint-a' else 8, tool.ZERO, A if name == 'mint-a' else C
            data = tool.selector('mint(address)') + tool.account_word(recipient)
        else:
            token, sender, recipient = (7, A, B) if name == 'transfer-a' else (8, C, B)
            target = N if name == 'transfer-a' else C
            data = tool.transfer_data(sender, recipient, token)
            if name == 'transfer-c': data = tool.execute_data(N, data)
        tx_hash = '0x' + format(index + 1, '064x'); header = block(index + 10)
        intent = dict(chainId=hex(tool.network.CHAIN_ID), **{'from': A}, data='0x' + data.hex(), value='0x0', nonce=hex(index),
                      gas=hex(200000), maxFeePerGas=hex(10**10), maxPriorityFeePerGas=hex(10**9))
        if target: intent['to'] = target
        ledger['operations'].append(dict(id=name, operationID='unit-operation-' + str(index), status='confirmed', createdAt=1700000000000 + index,
            hash=tx_hash, transaction=intent, calldataSHA256=hashlib.sha256(data).hexdigest(), reservedFeeWei=str(200000 * 10**10)))
        transactions[tx_hash] = dict(intent, to=target, input=intent['data'], hash=tx_hash, blockNumber=header['number'], blockHash=header['hash'])
        logs = []
        if token:
            logs = [dict(address=N, topics=[tool.transfer_topic(), '0x' + (b'\x00' * 12 + bytes.fromhex(sender[2:])).hex(),
                '0x' + tool.account_word(recipient).hex(), '0x' + tool.word(token).hex()], data='0x', logIndex='0x0', removed=False,
                transactionHash=tx_hash, blockNumber=header['number'], blockHash=header['hash'])]
        receipts[tx_hash] = dict(transactionHash=tx_hash, status='0x1', blockNumber=header['number'], blockHash=header['hash'],
            to=target, contractAddress=N if name == 'deploy-nft' else C if name == 'deploy-wallet' else None,
            gasUsed=hex(100000), effectiveGasPrice=hex(2 * 10**9), logs=logs)
    inputs['ledger'] = ledger
    return inputs, transactions, receipts


class RPC:
    def __init__(self, inputs, transactions, receipts, phase, slot, fault=''):
        self.inputs, self.transactions, self.receipts, self.phase = inputs, copy.deepcopy(transactions), copy.deepcopy(receipts), phase
        self.host, self.slot, self.endpoint, self.events, self.calls = 'unit-' + str(slot) + '.example', slot, 'https://unit-' + str(slot) + '.example', [], []
        self.fault, self.receipt_reads, self.anchor_reads = fault, {}, 0

    def call(self, method, params):
        value = self._call(method, params)
        completed = datetime.fromtimestamp(1700000000, timezone.utc).isoformat() if self.fault == 'early-safe-clock' and method == 'eth_getBlockByNumber' and params == ['safe', False] else datetime.now(timezone.utc).isoformat()
        self.events.append(dict(id=len(self.events) + 1, method=method, params=params, result=value, at=completed, completedAt=completed))
        return value

    def _call(self, method, params):
        assert method in tool.deployment.DeploymentRPC.allowed_methods, 'oracle attempted a write'
        self.calls.append((method, params))
        if method == 'eth_chainId': return '0x1' if self.fault == 'chain' else hex(tool.network.CHAIN_ID)
        if method == 'eth_getBlockByNumber':
            if params[0] == '0x0': return dict(number='0x0', hash='0x' + '0' * 64 if self.fault == 'genesis' else tool.network.GENESIS)
            if params[0] in ('safe', 'finalized'): return block(1 if self.fault == 'pending-safe' else 100 if params[0] == 'safe' else 90)
            height = int(params[0], 16)
            if height == 100:
                self.anchor_reads += 1
                if self.fault == 'anchor-reorg' and self.anchor_reads >= 3: return dict(number=params[0], hash='0x' + 'f' * 64)
            return block(height)
        if method == 'eth_getTransactionByHash':
            tx = copy.deepcopy(self.transactions[params[0]])
            if self.fault == 'sender': tx['from'] = B
            if self.fault == 'input': tx['input'] += '00'
            if self.fault == 'nonce': tx['nonce'] = '0xff'
            if self.fault == 'fee': tx['maxFeePerGas'] = hex(10**10 + 1)
            return tx
        if method == 'eth_getTransactionReceipt':
            self.receipt_reads[params[0]] = self.receipt_reads.get(params[0], 0) + 1
            value = copy.deepcopy(self.receipts[params[0]])
            if self.fault == 'receipt-reorg' and self.receipt_reads[params[0]] > 1: value['blockHash'] = '0x' + 'f' * 64
            if self.fault == 'revert': value['status'] = '0x0'
            if self.fault == 'missing-log': value['logs'] = []
            if self.fault == 'vendor-fields': value['vendorOptional'] = None
            return value
        if method == 'eth_getCode':
            if params[0] == A: return '0x'
            return '0xdead' if self.fault == 'runtime' else '0x' + self.inputs['contracts']['nft' if params[0] == N else 'wallet']['runtime'].hex()
        if method == 'eth_call':
            tx = params[0]; data = bytes.fromhex(tx['data'][2:]); prefix = data[:4]
            if tx['to'] == C:
                return '0x' + ((b'\x11' * 12 if self.fault == 'dirty-address' else b'\x00' * 12) + bytes.fromhex((B if self.fault == 'owner-policy' else A)[2:])).hex()
            if prefix == tool.selector('balanceOf(address)'):
                address = '0x' + data[-20:].hex(); balances = {'eoa-held': (1, 0, 0), 'eoa-transferred': (0, 0, 1), 'contract-held': (0, 1, 1), 'contract-transferred': (0, 0, 2)}[self.phase]
                amount = dict(zip((A, C, B), balances))[address]
                if self.fault == 'holding' and address == A: amount += 1
                return '0x' + tool.word(amount).hex()
            token = int.from_bytes(data[4:], 'big')
            owner = A if token == 7 and self.phase == 'eoa-held' else C if token == 8 and self.phase == 'contract-held' else B
            return '0x' + tool.account_word(owner).hex()
        raise AssertionError(method)


class TransactionOracleContract(unittest.TestCase):
    def evaluate(self, phase='contract-transferred', fault=''):
        inputs, txs, receipts = fixture(phase)
        pair = [RPC(inputs, txs, receipts, phase, 0), RPC(inputs, txs, receipts, phase, 1, fault)]
        return tool.qualify_phase(inputs, phase, pair), pair

    def test_all_four_phases_independently_check_two_providers_and_exact_transaction_prefix(self):
        for phase, count in tool.PHASES.items():
            with self.subTest(phase=phase):
                report, pair = self.evaluate(phase)
                self.assertEqual(count, len(report['transactions'])); self.assertEqual(100, report['safeAnchor']['number'])
                self.assertEqual(A, report['walletOwner']); self.assertEqual(phase, report['phase'])
                self.assertTrue(all(rpc.receipt_reads[tx['hash']] == 2 for rpc in pair for tx in report['transactions']))

    def test_bad_network_transactions_logs_runtime_policy_holdings_and_reorgs_refuse(self):
        for fault in ('chain', 'genesis', 'sender', 'input', 'nonce', 'fee', 'revert', 'missing-log', 'runtime', 'owner-policy', 'holding', 'dirty-address', 'anchor-reorg', 'receipt-reorg'):
            with self.subTest(fault=fault), self.assertRaises(ValueError): self.evaluate(fault=fault)

    def test_optional_provider_fields_do_not_invent_reorgs(self):
        report, _ = self.evaluate(fault='vendor-fields')
        self.assertEqual('2', report['balances']['B'])

    def test_slow_post_safe_work_cannot_restart_the_revocation_clock(self):
        inputs, txs, receipts = fixture('contract-transferred')
        pair = [RPC(inputs, txs, receipts, 'contract-transferred', index, 'early-safe-clock') for index in range(2)]
        report = tool.qualify_phase(inputs, 'contract-transferred', pair)
        self.assertEqual(1700000000000, report['safeObservedAtMs'])

    def test_safe_wait_is_pending_without_rebroadcast_and_invalid_wait_is_rejected(self):
        with self.assertRaisesRegex(tool.PendingSafe, 'PENDING') as pending: self.evaluate(fault='pending-safe')
        evidence = pending.exception.evidence
        self.assertEqual('PENDING', evidence['state']); self.assertIs(evidence['mayRebroadcast'], False)
        self.assertEqual(6, len(evidence['transactions'])); self.assertEqual(2, len(evidence['providerSafeViews']))
        self.assertLess(min(item['number'] for item in evidence['providerSafeViews']), evidence['requiredBlock'])
        inputs, txs, receipts = fixture('eoa-held')
        for wait in (-1, 1801):
            with self.assertRaisesRegex(ValueError, '30 minutes'): tool.qualify_phase(inputs, 'eoa-held', [], wait)

    def test_pending_cli_preserves_candidate_ledger_and_original_rpc_observation(self):
        inputs, txs, receipts = fixture('eoa-held')
        pair = [RPC(inputs, txs, receipts, 'eoa-held', index, 'pending-safe') for index in range(2)]
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); output = root / 'pending.json'
            argv = ['oracle', '--candidate', SHA, '--controller', A, '--ledger', str(root / 'ledger.json'),
                '--manifest', str(root / 'manifest.json'), '--build-identity', str(root / 'build.json'),
                '--phase', 'eoa-held', '--output', str(output)]
            with mock.patch.object(sys, 'argv', argv), mock.patch.object(tool, 'read_inputs', return_value=inputs), \
                    mock.patch.object(tool.deployment, 'providers', side_effect=lambda rpcs=None: pair):
                self.assertEqual(2, tool.main())
            report = json.loads(output.read_text())
            self.assertEqual('BLOCKED', report['status']); self.assertEqual(SHA, report['candidateSHA'])
            self.assertEqual(inputs['ledgerSHA256'], report['ledgerSHA256'])
            self.assertEqual(inputs['manifestSHA256'], report['manifestSHA256']); self.assertEqual('eoa-held', report['phase'])
            self.assertIs(report['completeAcceptance'], False); self.assertIs(report['pending']['mayRebroadcast'], False)
            self.assertEqual(set(txs), {entry['hash'] for entry in report['pending']['transactions']})
            self.assertTrue(all(provider['events'] for provider in report['providers']))

    def test_unknown_missing_duplicate_foreign_and_fabricated_ledger_never_reaches_rpc(self):
        original, txs, receipts = fixture('contract-transferred')
        changes = [lambda x: x['ledger'].update(candidateSHA='b' * 40), lambda x: x['ledger'].update(publicWalletSignoff=True),
            lambda x: x['ledger']['operations'][0].update(status='unknown'), lambda x: x['ledger']['operations'][1].update(hash=x['ledger']['operations'][0]['hash']),
            lambda x: x['ledger']['operations'][1].update(operationID=x['ledger']['operations'][0]['operationID']),
            lambda x: x['ledger']['operations'].pop(2), lambda x: x['ledger'].update(recipient=C)]
        for mutate in changes:
            inputs = copy.deepcopy(original); mutate(inputs)
            pair = [RPC(inputs, txs, receipts, 'contract-transferred', index) for index in range(2)]
            with self.subTest(change=changes.index(mutate)), self.assertRaises(ValueError): tool.qualify_phase(inputs, 'contract-transferred', pair)
            self.assertTrue(all(not rpc.calls for rpc in pair))
        inputs = copy.deepcopy(original); inputs['ledger']['operations'] = inputs['ledger']['operations'][:3]
        with self.assertRaises(tool.deployment.BlockedError): tool.ledger_operations(inputs, 'contract-held')

    def test_intent_digest_fee_or_input_tamper_cannot_be_hidden_by_valid_receipts(self):
        for field, value in [('calldataSHA256', '0' * 64), ('reservedFeeWei', '1')]:
            inputs, txs, receipts = fixture('eoa-held'); inputs['ledger']['operations'][0][field] = value
            with self.subTest(field=field), self.assertRaises(ValueError): tool.read_operations(RPC(inputs, txs, receipts, 'eoa-held', 0), inputs, inputs['ledger']['operations'], B)

    def test_dynamic_execute_encoding_matches_pinned_foundry_abi_encoder(self):
        inner = tool.transfer_data(C, B, 8)
        result = subprocess.run(['cast', 'calldata', 'execute(address,uint256,bytes)', N, '0', '0x' + inner.hex()], capture_output=True, text=True, timeout=10)
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(result.stdout.strip().lower(), '0x' + tool.execute_data(N, inner).hex())

    def test_existing_report_is_never_overwritten_even_on_invalid_input(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / 'original.json'; output.write_text('original evidence')
            result = subprocess.run([sys.executable, str(Path(tool.__file__)), '--candidate', SHA, '--controller', A,
                '--ledger', directory + '/absent', '--manifest', directory + '/absent', '--build-identity', directory + '/absent',
                '--phase', 'eoa-held', '--output', str(output)], capture_output=True, text=True)
            self.assertNotEqual(0, result.returncode); self.assertEqual('original evidence', output.read_text())


if __name__ == '__main__':
    unittest.main()
