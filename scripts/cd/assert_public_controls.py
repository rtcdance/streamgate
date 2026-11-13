"""Semantic gate for original public HTTP/gRPC transfer and failure observations.

These inputs are collected outside CI with the user's wallet. A report's PASS
labels, required list and decoded JWT claims cannot replace the private bytes.
"""
import base64
import hashlib
import hmac
import json
import math
import re


def require(ok, reason):
    if not ok:
        raise ValueError(reason)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def number(value):
    require(type(value) in (int, float) and math.isfinite(value), 'finite observation time required')
    return value


def decode_jwt(raw):
    require(isinstance(raw, str) and len(raw) <= 20000, 'original credential required')
    parts = raw.split('.')
    require(len(parts) == 3 and all(re.fullmatch('[A-Za-z0-9_-]+', part) for part in parts), 'invalid original credential encoding')
    values = [json.loads(base64.urlsafe_b64decode(part + '=' * (-len(part) % 4))) for part in parts[:2]]
    require(isinstance(values[0], dict) and values[0].get('alg') == 'HS256' and isinstance(values[1], dict), 'unexpected credential type')
    return values[1]


def authenticate_credentials(tokens, secret):
    require(isinstance(secret, str) and len(secret) >= 32, 'verified runtime signing key required')
    require(isinstance(tokens, list) and 0 < len(tokens) <= 2048, 'bounded original credentials required')
    for token in tokens:
        decode_jwt(token)
        unsigned, signature = token.rsplit('.', 1)
        expected = base64.urlsafe_b64encode(hmac.new(secret.encode(), unsigned.encode(), hashlib.sha256).digest()).decode().rstrip('=')
        # Use the secret from the independently inspected runtime, never a key
        # selected by a report. Keep both key and original token out of errors.
        require(hmac.compare_digest(expected, signature), 'credential was not signed by the verified runtime')


def playback_timeline(samples, started, safe, last_refusal):
    require(isinstance(samples, list) and 3 <= len(samples) <= 4096, 'original repeated playback timeline required')
    require(number(samples[0]['at']) <= started <= safe and number(samples[-1]['at']) >= last_refusal,
            'playback must start before the original transfer and cover the final media refusal')
    active = []; stopped = None; previous_at = number(samples[0]['at']); advance = 0
    for sample in samples:
        at = number(sample['at']); require(previous_at <= at <= previous_at + 5000, 'playback observation has a gap or time reversal'); previous_at = at
        if sample.get('paused') is True:
            require(sample.get('state') == 'authorization-ended' and safe - 5000 <= at <= safe + 70000,
                    'natural end, user pause or dependency error cannot prove NFT revocation')
            stopped = stopped or sample
            continue
        require(stopped is None and sample.get('paused') is False and sample.get('ended') is False and sample.get('loop') is True and
                sample.get('rate') == 1 and sample.get('error') is None, 'safe waiting did not preserve normal repeated playback')
        current = number(sample['time']); duration = number(sample['duration']); frames = number(sample['frames'])
        require(0 < duration <= 600 and 0 <= current <= duration and frames > 0, 'actual bounded fixture decode state required')
        if active:
            old = active[-1]; elapsed = (at - old['at']) / 1000; delta = current - old['time']
            require(abs(duration - old['duration']) <= 1 and frames >= old['frames'], 'media source or decoded frame counter changed')
            if delta < 0:
                require(old['time'] >= duration - elapsed - 2 and current <= elapsed + 2, 'time reset is not a natural end-of-media loop')
                delta += duration
            require(0 <= delta <= elapsed + 2, 'artificial seek or playback acceleration detected')
            advance += delta
        active.append(sample)
    require(stopped is not None and len(active) >= 2 and active[-1]['at'] >= safe - 5000 and
            active[-1]['frames'] > active[0]['frames'] and advance >= (active[-1]['at'] - active[0]['at']) / 1000 * 0.8,
            'decoded media did not continue until safe or stop for authorization loss')


