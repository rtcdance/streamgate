"""Recompute phase evidence from original RPC responses, then recheck the chain.

Recorded timestamps are preserved; current finality never restarts a historical
revocation clock. Replay does not send requests or accept report PASS flags.
"""
from datetime import datetime
import hashlib
import importlib.util
from pathlib import Path
import re

spec = importlib.util.spec_from_file_location('phase_transaction_oracle', Path(__file__).with_name('transaction_oracle.py'))
oracle = importlib.util.module_from_spec(spec); spec.loader.exec_module(oracle)


def require(condition, reason):
    if not condition:
        raise ValueError(reason)


def timestamp(value):
    parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
    require(parsed.tzinfo is not None, 'RPC evidence timestamp must have timezone')
    return parsed.timestamp()


class OriginalResponses:
    def __init__(self, record, endpoint, started, finished):
        self.endpoint, self.slot = endpoint.endpoint, endpoint.slot
        self.host = endpoint.host
        require(record.get('slot') == self.slot and record.get('endpointSHA256') == hashlib.sha256(self.endpoint.encode()).hexdigest(),
                'original RPC evidence is from a different provider')
        self.original, self.events, self.cursor = record.get('events'), [], 0
        require(isinstance(self.original, list) and 0 < len(self.original) <= 10000, 'original RPC responses missing or over budget')
        previous = started
        for index, event in enumerate(self.original, 1):
            require(isinstance(event, dict) and event.get('id') == index and event.get('method') in oracle.deployment.DeploymentRPC.allowed_methods and
                    isinstance(event.get('params'), list) and 'result' in event and not any(key in event for key in ('error', 'rpcErrorCode', 'transportError', 'httpStatus')),
                    'original RPC stream has invalid, failed or ambiguous responses')
            at, completed = timestamp(event['at']), timestamp(event['completedAt'])
            require(started <= at <= completed <= finished and previous <= at, 'RPC response time is outside the original observation')
            previous = completed

    def call(self, method, params):
        require(self.cursor < len(self.original), 'original RPC response stream is truncated')
        event = self.original[self.cursor]
        require(event['method'] == method and event['params'] == params, 'original RPC sequence differs from the required phase')
        self.cursor += 1; self.events.append(event)
        return event['result']

    def complete(self):
        require(self.cursor == len(self.original), 'original RPC stream contains unverified extra responses')


def replay(report, inputs, phase, endpoints, original_exit, tool_sha256, pending=False):
    require(type(original_exit) is int and original_exit == (2 if pending else 0), 'wrong original phase process exit')
    require(report.get('schemaVersion') == 1 and report.get('scope') == 'SEPOLIA_FIXTURE_PHASE_READ_ONLY' and
            report.get('completeAcceptance') is False and report.get('publicWalletSignoff') is False and
            report.get('toolSHA256') == tool_sha256 and re.fullmatch('[0-9a-f]{64}', tool_sha256), 'foreign oracle tool or fabricated acceptance')
    started, finished = timestamp(report['startedAt']), timestamp(report['finishedAt'])
    require(started <= finished and finished - started <= 2400, 'invalid or unbounded original phase duration')
    records = report.get('providers')
    require(isinstance(records, list) and len(records) == 2, 'two original RPC response streams required')
    pair = [OriginalResponses(record, endpoint, started, finished) for record, endpoint in zip(records, endpoints)]
    if pending:
        require(report.get('status') == 'BLOCKED' and report.get('candidateSHA') == inputs['candidate'] and
                report.get('controller') == inputs['controller'] and report.get('phase') == phase and
                report.get('ledgerSHA256') == inputs['ledgerSHA256'] and report.get('manifestSHA256') == inputs['manifestSHA256'] and
                report.get('compiledBuild') == inputs['compilation'] and report.get('chainId') == oracle.network.CHAIN_ID and
                report.get('genesis') == oracle.network.GENESIS, 'pending phase lost its candidate/input identity')
        try:
            oracle.qualify_phase(inputs, phase, pair)
        except oracle.PendingSafe as error:
            require(report.get('pending') == error.evidence, 'pending report differs from original transaction/finality observations')
        else:
            raise ValueError('claimed PENDING phase was already safe')
        result = report['pending']
    else:
        require(report.get('status') == 'PHASE_CHAIN_VERIFIED', 'phase was not verified')
        result = oracle.qualify_phase(inputs, phase, pair, 1800, sleeper=lambda _: None)
        require(all(report.get(key) == value for key, value in result.items()), 'phase report differs from recomputed original chain evidence')
    for rpc in pair:
        rpc.complete()
    return result


def recheck_history(phases, endpoints):
    """Verify original hashes and holdings at their original safe blocks today."""
    require(set(phases) == set(oracle.PHASES), 'all four original asset phases required')
    final_inputs, final_report = phases['contract-transferred']
    final_facts = oracle.qualify_phase(final_inputs, 'contract-transferred', endpoints)
    for key in ('transactions', 'tokens', 'balances', 'owners', 'walletOwner', 'recipient', 'nft', 'wallet'):
        require(final_report.get(key) == final_facts[key], 'final canonical state changed since the original observation')
    previous_block, previous_time, prefix = -1, -1, []
    for phase in oracle.PHASES:
        inputs, report = phases[phase]
        require(inputs['candidate'] == final_inputs['candidate'] and inputs['controller'] == final_inputs['controller'] and
                inputs['contracts'] == final_inputs['contracts'] and inputs['compilation'] == final_inputs['compilation'], 'historical phases use different deployments/candidates')
        operations, recipient = oracle.ledger_operations(inputs, phase)
        require(operations[:len(prefix)] == prefix, 'phase ledger rewrote an earlier original operation')
        prefix = operations
        anchor = report['safeAnchor']; observed = report['safeObservedAtMs']
        require(anchor['number'] >= previous_block and observed >= previous_time, 'historical phase order moved backwards')
        previous_block, previous_time = anchor['number'], observed
        for rpc in endpoints:
            actual = oracle.state_at(rpc, inputs, recipient, report['tokens'], anchor)
            require(all(report.get(key) == value for key, value in actual.items()), 'historical safe holding changed or was fabricated')
    return final_facts
