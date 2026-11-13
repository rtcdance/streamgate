#!/usr/bin/env python3
"""Fail-closed component gate for the candidate's local contract-wallet browser run.

This checks the browser component only, never all CW cases or public-wallet control.
The controller supplies the expected candidate/mode and the original process exit.
"""
import argparse
import json
import math
from pathlib import Path
import re

REQUIRED = frozenset({
    'CW01_H5_SIGNER_AND_SUBJECT', 'CW09_INITIAL_LEASE', 'CW01_PROFILE_SUBJECT',
    'CW01_INDEPENDENT_SAFE_OWNERSHIP', 'CW01_PERSISTED_CONTENT_OWNER',
    'CW01_REAL_WORKER_MEDIA', 'CW01_CONTROLLER_CANNOT_BORROW_NFT',
    'CW09_CONTINUOUS_PLAYBACK_ACROSS_LEASES', 'CW09_MEDIA_REQUESTS_USE_NEW_CAPABILITY',
    'CW09_OLD_ACCESS_REJECTED', 'CW11_CHAIN_SWITCH_CLEARS_STATE',
    'CW06_CANCEL_HAS_NO_HALF_LOGIN', 'CW_H5_NO_PAGE_ERRORS',
    'CW01_REAL_UPLOAD', 'CW01_CONTENT_CREATED', 'CW01_SERVER_POLICY_CREATED',
    'CW10_LIVE_POLICY_BASELINE', 'CW10_SAFE_POLICY_ONLY_CHANGE',
    'CW10_POLICY_REVOKES_WITH_UNCHANGED_NFT',
    'CW14_GRPC_LOGIN_SUBJECT', 'CW14_GRPC_REAL_MEDIA', 'CW14_GRPC_REFRESH_SUBJECT',
    'CW10_GRPC_POLICY_REVOKES_WITH_UNCHANGED_NFT',
    'CW16_NFT_TRANSFER_SAFE', 'CW14_NFT_MANIFEST_DENIED_HTTP_GRPC',
    'CW14_NFT_CAPABILITIES_REVOKED_WITH_LIVE_IDENTITY',
})


def require(condition, reason):
    if not condition:
        raise ValueError(reason)


def number(value):
    require(type(value) in (int, float) and math.isfinite(value), 'missing or non-finite measurement')
    return value


def digest(value):
    require(isinstance(value, str) and re.fullmatch('[0-9a-f]{64}', value), 'invalid digest')
    return value


def address(value):
    require(isinstance(value, str) and re.fullmatch('0x[0-9a-fA-F]{40}', value) and int(value, 16), 'invalid wallet address')
    return value.lower()


def validate(report, fixture, content, candidate, mode, exit_code):
    require(re.fullmatch('[0-9a-f]{40}', candidate or ''), 'expected frozen candidate is required')
    for entry in (report, fixture, content):
        require(entry.get('candidateSHA') == candidate and entry.get('evidenceContext') == 'CANDIDATE', 'foreign or development evidence')
    validate_component(report, fixture, content, mode, exit_code)


