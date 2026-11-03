"""Adversarial evidence controls with synthetic credentials; not acceptance."""
import base64
import copy
import hashlib
import hmac
import json
import unittest

import assert_public_controls as gate


def fixture():
    config = dict(candidateSHA='a' * 40, mode='monolith', controller='0x' + '1' * 40, wallet='0x' + '2' * 40,
                  nft='0x' + '3' * 40, content='test-content', origin='http://localhost:18500')
    private = dict(credentials={}, responses={})

    def token(subject, capability=False, issued=1000):
        values = dict(iss='streamgate', aud=['streamgate-playback' if capability else 'streamgate-api'],
            token_type='playback' if capability else 'access', sub=config['content'] if capability else subject,
            wallet_address=subject, chain_id=11155111, session_id='cw:subject' if subject == config['wallet'] else 'eoa:subject',
            auth_epoch=1, restore_epoch='uuid', iat=issued, exp=issued + (40 if capability else 60),
            content_id=config['content'], contract=config['nft'])
        if subject == config['wallet']: values.update(account_kind='erc1271', wallet_proof_id='immutable', wallet_session_exp=8000)
        encode = lambda x: base64.urlsafe_b64encode(json.dumps(x).encode()).decode().rstrip('=')
        unsigned = encode(dict(alg='HS256', typ='JWT')) + '.' + encode(values)
        raw = unsigned + '.' + base64.urlsafe_b64encode(hmac.new(b'unit-fixture', unsigned.encode(), hashlib.sha256).digest()).decode().rstrip('=')
        digest = gate.sha(raw.encode()); private['credentials'][digest] = raw
        return digest

    def event(transport, method, code, subject, at, capability=False, raw=None, issued=1000):
        if raw is None: raw = json.dumps({'wallet_address': subject} if transport == 'http' else dict(method='verify', code='OK', data=dict(valid=True))).encode()
        digest = gate.sha(raw); private['responses'][digest] = base64.b64encode(raw).decode()
        return dict(transport=transport, method=method, code=code, origin=config['origin'], credentialSHA256=token(subject, capability, issued), at=at * 1000, bodySHA256=digest)

    def live(subject, at, issued=1000):
        return dict(http=event('http', 'profile', 200, subject, at, issued=issued),
                    grpc=event('grpc', 'VerifyToken', 'OK', subject, at, issued=issued))

    transfers, oracles = {}, {}
    media = b'unit-media-bytes'
    for role in ('eoa', 'contract'):
        subject = config['controller' if role == 'eoa' else 'wallet']
        held = dict(identity=live(subject, 1008), media=event('http', 'segment', 200, subject, 1008, True, media),
            grpcMedia=event('grpc', 'GetSegment', 'OK', subject, 1008, True,
                json.dumps(dict(method='segment', code='OK', data=dict(size=len(media), sha256=gate.sha(media)))).encode()))
        refusal = dict(httpManifest=event('http', 'manifest', 403, subject, 1023),
            grpcManifest=event('grpc', 'GetManifest', 'PermissionDenied', subject, 1023),
            httpMedia=event('http', 'segment', 401, subject, 1024, True),
            grpcMedia=event('grpc', 'GetSegment', 'Unauthenticated', subject, 1024, True))
        transfers[role] = dict(oracleSHA256='b' * 64, baseline=held, identities=[live(subject, at) for at in (1012, 1016, 1020, 1024)], refusal=refusal)
        transfers[role]['playback'] = [dict(at=at * 1000, time=at - 1000, duration=105, frames=(at - 999) * 30,
                                          paused=False, ended=False, loop=True, rate=1, error=None, state='playing') for at in (1000, 1004, 1008)]
        transfers[role]['playback'] += [dict(at=at * 1000, paused=True, state='authorization-ended') for at in (1012, 1016, 1020, 1024)]
        oracles[role + '-transferred'] = dict(sha256='b' * 64, report=dict(safeObservedAtMs=1010000))
    subject = config['wallet']
    report = dict(config, schemaVersion=1, component='SEPOLIA_PUBLIC_CONTROLS', chainId=11155111, completeAcceptance=False, publicWalletSignoff=False,
        transfers=transfers, dependencyOutage=dict(before=live(subject, 1100, 1100),
            http=event('http', 'refresh', 503, subject, 1103, issued=1100), grpc=event('grpc', 'RefreshToken', 'Unavailable', subject, 1104, issued=1100),
            after=live(subject, 1110, 1100), uiState='unavailable', anonymousAccessGranted=False),
        reload=dict(navigationType='reload', startedAt=1111000, finishedAt=1112000, provider='MetaMask', account=config['controller'], chainId=11155111,
            authenticated=True, identity=live(subject, 1113, 1100)))
    anonymous = dict(http=event('http', 'manifest', 401, subject, 1105, issued=1100),
                     grpc=event('grpc', 'GetManifest', 'Unauthenticated', subject, 1105, issued=1100))
    for observed in anonymous.values(): observed.pop('credentialSHA256')
    report['dependencyOutage']['anonymous'] = anonymous
    raw = json.dumps(private).encode(); report['privateSHA256'] = gate.sha(raw)
    config['sessionBindings'] = {role: gate.decode_jwt(private['credentials'][transfers[role]['baseline']['identity']['http']['credentialSHA256']]) for role in transfers}
    config['transferStartedAt'] = dict(eoa=1002000, contract=1002000)
    return report, raw, config, oracles


