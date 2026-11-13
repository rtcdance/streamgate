"""Synthetic RPC controls, never public-network acceptance."""
import copy
from datetime import datetime, timezone
import hashlib
import importlib.util
from pathlib import Path
import unittest

from test_wallet_transaction_oracle import RPC, fixture, block

spec = importlib.util.spec_from_file_location('wallet_phase_evidence', Path(__file__).resolve().parents[1] / 'wallet-testnet/phase_evidence.py')
gate = importlib.util.module_from_spec(spec); spec.loader.exec_module(gate)
oracle = gate.oracle
TOOL = 'f' * 64


class HistoricalRPC(RPC):
    def _call(self, method, params):
        if method == 'eth_getBlockByNumber' and params[0] in ('safe', 'finalized'):
            return block(100 + list(oracle.PHASES).index(self.phase) * 10)
        if method == 'eth_call' and int(params[1], 16) in (100, 110, 120, 130):
            previous = self.phase
            self.phase = list(oracle.PHASES)[(int(params[1], 16) - 100) // 10]
            try: return super()._call(method, params)
            finally: self.phase = previous
        return super()._call(method, params)


def original(phase='contract-transferred', pending=False):
    inputs, txs, receipts = fixture(phase)
    pair = [(RPC if pending else HistoricalRPC)(inputs, txs, receipts, phase, i, 'pending-safe' if pending else '') for i in range(2)]
    report = dict(schemaVersion=1, scope='SEPOLIA_FIXTURE_PHASE_READ_ONLY', completeAcceptance=False, publicWalletSignoff=False,
        toolSHA256=TOOL, startedAt=datetime.now(timezone.utc).isoformat())
    if pending:
        try: oracle.qualify_phase(inputs, phase, pair)
        except oracle.PendingSafe as error:
            report.update(status='BLOCKED', candidateSHA=inputs['candidate'], controller=inputs['controller'], phase=phase,
                chainId=oracle.network.CHAIN_ID, genesis=oracle.network.GENESIS, ledgerSHA256=inputs['ledgerSHA256'],
                manifestSHA256=inputs['manifestSHA256'], compiledBuild=inputs['compilation'], pending=error.evidence)
    else:
        report.update(oracle.qualify_phase(inputs, phase, pair), status='PHASE_CHAIN_VERIFIED')
    report.update(providers=oracle.deployment.record_provider_evidence(pair), finishedAt=datetime.now(timezone.utc).isoformat())
    return inputs, report, pair


class OriginalPhaseEvidenceTests(unittest.TestCase):
    def test_four_complete_original_streams_recompute_the_same_safe_observation(self):
        for phase in oracle.PHASES:
            inputs, report, pair = original(phase)
            result = gate.replay(report, inputs, phase, pair, 0, TOOL)
            self.assertEqual(report['safeObservedAtMs'], result['safeObservedAtMs'])

    def test_changed_report_raw_bytes_sequence_provider_or_exit_never_pass(self):
        inputs, report, pair = original()
        changes = [lambda r: r.update(candidateSHA='0' * 40), lambda r: r.update(safeObservedAtMs=r['safeObservedAtMs'] + 1),
            lambda r: r['providers'][0].update(endpointSHA256='0' * 64), lambda r: r['providers'][0]['events'].pop(),
            lambda r: r['providers'][0]['events'][0].update(result='0x1'),
            lambda r: r['providers'][1]['events'][0].update(params=['unexpected']),
            lambda r: r['providers'][1]['events'][0].update(transportError=True),
            lambda r: r['providers'][0]['events'][0].update(completedAt='2099-01-01T00:00:00Z'),
            lambda r: r['providers'][0]['events'].append(copy.deepcopy(r['providers'][0]['events'][-1])),
            lambda r: r.update(completeAcceptance=True)]
        for change in changes:
            edited = copy.deepcopy(report); change(edited)
            with self.subTest(change=change), self.assertRaises(ValueError): gate.replay(edited, inputs, 'contract-transferred', pair, 0, TOOL)
        for exit_code, tool in [(1, TOOL), (0, '0' * 64)]:
            with self.assertRaises(ValueError): gate.replay(report, inputs, 'contract-transferred', pair, exit_code, tool)

    def test_pending_requires_real_not_yet_safe_receipts_and_retains_original_hashes(self):
        inputs, report, pair = original('eoa-held', pending=True)
        result = gate.replay(report, inputs, 'eoa-held', pair, 2, TOOL, pending=True)
        self.assertFalse(result['mayRebroadcast'])
        for change in [lambda r: r['pending'].update(mayRebroadcast=True), lambda r: r.update(ledgerSHA256='0' * 64),
                       lambda r: r['pending']['transactions'].pop()]:
            edited = copy.deepcopy(report); change(edited)
            with self.assertRaises(ValueError): gate.replay(edited, inputs, 'eoa-held', pair, 2, TOOL, pending=True)

    def test_history_is_rechecked_at_original_blocks_without_restarting_its_clock(self):
        phases = {phase: original(phase)[:2] for phase in oracle.PHASES}
        inputs, _, pair = original()
        before = [r['safeObservedAtMs'] for _, r in phases.values()]
        facts = gate.recheck_history(phases, pair)
        self.assertEqual('2', facts['balances']['B'])
        self.assertEqual(before, [r['safeObservedAtMs'] for _, r in phases.values()])
        edited = copy.deepcopy(phases); edited['eoa-held'][1]['balances']['A'] = '2'
        with self.assertRaises(ValueError): gate.recheck_history(edited, pair)


if __name__ == '__main__':
    unittest.main()