def validate_component(report, fixture, content, mode, exit_code):
    """Semantic checks only; a caller MUST NOT promote this to candidate acceptance.

    The CLI calls validate(), which independently requires the frozen candidate.
    Development diagnostics can reuse these checks without fabricating a SHA.
    """
    require(exit_code == 0, 'original browser process failed')
    require(mode in ('monolith', 'api-gateway'), 'unknown deployment mode')
    require(report.get('mode') == mode == content.get('mode'), 'foreign deployment mode')
    require(report.get('component') == 'LOCAL_WALLET_BROWSER' and
            report.get('testAdapter') == 'EIP1193_LOCAL_ANVIL_REAL_SIGNATURE' and
            report.get('publicWalletSignoff') is False, 'incorrect evidence boundary')
    require(report.get('passed') is True and report.get('errors') == [], 'browser did not pass')
    require(fixture.get('network') == 'ANVIL_LOCAL_ONLY' and fixture.get('chainId') == 31337, 'foreign fixture network')
    controller, subject, nft = (address(fixture.get(k)) for k in ('controller', 'contractWallet', 'nft'))
    require(len({controller, subject, nft}) == 3, 'fixture identities must be distinct')
    require(address(content.get('subject')) == subject and content.get('freshUpload') is True and
            isinstance(content.get('contentId'), str) and content['contentId'], 'fresh C-owned upload is required')
    checks = report.get('checks')
    require(isinstance(checks, list) and all(isinstance(c, dict) for c in checks), 'missing browser checks')
    ids = [c.get('id') for c in checks]
    require(all(isinstance(i, str) for i in ids) and len(set(ids)) == len(ids), 'duplicate or invalid check IDs')
    require(REQUIRED <= set(ids) and all(c.get('ok') is True for c in checks), 'missing or failed required browser case')
    required = report.get('required')
    require(isinstance(required, list) and len(required) == len(REQUIRED) and set(required) == REQUIRED, 'reported required list differs from the component contract')
    by_id = {c['id']: c.get('detail', {}) for c in checks}
    identity = by_id['CW01_H5_SIGNER_AND_SUBJECT']
    require(address(identity.get('signer')) == controller and address(identity.get('subject')) == subject, 'controller/subject mismatch')
    holding = by_id['CW01_INDEPENDENT_SAFE_OWNERSHIP']
    require(holding.get('A') == '0' and holding.get('C') == '1' and
            re.fullmatch('0x[0-9a-fA-F]+', holding.get('block', '')) and
            re.fullmatch('0x[0-9a-fA-F]{64}', holding.get('blockHash', '')), 'missing independent safe holdings')
    baseline = by_id['CW10_LIVE_POLICY_BASELINE']
    for key in ('tokenHash', 'capabilityHash'):
        digest(baseline.get(key))
    access_exp, cap_exp = (number(baseline.get(k)) for k in ('accessExp', 'capabilityExp'))
    require(min(access_exp, cap_exp) * 1000 - number(baseline.get('observedAt')) >= 20000, 'revocation started with expiring credentials')
    policy = by_id['CW10_SAFE_POLICY_ONLY_CHANGE']
    require(address(policy.get('ownerBefore')) == controller and address(policy.get('ownerAfter')) != controller and
            policy.get('A') == '0' and policy.get('C') == '1' and
            re.fullmatch('0x[0-9a-fA-F]{64}', policy.get('transactionHash', '')) and
            re.fullmatch('0x[0-9a-fA-F]+', policy.get('block', '')) and
            re.fullmatch('0x[0-9a-fA-F]{64}', policy.get('blockHash', '')), 'missing safe policy-only change')
    revoked = by_id['CW10_POLICY_REVOKES_WITH_UNCHANGED_NFT']
    confirmed, rejected, elapsed = (number(revoked.get(k)) for k in ('safeObservedAt', 'rejectedAt', 'elapsedMs'))
    require(revoked.get('accessExp') == access_exp and revoked.get('capabilityExp') == cap_exp and
            baseline['observedAt'] <= confirmed <= rejected and 0 <= elapsed == rejected - confirmed <= 70000 and
            rejected < (min(access_exp, cap_exp) - 5) * 1000 and revoked.get('oldAccessStatus') == 401 and
            revoked.get('oldSignerLoginStatus') == 401 and revoked.get('A') == '0' and revoked.get('C') == '1' and
            address(revoked.get('nft')) == nft and revoked.get('tokenId') == fixture.get('tokenId'),
            'policy revocation was absent, late, expired or replaced by an NFT transfer')
    signatures = report.get('signedMessages')
    require(isinstance(signatures, list) and signatures, 'missing real local signatures')
    for signature in signatures:
        require(address(signature.get('signer')) == controller, 'foreign local signer')
        require(re.fullmatch('0x[0-9a-fA-F]{64}', signature.get('digest', '')), 'missing signed message digest')
    samples = report.get('samples')
    require(isinstance(samples, list) and len(samples) >= 7, 'insufficient playback samples')
    last_at = -1
    absolute = None
    tokens = set()
    for sample in samples:
        at = number(sample.get('at'))
        require(at > last_at, 'unordered playback samples')
        last_at = at
        iat, exp, end = (number(sample.get(k)) for k in ('iat', 'exp', 'absolute'))
        require(0 < exp - iat <= 60 and exp <= end <= iat + 7200, 'lease or absolute lifetime exceeds policy')
        require(iat - 5 <= at / 1000 <= exp + 5, 'sample uses an expired or future credential')
        require(address(sample.get('subject')) == subject and sample.get('kind') == 'erc1271' and sample.get('chainId') == 31337, 'sample identity changed')
        require(absolute is None or end == absolute, 'absolute session deadline moved')
        absolute = end
        number(sample.get('currentTime'))
        tokens.add(digest(sample.get('tokenHash')))
    require(samples[-1]['at'] - samples[0]['at'] >= 70000 and
            samples[-1]['currentTime'] - samples[0]['currentTime'] >= 65 and len(tokens) >= 3, 'continuous playback did not cross two renewals')
    detail = by_id['CW09_CONTINUOUS_PLAYBACK_ACROSS_LEASES']
    require(number(detail.get('elapsedSeconds')) >= 70 and number(detail.get('mediaAdvance')) >= 65 and
            number(detail.get('accessTokensObserved')) >= 3, 'continuity assertion lacks its measurements')
    responses = report.get('mediaResponses')
    require(isinstance(responses, list) and responses, 'missing real media responses')
    media_tokens = set()
    for response in responses:
        claims = response.get('claims', {})
        require(address(claims.get('wallet_address')) == subject and claims.get('account_kind') == 'erc1271' and
                claims.get('chain_id') == 31337 and claims.get('content_id') == content['contentId'] and
                address(claims.get('contract')) == nft, 'foreign media capability')
        iat, exp, end = (number(claims.get(k)) for k in ('iat', 'exp', 'wallet_session_exp'))
        phase = response.get('phase', 'lease')
        expected_absolute = absolute if phase == 'lease' else by_id['CW16_NFT_TRANSFER_SAFE'].get('httpAbsolute')
        require(phase in ('lease', 'nft-transfer') and 0 < exp - iat <= 60 and exp <= end == expected_absolute and claims.get('token_type') == 'playback', 'unbounded or wrong-kind capability')
        if response.get('status') == 200 and phase == 'lease':
            media_tokens.add(digest(response.get('tokenHash')))
    require(len(media_tokens) >= 2, 'media never used a renewed capability')
    grpc_login = by_id['CW14_GRPC_LOGIN_SUBJECT']
    digest(report.get('grpcBridgeSHA256'))
    require(address(grpc_login.get('subject')) == subject and grpc_login.get('chainId') == 31337 and grpc_login.get('kind') == 'erc1271', 'gRPC did not admit C')
    grpc_samples = report.get('grpcSamples')
    require(isinstance(grpc_samples, list) and len(grpc_samples) >= 3, 'missing gRPC renewals')
    admissions, grpc_tokens = set(), set()
    for sample in grpc_samples:
        iat, exp, end = (number(sample.get(k)) for k in ('iat', 'exp', 'absolute'))
        require(address(sample.get('subject')) == subject and sample.get('kind') == 'erc1271' and sample.get('chainId') == 31337 and
                0 < exp - iat <= 60 and exp <= end <= iat + 7200, 'foreign or unbounded gRPC identity')
        admissions.add((digest(sample.get('sessionHash')), digest(sample.get('proofHash')), end))
        grpc_tokens.add(digest(sample.get('tokenHash')))
    require(len(admissions) == 1 and len(grpc_tokens) >= 3, 'gRPC refresh changed its admission or did not rotate tokens')
    grpc_media, grpc_refresh = (by_id[k] for k in ('CW14_GRPC_REAL_MEDIA', 'CW14_GRPC_REFRESH_SUBJECT'))
    digest(grpc_media.get('sha256')); digest(grpc_refresh.get('capabilityHash'))
    require(number(grpc_media.get('size')) > 0 and grpc_media.get('sha256') == grpc_refresh.get('sha256'), 'gRPC media was absent or changed')
    grpc_revoked = by_id['CW10_GRPC_POLICY_REVOKES_WITH_UNCHANGED_NFT']
    access_exp, cap_exp = (number(grpc_refresh.get(k)) for k in ('accessExp', 'capabilityExp'))
    confirmed, rejected, elapsed = (number(grpc_revoked.get(k)) for k in ('safeObservedAt', 'rejectedAt', 'elapsedMs'))
    require(cap_exp <= access_exp and number(grpc_refresh.get('observedAt')) <= confirmed <= rejected and
            0 <= elapsed == rejected - confirmed <= 70000 and rejected < (min(access_exp, cap_exp) - 5) * 1000 and
            grpc_revoked.get('accessExp') == access_exp and grpc_revoked.get('capabilityExp') == cap_exp and
            grpc_revoked.get('refreshCode') == 'Unauthenticated' and grpc_revoked.get('verifyCode') in ('OK', 'Unauthenticated') and grpc_revoked.get('verifyValid') is False and
            grpc_revoked.get('manifestCode') == 'Unauthenticated' and grpc_revoked.get('segmentCode') == 'Unauthenticated' and
            grpc_revoked.get('newLoginValid') is False and grpc_revoked.get('A') == '0' and grpc_revoked.get('C') == '1' and
            address(grpc_revoked.get('nft')) == nft, 'gRPC policy revocation was missing, expired or changed the asset')
    exits = report.get('grpcExits')
    require(isinstance(exits, list) and exits and all(e.get('exit') == 0 for e in exits) and
            {'challenge', 'admit', 'refresh', 'verify', 'manifest', 'segment'} <= {e.get('method') for e in exits}, 'missing or failed real gRPC process evidence')
    transfer = by_id['CW16_NFT_TRANSFER_SAFE']
    require(address(transfer.get('from')) == subject and address(transfer.get('to')) not in (subject, controller) and address(transfer.get('walletOwner')) == controller and
            transfer.get('A') == '0' and transfer.get('C') == '0' and transfer.get('B') == '1' and transfer.get('tokenId') == fixture.get('tokenId') and
            re.fullmatch('0x[0-9a-fA-F]{64}', transfer.get('transactionHash', '')) and re.fullmatch('0x[0-9a-fA-F]{64}', transfer.get('blockHash', '')) and
            re.fullmatch('0x[0-9a-fA-F]+', transfer.get('block', '')), 'missing safe NFT transfer or wallet policy changed with the asset')
    nft_manifest, nft_revoked = (by_id[k] for k in ('CW14_NFT_MANIFEST_DENIED_HTTP_GRPC', 'CW14_NFT_CAPABILITIES_REVOKED_WITH_LIVE_IDENTITY'))
    safe_at = number(nft_revoked.get('safeObservedAt'))
    baseline_at = number(transfer.get('baselineAt'))
    bounds = [number(transfer.get(k)) for k in ('httpAccessExp', 'httpCapabilityExp', 'grpcAccessExp', 'grpcCapabilityExp')]
    require(min(bounds) * 1000 - baseline_at >= 20000 and baseline_at <= safe_at < min(bounds) * 1000 and
            nft_manifest.get('safeObservedAt') == safe_at and nft_manifest.get('httpStatus') == 403 and nft_manifest.get('grpcCode') == 'PermissionDenied' and
            safe_at <= number(nft_manifest.get('rejectedAt')) <= safe_at + 70000 and nft_revoked.get('identityValid') is True and address(nft_revoked.get('nft')) == nft and
            nft_revoked.get('tokenId') == fixture.get('tokenId'), 'NFT transfer started with expired grants or did not deny new manifests')
    probes = report.get('nftProbes')
    require(isinstance(probes, list) and probes, 'missing NFT revocation observations')
    for probe in probes:
        at = number(probe.get('at'))
        require(safe_at <= at <= safe_at + 70000 and probe.get('grpcIdentityBefore') is True and probe.get('grpcIdentityAfter') is True and
                number(probe.get('httpIdentityExp')) * 1000 > at and probe.get('httpManifestStatus') in (200, 403) and probe.get('grpcManifestCode') in ('OK', 'PermissionDenied') and
                probe.get('httpMediaStatus') in (200, 401) and probe.get('grpcMediaCode') in ('OK', 'Unauthenticated', 'PermissionDenied'), 'NFT denial used invalid identity or dependency errors')
        digest(probe.get('httpTokenHash')); digest(probe.get('grpcTokenHash'))
    last = probes[-1]
    require(last['httpManifestStatus'] == 403 and last['grpcManifestCode'] == 'PermissionDenied' and last['httpMediaStatus'] == 401 and
            last['grpcMediaCode'] in ('Unauthenticated', 'PermissionDenied'), 'NFT capability never ended on both transports')
    first_http = next(p['at'] for p in probes if p['httpMediaStatus'] == 401)
    first_grpc = next(p['at'] for p in probes if p['grpcMediaCode'] in ('Unauthenticated', 'PermissionDenied'))
    require(nft_revoked.get('httpDeniedAt') == first_http and nft_revoked.get('grpcDeniedAt') == first_grpc and
            nft_revoked.get('httpElapsedMs') == first_http - safe_at and nft_revoked.get('grpcElapsedMs') == first_grpc - safe_at, 'NFT revocation timestamps do not match raw probes')
    nft_samples = report.get('grpcNFTSamples')
    require(isinstance(nft_samples, list) and nft_samples, 'missing gRPC identity continuity during transfer')
    nft_admissions = set()
    for sample in nft_samples:
        iat, exp, end = (number(sample.get(k)) for k in ('iat', 'exp', 'absolute'))
        require(address(sample.get('subject')) == subject and sample.get('chainId') == 31337 and sample.get('kind') == 'erc1271' and
                0 < exp - iat <= 60 and exp <= end <= iat + 7200, 'invalid gRPC identity during NFT transfer')
        nft_admissions.add((digest(sample.get('sessionHash')), digest(sample.get('proofHash')), end))
    require(len(nft_admissions) == 1, 'NFT transfer switched to another admission')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('report', 'fixture', 'content', 'exit-file'):
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--candidate', required=True)
    parser.add_argument('--mode', required=True)
    args = parser.parse_args()
    try:
        validate(*(json.loads(p.read_text()) for p in (args.report, args.fixture, args.content)),
                 args.candidate, args.mode, int(args.exit_file.read_text().strip()))
    except (OSError, ValueError, TypeError, KeyError, AttributeError) as error:
        print('FAIL: local wallet browser component: ' + str(error))
        return 1
    print('PASS: candidate local wallet browser component; public and full CW acceptance remain separate')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