class PublicControlEvidenceTests(unittest.TestCase):
    def test_complete_semantic_control_records_are_accepted(self):
        report, raw, config, oracles = fixture()
        self.assertEqual(dict(transfer=True, outage=True, reload=True), gate.validate(report, raw, config, oracles, 0))

    def test_foreign_identities_missing_transport_stale_session_and_wrong_media_fail(self):
        changes = [lambda r: r.update(candidateSHA='0' * 40), lambda r: r.update(mode='api-gateway'),
            lambda r: r['transfers'].pop('contract'), lambda r: r['transfers']['eoa'].update(oracleSHA256='0' * 64),
            lambda r: r['transfers']['contract']['identities'].pop(1),
            lambda r: r['transfers']['contract']['refusal']['grpcMedia'].update(code='PermissionDenied'),
            lambda r: r['transfers']['eoa']['baseline']['grpcMedia'].update(bodySHA256=r['transfers']['eoa']['baseline']['media']['bodySHA256']),
            lambda r: r['transfers']['eoa']['refusal']['httpManifest'].update(at=1100000),
            lambda r: r['transfers']['eoa']['baseline']['media'].update(credentialSHA256=r['transfers']['eoa']['baseline']['identity']['http']['credentialSHA256']),
            lambda r: r['dependencyOutage']['http'].update(code=401), lambda r: r['dependencyOutage'].update(anonymousAccessGranted=True),
            lambda r: r['dependencyOutage']['anonymous']['http'].update(code=200), lambda r: r['dependencyOutage'].pop('anonymous'),
            lambda r: r['transfers']['eoa']['baseline']['media'].update(at=1011000),
            lambda r: r['transfers']['contract']['playback'][1].update(loop=False),
            lambda r: r['transfers']['contract']['playback'][1].update(time=90),
            lambda r: r['transfers']['contract']['playback'][3].update(state='ended'),
            lambda r: r['transfers']['contract']['playback'].pop(1),
            lambda r: r['reload'].update(navigationType='navigate'), lambda r: r['reload'].update(authenticated=False, hasAccessToken=True, uiState='signed-out')]
        report, raw, config, oracles = fixture()
        for change in changes:
            edited = copy.deepcopy(report); change(edited)
            with self.subTest(change=change), self.assertRaises((ValueError, KeyError, TypeError)):
                gate.validate(edited, raw, config, oracles, 0)

    def test_original_private_bytes_and_raw_process_exit_are_required(self):
        report, raw, config, oracles = fixture()
        for changed, code in [(raw + b' ', 0), (raw, 1)]:
            with self.assertRaises(ValueError): gate.validate(report, changed, config, oracles, code)
        private = json.loads(raw); digest = next(iter(private['credentials'])); private['credentials'][digest] += 'edited'
        changed = json.dumps(private).encode(); report['privateSHA256'] = gate.sha(changed)
        with self.assertRaises(ValueError): gate.validate(report, changed, config, oracles, 0)


if __name__ == '__main__':
    unittest.main()