def validate(report, private_raw, config, oracles, original_exit):
    require(original_exit == 0 and type(original_exit) is int, 'public control process did not finish successfully')
    require(report.get('schemaVersion') == 1 and report.get('component') == 'SEPOLIA_PUBLIC_CONTROLS' and
            report.get('candidateSHA') == config['candidateSHA'] and report.get('mode') == config['mode'] and
            report.get('chainId') == 11155111 and report.get('controller') == config['controller'] and
            report.get('wallet') == config['wallet'] and report.get('nft') == config['nft'] and report.get('content') == config['content'] and
            report.get('origin') == config['origin'] and report.get('completeAcceptance') is False and report.get('publicWalletSignoff') is False,
            'public controls belong to another candidate, deployment, subject or chain')
    require(sha(private_raw) == report.get('privateSHA256'), 'original private public-control evidence is missing or changed')
    private = json.loads(private_raw)
    require(set(private) == {'credentials', 'responses'}, 'unexpected private evidence layout')
    credentials, responses = private['credentials'], private['responses']
    require(isinstance(credentials, dict) and 0 < len(credentials) <= 2048 and isinstance(responses, dict) and len(responses) <= 10000,
            'private evidence is missing or exceeds its budget')
    authenticate_credentials(list(credentials.values()), config.get('jwtSecret'))
    claims = {}
    for digest, token in credentials.items():
        require(sha(token.encode()) == digest, 'original credential hash mismatch')
        claims[digest] = decode_jwt(token)
    for digest, raw in responses.items():
        require(isinstance(raw, str) and sha(base64.b64decode(raw, validate=True)) == digest, 'original response body hash mismatch')

    def identity(digest, subject, at, capability=False):
        value = claims[digest]
        require(str(value.get('wallet_address', '')).lower() == subject and value.get('chain_id') == 11155111 and
                number(value.get('iat')) * 1000 <= at + 5000 < number(value.get('exp')) * 1000,
                'observation uses expired or foreign credentials')
        audience = value.get('aud'); audience = audience if isinstance(audience, list) else [audience]
        if capability:
            require(value.get('iss') == 'streamgate' and value.get('token_type') == 'playback' and 'streamgate-playback' in audience and
                    value.get('content_id') == config['content'] and str(value.get('contract', '')).lower() == config['nft'], 'wrong playback capability purpose/content/NFT')
        else:
            require(value.get('iss') == 'streamgate' and value.get('token_type') == 'access' and 'streamgate-api' in audience and str(value.get('sub', '')).lower() == subject,
                    'wrong access token purpose or subject')
        if subject == config['wallet']:
            require(value.get('account_kind') == 'erc1271' and value.get('session_id', '').startswith('cw:') and value.get('wallet_proof_id') and
                    value['exp'] - value['iat'] <= 60 and value['exp'] <= value.get('wallet_session_exp', 0), 'contract credential lost its proof/lease')
        return value

    def body(event):
        digest = event.get('bodySHA256'); require(digest in responses, 'original response bytes missing')
        return base64.b64decode(responses[digest], validate=True)

    def event(value, transport, method, code, subject, capability=False):
        require(value.get('transport') == transport and value.get('method') == method and value.get('code') == code and
                value.get('origin') == config['origin'], 'wrong original transport, operation, response or origin')
        at = number(value.get('at')); identity(value['credentialSHA256'], subject, at, capability)
        body(value)
        return at

    def live(value, subject):
        at = event(value['http'], 'http', 'profile', 200, subject)
        other = event(value['grpc'], 'grpc', 'VerifyToken', 'OK', subject)
        require(abs(at - other) <= 5000, 'HTTP/gRPC live identity observations are too far apart')
        access = identity(value['http']['credentialSHA256'], subject, at)
        grpc_access = identity(value['grpc']['credentialSHA256'], subject, other)
        require(access['session_id'] == grpc_access['session_id'] and access['auth_epoch'] == grpc_access['auth_epoch'], 'transport identities use different sessions')
        profile = json.loads(body(value['http'])); verified = json.loads(body(value['grpc']))
        require(verified.get('method') == 'verify' and verified.get('code') == 'OK', 'original gRPC client result belongs to another operation/status')
        verified = verified.get('data', {})
        require(str(profile.get('wallet_address', profile.get('subject', ''))).lower() == subject and
                verified.get('valid') is True,
                'original application responses did not verify the identity')
        return min(at, other), max(at, other), access

    transfers = report.get('transfers')
    require(isinstance(transfers, dict) and set(transfers) == {'eoa', 'contract'}, 'both real transfer timelines required')
    for role, subject in [('eoa', config['controller']), ('contract', config['wallet'])]:
        value = transfers[role]; oracle = oracles[role + '-transferred']
        require(value.get('oracleSHA256') == oracle['sha256'], 'transfer uses another original chain oracle')
        safe = oracle['report']['safeObservedAtMs']; baseline = value['baseline']
        _, before, admitted = live(baseline['identity'], subject)
        binding = config['sessionBindings'][role]
        require(all(admitted.get(key) == binding.get(key) for key in ('session_id', 'auth_epoch', 'restore_epoch', 'wallet_proof_id', 'wallet_session_exp')),
                'transfer observations are not from the original independently verified wallet admission')
        media = baseline['media']; event(media, 'http', 'segment', 200, subject, True)
        grpc_media = baseline['grpcMedia']; event(grpc_media, 'grpc', 'GetSegment', 'OK', subject, True)
        grpc_body = json.loads(body(grpc_media))
        require(grpc_body.get('method') == 'segment' and grpc_body.get('code') == 'OK' and
                grpc_body.get('data') == {'size': len(body(media)), 'sha256': sha(body(media))}, 'gRPC did not return the same real media bytes')
        require(0 < len(body(media)) and before <= safe and safe - before <= 5000 and
                all(safe - 5000 <= number(observed['at']) <= safe for observed in (media, grpc_media)),
                'live media sample must precede and remain near the original safe observation')
        old_capability = media['credentialSHA256']; cap = identity(old_capability, subject, safe, True)
        require(cap['session_id'] == admitted['session_id'] and cap['exp'] <= admitted['exp'], 'old capability did not inherit the presented live credential')
        samples = value['identities']; require(isinstance(samples, list) and len(samples) >= 2, 'continuous live session samples missing')
        previous = before; last = before
        for sample in samples:
            start, end, current = live(sample, subject)
            require(previous <= start <= previous + 5000 and end - start <= 5000 and current['session_id'] == admitted['session_id'] and
                    current['auth_epoch'] == admitted['auth_epoch'] and current.get('wallet_proof_id') == admitted.get('wallet_proof_id') and
                    current.get('wallet_session_exp') == admitted.get('wallet_session_exp'), 'session stopped, was replaced or slid its absolute deadline')
            previous = start; last = end
        refusal = value['refusal']
        denied_http = event(refusal['httpManifest'], 'http', 'manifest', 403, subject)
        denied_grpc = event(refusal['grpcManifest'], 'grpc', 'GetManifest', 'PermissionDenied', subject)
        # Old media may expire within its original lease; the continuously live
        # identity and fresh-manifest denials distinguish NFT loss from logout.
        for key, transport, method, code in [('httpMedia', 'http', 'segment', 401), ('grpcMedia', 'grpc', 'GetSegment', 'Unauthenticated')]:
            observed = refusal[key]
            require(observed.get('credentialSHA256') == old_capability and observed.get('transport') == transport and
                    observed.get('method') == method and observed.get('code') == code and observed.get('origin') == config['origin'], 'old media capability was replaced or refusal was not observed')
            body(observed)
            at = number(observed.get('at')); require(safe <= at <= safe + 70000 and at <= last <= at + 5000, 'media refusal lacks a bounded live identity witness')
        require(safe <= denied_http <= safe + 70000 and safe <= denied_grpc <= safe + 70000 and
                max(denied_http, denied_grpc) <= last <= max(denied_http, denied_grpc) + 5000, 'manifest denial lacks a bounded live identity witness')
        playback_timeline(value['playback'], config['transferStartedAt'][role], safe, max(number(item['at']) for item in refusal.values()))

    fault = report['dependencyOutage']; subject = config['wallet']
    _, before, admitted = live(fault['before'], subject)
    unavailable = event(fault['http'], 'http', 'refresh', 503, subject)
    grpc = event(fault['grpc'], 'grpc', 'RefreshToken', 'Unavailable', subject)
    after, _, restored = live(fault['after'], subject)
    require(before <= unavailable <= after and before <= grpc <= after and after - before <= 60000 and
            restored['session_id'] == admitted['session_id'] and fault.get('uiState') == 'unavailable' and
            fault.get('anonymousAccessGranted') is False, 'dependency outage became denial/anonymous success or lost its original session')
    anonymous = fault['anonymous']
    for key, transport, method, code in [('http', 'http', 'manifest', 401), ('grpc', 'grpc', 'GetManifest', 'Unauthenticated')]:
        observed = anonymous[key]
        require(observed.get('transport') == transport and observed.get('method') == method and observed.get('code') == code and
                observed.get('origin') == config['origin'] and before <= number(observed['at']) <= after and
                'credentialSHA256' not in observed, 'original anonymous manifest refusal during the outage required')
        body(observed)
    reload = report['reload']
    require(reload.get('navigationType') == 'reload' and number(reload['finishedAt']) >= number(reload['startedAt']) and
            reload.get('provider') == 'MetaMask' and reload.get('chainId') == 11155111 and reload.get('account') == config['controller'], 'real browser reload/wallet observation missing')
    if reload.get('authenticated') is True:
        at, _, _ = live(reload['identity'], subject)
        require(at >= reload['finishedAt'], 'reload reused a prior identity response')
    else:
        require(reload.get('authenticated') is False and reload.get('hasAccessToken') is False and
                reload.get('uiState') in ('unknown', 'signed-out'), 'reload kept a half-authenticated state')
    return {'transfer': True, 'outage': True, 'reload': True}
