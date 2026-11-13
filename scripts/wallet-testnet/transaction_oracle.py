#!/usr/bin/env python3
"""Read-only, two-provider oracle for the six user-operated Sepolia fixture writes.

Ledger states and PASS flags are declarations, never chain evidence. Every
transaction, calldata, nonce, receipt and holding is independently read again.
This tool neither signs nor broadcasts and never claims complete acceptance.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import sys
import time
from concurrent.futures import ThreadPoolExecutor
from functools import lru_cache
import subprocess

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('wallet_deployment', Path(__file__).with_name('deployment_manifest.py'))
deployment = importlib.util.module_from_spec(spec)
spec.loader.exec_module(deployment)
network = deployment.network
OPS = ('deploy-nft', 'deploy-wallet', 'mint-a', 'transfer-a', 'mint-c', 'transfer-c')
PHASES = {'eoa-held': 3, 'eoa-transferred': 4, 'contract-held': 5, 'contract-transferred': 6}
ZERO = '0x' + '00' * 20


class PendingSafe(deployment.BlockedError):
    def __init__(self, transactions, safe, wanted):
        super().__init__('original transaction has not reached both providers safe view; keep PENDING without rebroadcast')
        self.evidence = dict(state='PENDING', requiredBlock=wanted,
            providerSafeViews=[dict(number=item['head']['number'], hash=item['head']['hash'],
                observedAtMs=item['observedAtMs']) for item in safe],
            transactions=transactions, mayRebroadcast=False)


def require(ok, reason):
    if not ok: raise deployment.EvidenceError(reason)


def word(value):
    require(type(value) is int and 0 <= value < 2**256, 'invalid ABI uint256')
    return value.to_bytes(32, 'big')


def account_word(value):
    return bytes.fromhex('00' * 12 + deployment.address(value)[2:])


@lru_cache(maxsize=16)
def selector(signature):
    return bytes.fromhex(deployment.keccak(signature.encode())[2:10])


@lru_cache(maxsize=1)
def transfer_topic():
    return deployment.keccak(b'Transfer(address,address,uint256)')


def transfer_data(sender, recipient, token):
    return selector('transferFrom(address,address,uint256)') + account_word(sender) + account_word(recipient) + word(token)


def execute_data(nft, data):
    return selector('execute(address,uint256,bytes)') + account_word(nft) + word(0) + word(96) + word(len(data)) + data + b'\x00' * (-len(data) % 32)


def read_inputs(args):
    candidate, controller = deployment.candidate_id(args.candidate), deployment.address(args.controller)
    manifest = json.loads(args.manifest.read_text())
    require(manifest.get('schemaVersion') == 2 and manifest.get('deploymentVerified') is True and
        manifest.get('candidateSHA') == candidate and manifest.get('controller') == controller and
        manifest.get('chainId') == network.CHAIN_ID and manifest.get('genesis') == network.GENESIS, 'foreign or unverified deployment manifest')
    deployment.validate_roles(manifest.get('contracts'), controller)
    contracts, entries = {}, []
    for entry in manifest['contracts']:
        path = (args.manifest.parent / entry['artifact']['artifactPath']).resolve()
        require(path.is_relative_to(args.manifest.parent.resolve()), 'contract artifact escaped its bundle')
        identity, creation, runtime = deployment.artifact_identity(path, entry['role'], candidate, args.source_root)
        expected = {key: value for key, value in entry['artifact'].items() if key != 'artifactPath'}
        require(identity == expected, 'deployment artifact identity changed')
        contracts[entry['role']] = dict(entry, creation=creation, runtime=runtime)
        entries.append({'role': entry['role'], 'artifact': identity})
    compilation = deployment.compiled_identity(args.build_identity, candidate, entries)
    require(compilation == manifest.get('compiledBuild'), 'deployment manifest differs from original CI build')
    ledger = json.loads(args.ledger.read_text())
    return dict(candidate=candidate, controller=controller, contracts=contracts, compilation=compilation,
        ledger=ledger, ledgerSHA256=hashlib.sha256(args.ledger.read_bytes()).hexdigest(),
        manifestSHA256=hashlib.sha256(args.manifest.read_bytes()).hexdigest())


def ledger_operations(inputs, phase):
    ledger, controller = inputs['ledger'], inputs['controller']
    require(ledger.get('schemaVersion') == 1 and ledger.get('candidateSHA') == inputs['candidate'] and
        deployment.address(ledger.get('controller')) == controller and ledger.get('chainId') == network.CHAIN_ID and
        ledger.get('completeAcceptance') is False and ledger.get('publicWalletSignoff') is False, 'foreign ledger or fabricated signoff')
    recipient = deployment.address(ledger.get('recipient'))
    require(recipient not in {controller, *(item['address'] for item in inputs['contracts'].values())}, 'recipient is not an independent B')
    rows = ledger.get('operations')
    require(isinstance(rows, list) and len(rows) <= 128 and all(isinstance(op, dict) for op in rows), 'malformed operation ledger')
    require(all(op.get('id') in OPS and op.get('status') in {'intent', 'unknown', 'submitted', 'confirmed', 'reverted', 'rejected', 'invalid'} for op in rows), 'unknown operation or status')
    require(len({op.get('operationID') for op in rows}) == len(rows) and all(isinstance(op.get('operationID'), str) and op['operationID'] for op in rows), 'missing or duplicated operation ID')
    if any(op['status'] in {'intent', 'unknown', 'submitted'} for op in rows):
        raise deployment.BlockedError('a wallet operation is unresolved; reconcile its original hash, never rebroadcast')
    active = [op for op in rows if op['status'] != 'rejected']
    require(all(not op.get('hash') for op in rows if op['status'] == 'rejected'), 'rejected operation has a broadcast hash')
    require(all(op['status'] == 'confirmed' for op in active), 'reverted or invalid fixture operation')
    require(len(active) <= 8 and [op['id'] for op in active] == list(OPS[:len(active)]), 'phase requires the exact ordered transaction prefix')
    if len(active) < PHASES[phase]: raise deployment.BlockedError('user fixture transactions for this phase are still missing')
    require(len(active) == PHASES[phase], 'ledger contains operations beyond the selected phase')
    hashes = [deployment.hash32(op.get('hash')) for op in active]
    require(len(set(hashes)) == len(hashes), 'a transaction hash was reused for multiple operations')
    require(all(type(op.get('createdAt')) is int and op['createdAt'] > 0 for op in rows), 'missing original operation timestamp')
    return active, recipient


def original_intent(op, expected_input, controller, target):
    intent = op.get('transaction')
    require(isinstance(intent, dict), 'original persisted transaction intent is absent')
    require(deployment.address(intent.get('from')) == controller and network.quantity(intent.get('chainId')) == network.CHAIN_ID and
        network.quantity(intent.get('value')) == 0 and (deployment.address(intent['to']) if intent.get('to') else None) == target and
        deployment.hex_bytes(intent.get('data')) == expected_input and
        op.get('calldataSHA256') == hashlib.sha256(expected_input).hexdigest(), 'persisted operation does not describe the expected fixture call')
    gas, fee = network.quantity(intent.get('gas')), network.quantity(intent.get('maxFeePerGas'))
    require(gas > 0 and fee > 0 and op.get('reservedFeeWei') == str(gas * fee), 'fee reservation differs from original gas ceiling')
    require(0 < network.quantity(intent.get('maxPriorityFeePerGas')) <= fee, 'invalid persisted priority fee')
    return intent, gas * fee


def transaction_facts(rpc, tx_hash, controller, target, data, intent, expected_chain=network.CHAIN_ID):
    """Chain-independent parser; the public entry always passes Sepolia's chain.

    Local deterministic tests can exercise this parser on real Anvil receipts
    without weakening the public network/genesis qualification.
    """
    tx = rpc.call('eth_getTransactionByHash', [tx_hash])
    receipt = rpc.call('eth_getTransactionReceipt', [tx_hash])
    if not tx or not receipt: raise deployment.BlockedError('original transaction is pending or unavailable')
    require(deployment.hash32(tx.get('hash')) == tx_hash and deployment.address(tx.get('from')) == controller and
        (deployment.address(tx['to']) if tx.get('to') else None) == target and
        network.quantity(tx.get('chainId')) == expected_chain and network.quantity(tx.get('value')) == 0 and
        deployment.hex_bytes(tx.get('input')) == data and network.quantity(tx.get('nonce')) == network.quantity(intent.get('nonce')),
        'chain transaction differs from the original operation')
    require(network.quantity(tx.get('gas')) <= network.quantity(intent.get('gas')) and
        network.quantity(tx.get('maxFeePerGas')) <= network.quantity(intent.get('maxFeePerGas')), 'wallet increased the saved fee ceiling')
    require(deployment.hash32(receipt.get('transactionHash')) == tx_hash and network.quantity(receipt.get('status')) == 1, 'transaction failed or receipt identity changed')
    block = network.header(rpc.call('eth_getBlockByNumber', [receipt.get('blockNumber'), False]))
    require(block['number'] == network.quantity(receipt.get('blockNumber')) == network.quantity(tx.get('blockNumber')) and
        block['hash'] == deployment.hash32(receipt.get('blockHash')) == deployment.hash32(tx.get('blockHash')), 'transaction receipt is not canonical')
    gas, price = network.quantity(receipt.get('gasUsed')), network.quantity(receipt.get('effectiveGasPrice'))
    require(0 < gas <= network.quantity(tx['gas']) and 0 < price <= network.quantity(tx['maxFeePerGas']), 'receipt fees exceed the actual transaction')
    return dict(hash=tx_hash, nonce=network.quantity(tx['nonce']), block=block, feeWei=str(gas * price), receipt=receipt_identity(receipt))


def receipt_identity(receipt):
    # Providers may include different optional fields. Compare all consensus
    # identities and logs, without treating an extra vendor field as a reorg.
    require(isinstance(receipt, dict), 'missing receipt')
    result = {key: deployment.hash32(receipt.get(key)) for key in ('transactionHash', 'blockHash')}
    result.update({key: hex(network.quantity(receipt.get(key))) for key in ('blockNumber', 'status', 'gasUsed', 'effectiveGasPrice')})
    result.update({key: deployment.address(receipt[key]) if receipt.get(key) else None for key in ('contractAddress', 'to')})
    logs = receipt.get('logs')
    require(isinstance(logs, list), 'missing receipt logs')
    normalized, indices = [], set()
    for log in logs:
        require(isinstance(log, dict) and isinstance(log.get('topics'), list), 'malformed receipt log')
        index = network.quantity(log.get('logIndex'))
        require(index not in indices and log.get('removed') is not True, 'duplicated or removed receipt log')
        indices.add(index)
        require(deployment.hash32(log.get('transactionHash')) == deployment.hash32(receipt['transactionHash']) and
            deployment.hash32(log.get('blockHash')) == deployment.hash32(receipt['blockHash']) and
            network.quantity(log.get('blockNumber')) == network.quantity(receipt['blockNumber']), 'log belongs to another transaction or block')
        data = log.get('data')
        require(isinstance(data, str) and re.fullmatch('0x(?:[0-9a-fA-F]{2})*', data), 'malformed receipt log data')
        normalized.append(dict(address=deployment.address(log.get('address')), topics=[deployment.hash32(topic) for topic in log['topics']],
            data=data.lower(), logIndex=hex(index), removed=False))
    result['logs'] = normalized
    return result


def nft_transfer(receipt, nft, sender, recipient, expected_token=None):
    topic = transfer_topic()
    matches = []
    for log in receipt.get('logs', []):
        if not isinstance(log, dict) or str(log.get('address', '')).lower() != nft: continue
        topics = log.get('topics', [])
        if len(topics) != 4 or topics[0].lower() != topic: continue
        if topics[1].lower() != '0x' + (b'\x00' * 12 + bytes.fromhex(sender[2:])).hex() or topics[2].lower() != '0x' + account_word(recipient).hex(): continue
        token = network.quantity(topics[3])
        require(log.get('removed') is not True and log.get('data') == '0x', 'removed or malformed ERC-721 transfer log')
        require(token > 0 and (expected_token is None or expected_token == token), 'wrong NFT token was transferred')
        matches.append(token)
    require(len(matches) == 1, 'required NFT mint/transfer event is missing or ambiguous')
    return matches[0]


def read_operations(rpc, inputs, operations, recipient):
    controller, contracts = inputs['controller'], inputs['contracts']
    nft, wallet = (contracts[role]['address'] for role in ('nft', 'wallet'))
    tokens, facts, reservation = {}, [], 0
    for op in operations:
        name = op['id']; target = None
        if name == 'deploy-nft': data = contracts['nft']['creation']
        elif name == 'deploy-wallet': data = contracts['wallet']['creation'] + account_word(controller)
        elif name.startswith('mint-'):
            target = nft; data = selector('mint(address)') + account_word(controller if name == 'mint-a' else wallet)
        else:
            owner = controller if name == 'transfer-a' else wallet
            token = tokens['mint-a' if name == 'transfer-a' else 'mint-c']
            data = transfer_data(owner, recipient, token)
            target = nft if name == 'transfer-a' else wallet
            if name == 'transfer-c': data = execute_data(nft, data)
        intent, reserved = original_intent(op, data, controller, target); reservation += reserved
        fact = transaction_facts(rpc, deployment.hash32(op['hash']), controller, target, data, intent)
        receipt = fact['receipt']
        if name.startswith('deploy-'):
            role = 'nft' if name == 'deploy-nft' else 'wallet'
            require(deployment.address(receipt.get('contractAddress')) == contracts[role]['address'] and
                fact['hash'] == contracts[role]['creationTxHash'] and receipt.get('to') is None, 'deployment operation does not create the recorded contract')
        else:
            require(receipt.get('contractAddress') is None and deployment.address(receipt.get('to')) == target, 'business receipt target mismatch')
            if name.startswith('mint-'):
                tokens[name] = nft_transfer(receipt, nft, ZERO, controller if name == 'mint-a' else wallet)
                if op.get('tokenId') is not None: require(str(tokens[name]) == op['tokenId'], 'ledger mint token differs from the real Transfer log')
            else: nft_transfer(receipt, nft, controller if name == 'transfer-a' else wallet, recipient, token)
        fact['operation'] = name; facts.append(fact)
    nonces = [fact['nonce'] for fact in facts]
    require(nonces == sorted(set(nonces)), 'nonce reused or operation ordering reversed')
    require(all(left['block']['number'] <= right['block']['number'] for left, right in zip(facts, facts[1:])), 'transaction block order reversed')
    require(sum(int(fact['feeWei']) for fact in facts) <= network.MAX_FEE_WEI and reservation * 6 <= network.MAX_FEE_WEI * 5,
        'cumulative fee or reservation with 20 percent margin exceeds 0.02 ETH')
    require(len(set(tokens.values())) == len(tokens), 'A and C must receive different NFT tokens')
    return dict(transactions=facts, tokens=tokens, reservedFeeWei=str(reservation), actualFeeWei=str(sum(int(fact['feeWei']) for fact in facts)))


def call_word(rpc, contract, data, height):
    value = rpc.call('eth_call', [{'to': contract, 'data': '0x' + data.hex(), 'gas': '0x7a120'}, hex(height)])
    require(isinstance(value, str) and re.fullmatch('0x[0-9a-fA-F]{64}', value), 'malformed ABI response')
    return bytes.fromhex(value[2:])


def call_address(rpc, contract, data, height):
    value = call_word(rpc, contract, data, height)
    require(value[:12] == b'\x00' * 12, 'nonzero ABI address padding')
    return deployment.address('0x' + value[-20:].hex())


def state_at(rpc, inputs, recipient, tokens, anchor):
    height, contracts, controller = anchor['number'], inputs['contracts'], inputs['controller']
    require(network.header(rpc.call('eth_getBlockByNumber', [hex(height), False]), height) == anchor, 'state anchor changed before reads')
    for contract in contracts.values():
        require(deployment.hex_bytes(rpc.call('eth_getCode', [contract['address'], hex(height)])) == contract['runtime'], 'runtime changed at the holding anchor')
    require(rpc.call('eth_getCode', [controller, hex(height)]) == '0x', 'controller is no longer an EOA')
    nft, wallet = (contracts[role]['address'] for role in ('nft', 'wallet'))
    owner = call_address(rpc, wallet, selector('owner()'), height)
    require(owner == controller, 'wallet owner changed; NFT transfer cannot stand in for policy revocation')
    balances = {role: str(int.from_bytes(call_word(rpc, nft, selector('balanceOf(address)') + account_word(account), height), 'big'))
        for role, account in [('A', controller), ('C', wallet), ('B', recipient)]}
    owners = {role: call_address(rpc, nft, selector('ownerOf(uint256)') + word(token), height) for role, token in tokens.items()}
    require(network.header(rpc.call('eth_getBlockByNumber', [hex(height), False]), height) == anchor, 'state anchor changed during reads')
    return dict(balances=balances, owners=owners, walletOwner=owner)


def safe_observed_at(rpc, start):
    events = [event for event in rpc.events[start:] if event.get('method') == 'eth_getBlockByNumber' and event.get('params') == ['safe', False]]
    require(len(events) == 1 and isinstance(events[0].get('completedAt'), str), 'missing original safe response timestamp')
    observed = datetime.fromisoformat(events[0]['completedAt'])
    require(observed.tzinfo is not None, 'safe response timestamp must include timezone')
    return int(observed.timestamp() * 1000)


def initial_head(rpc):
    start = len(rpc.events); head = network.network_head(rpc)
    return dict(head=head, observedAtMs=safe_observed_at(rpc, start))


def next_safe(rpc):
    start = len(rpc.events)
    head = network.header(rpc.call('eth_getBlockByNumber', ['safe', False]))
    return dict(head=head, observedAtMs=safe_observed_at(rpc, start))


def qualify_phase(inputs, phase, rpcs, wait_seconds=0, sleeper=time.sleep):
    require(phase in PHASES, 'unknown fixture phase')
    require(type(wait_seconds) is int and 0 <= wait_seconds <= 1800, 'safe wait must be bounded by 30 minutes')
    operations, recipient = ledger_operations(inputs, phase)
    rpcs = deployment.providers(rpcs)
    # Validate network/genesis before accepting any transaction/holding evidence.
    with ThreadPoolExecutor(max_workers=2) as pool:
        initial = list(pool.map(initial_head, rpcs))
        results = list(pool.map(lambda rpc: read_operations(rpc, inputs, operations, recipient), rpcs))
    require(results[0] == results[1], 'providers disagree on original transactions or receipts')
    facts = results[0]; wanted = max(fact['block']['number'] for fact in facts['transactions'])
    deadline = time.monotonic() + wait_seconds
    safe = [dict(head=item['head']['safe'], observedAtMs=item['observedAtMs']) for item in initial]
    first_included = [item['observedAtMs'] if item['head']['number'] >= wanted else None for item in safe]
    while True:
        height = min(item['head']['number'] for item in safe)
        if height >= wanted: break
        if time.monotonic() >= deadline: raise PendingSafe(facts['transactions'], safe, wanted)
        # A ten-second cadence can miss the required live-media baseline before
        # safe inclusion. Two seconds keeps this reader at 0.5 req/s/provider.
        sleeper(min(2, max(0, deadline - time.monotonic())))
        with ThreadPoolExecutor(max_workers=2) as pool: current = list(pool.map(next_safe, rpcs))
        for index, item in enumerate(current):
            previous = safe[index]['head']; head = item['head']
            require(head['number'] >= previous['number'] and (head['number'] != previous['number'] or head['hash'] == previous['hash']), 'safe view regressed or reorged while waiting')
            if first_included[index] is None and head['number'] >= wanted: first_included[index] = item['observedAtMs']
        safe = current
    # Never restart the revocation clock after the potentially slow calldata,
    # receipt or balance checks: use the FIRST safe response from each provider
    # which actually included the relevant original transaction.
    observed_at = max(first_included)
    with ThreadPoolExecutor(max_workers=2) as pool:
        anchors = list(pool.map(lambda rpc: network.header(rpc.call('eth_getBlockByNumber', [hex(height), False]), height), rpcs))
    require(anchors[0] == anchors[1], 'providers disagree at the common safe height')
    with ThreadPoolExecutor(max_workers=2) as pool:
        states = list(pool.map(lambda rpc: state_at(rpc, inputs, recipient, facts['tokens'], anchors[0]), rpcs))
    require(states[0] == states[1], 'providers disagree on independently pinned NFT ownership')
    state = states[0]
    expected_balances = {'eoa-held': ('1', '0', '0'), 'eoa-transferred': ('0', '0', '1'),
                         'contract-held': ('0', '1', '1'), 'contract-transferred': ('0', '0', '2')}[phase]
    require(tuple(state['balances'][role] for role in ('A', 'C', 'B')) == expected_balances, 'safe balances do not match this phase')
    require(state['owners']['mint-a'] == (inputs['controller'] if phase == 'eoa-held' else recipient), 'A token owner does not match this phase')
    if 'mint-c' in facts['tokens']:
        require(state['owners']['mint-c'] == (inputs['contracts']['wallet']['address'] if phase == 'contract-held' else recipient), 'C token owner does not match this phase')
    # Re-read every receipt after state queries: a coherent historical holding
    # cannot rescue a transaction that reorged out while the oracle was running.
    for rpc in rpcs:
        for fact in facts['transactions']:
            receipt = rpc.call('eth_getTransactionReceipt', [fact['hash']])
            require(receipt_identity(receipt) == fact['receipt'], 'original receipt changed during oracle observation')
        require(network.header(rpc.call('eth_getBlockByNumber', [hex(height), False]), height) == anchors[0], 'safe anchor changed after receipt checks')
    return dict(candidateSHA=inputs['candidate'], controller=inputs['controller'], recipient=recipient, chainId=network.CHAIN_ID,
        genesis=network.GENESIS, phase=phase, safeAnchor=anchors[0], safeObservedAtMs=observed_at,
        ledgerSHA256=inputs['ledgerSHA256'], lastOperationCreatedAtMs=operations[-1]['createdAt'], deploymentManifestSHA256=inputs['manifestSHA256'], compiledBuild=inputs['compilation'],
        nft=inputs['contracts']['nft']['address'], wallet=inputs['contracts']['wallet']['address'], **facts, **state)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate', required=True); parser.add_argument('--controller', required=True)
    parser.add_argument('--ledger', type=Path, required=True); parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--build-identity', type=Path, required=True); parser.add_argument('--source-root', type=Path, default=deployment.REPO_ROOT)
    parser.add_argument('--phase', choices=PHASES, required=True); parser.add_argument('--wait-safe-seconds', type=int, default=0)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args(); rpcs = []
    report = dict(schemaVersion=1, scope='SEPOLIA_FIXTURE_PHASE_READ_ONLY', status='BLOCKED', completeAcceptance=False, publicWalletSignoff=False,
        startedAt=datetime.now(timezone.utc).isoformat(), toolSHA256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest())
    try:
        require(not args.output.exists() and args.output.resolve() not in {args.ledger.resolve(), args.manifest.resolve(), args.build_identity.resolve()}, 'output must be new and cannot replace original evidence')
        inputs = read_inputs(args)
        report.update(candidateSHA=inputs['candidate'], controller=inputs['controller'], phase=args.phase,
            chainId=network.CHAIN_ID, genesis=network.GENESIS, ledgerSHA256=inputs['ledgerSHA256'],
            manifestSHA256=inputs['manifestSHA256'], compiledBuild=inputs['compilation'])
        rpcs = deployment.providers()
        report.update(qualify_phase(inputs, args.phase, rpcs, args.wait_safe_seconds)); report['status'] = 'PHASE_CHAIN_VERIFIED'; code = 0
    except PendingSafe as error: report.update(status='BLOCKED', reason=str(error), pending=error.evidence); code = 2
    except (deployment.EvidenceError, KeyError, TypeError) as error: report.update(status='FAIL', reason=str(error)); code = 1
    except (ValueError, OSError, subprocess.SubprocessError) as error: report.update(status='BLOCKED', reason=str(error)); code = 2
    report['providers'] = deployment.record_provider_evidence(rpcs)
    report['finishedAt'] = datetime.now(timezone.utc).isoformat()
    # Preserve a prior success/failure verbatim; a repeated command gets a
    # nonzero result and never silently overwrites the first observation.
    if not args.output.exists() and args.output.resolve() not in {args.ledger.resolve(), args.manifest.resolve(), args.build_identity.resolve()}:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        try:
            with args.output.open('x') as stream: stream.write(json.dumps(report, indent=2) + '\n')
        except FileExistsError: code = 1; report['status'] = 'FAIL'
    print(report['status'] + ': read-only fixture phase; no signatures, broadcasts or full acceptance')
    return code


if __name__ == '__main__':
    raise SystemExit(main())
