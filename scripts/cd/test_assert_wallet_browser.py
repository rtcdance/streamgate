import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True
SCRIPT = Path(__file__).with_name('assert_wallet_browser.py')
spec = importlib.util.spec_from_file_location('assert_wallet_browser', SCRIPT)
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)
CANDIDATE = 'a' * 40
A, C, N = ('0x' + value * 40 for value in ('1', '2', '3'))


def evidence():
    identity = {'candidateSHA': CANDIDATE, 'evidenceContext': 'CANDIDATE'}
    fixture = dict(identity, network='ANVIL_LOCAL_ONLY', chainId=31337, controller=A, contractWallet=C, nft=N, tokenId='1')
    content = dict(identity, mode='monolith', subject=C, contentId='fixture-content', freshUpload=True)
    checks = [{'id': name, 'ok': True, 'detail': {}} for name in sorted(gate.REQUIRED)]
    by_id = {check['id']: check for check in checks}
    by_id['CW01_H5_SIGNER_AND_SUBJECT']['detail'] = {'signer': A, 'subject': C}
    by_id['CW01_INDEPENDENT_SAFE_OWNERSHIP']['detail'] = {'A': '0', 'C': '1', 'block': '0x10', 'blockHash': '0x' + '4' * 64}
    by_id['CW09_CONTINUOUS_PLAYBACK_ACROSS_LEASES']['detail'] = {'elapsedSeconds': 71, 'mediaAdvance': 70, 'accessTokensObserved': 3}
    by_id['CW10_LIVE_POLICY_BASELINE']['detail'] = {'accessExp': 1150, 'capabilityExp': 1150, 'observedAt': 1100000, 'tokenHash': '6' * 64, 'capabilityHash': '7' * 64}
    by_id['CW10_SAFE_POLICY_ONLY_CHANGE']['detail'] = {'block': '0x20', 'blockHash': '0x' + '8' * 64, 'transactionHash': '0x' + '9' * 64, 'ownerBefore': A, 'ownerAfter': N, 'A': '0', 'C': '1'}
    by_id['CW10_POLICY_REVOKES_WITH_UNCHANGED_NFT']['detail'] = {'safeObservedAt': 1101000, 'rejectedAt': 1120000, 'elapsedMs': 19000, 'accessExp': 1150, 'capabilityExp': 1150,
        'oldAccessStatus': 401, 'oldSignerLoginStatus': 401, 'A': '0', 'C': '1', 'nft': N, 'tokenId': '1'}
    by_id['CW14_GRPC_LOGIN_SUBJECT']['detail'] = {'subject': C, 'chainId': 31337, 'kind': 'erc1271'}
    by_id['CW14_GRPC_REAL_MEDIA']['detail'] = {'sha256': '8' * 64, 'size': 10}
    by_id['CW14_GRPC_REFRESH_SUBJECT']['detail'] = {'tokens': 3, 'sha256': '8' * 64, 'capabilityHash': '9' * 64, 'accessExp': 1150, 'capabilityExp': 1150, 'observedAt': 1100000}
    by_id['CW10_GRPC_POLICY_REVOKES_WITH_UNCHANGED_NFT']['detail'] = {'safeObservedAt': 1101000, 'rejectedAt': 1102000, 'elapsedMs': 1000, 'accessExp': 1150, 'capabilityExp': 1150,
        'refreshCode': 'Unauthenticated', 'verifyCode': 'Unauthenticated', 'verifyValid': False, 'manifestCode': 'Unauthenticated', 'segmentCode': 'Unauthenticated', 'newLoginValid': False, 'A': '0', 'C': '1', 'nft': N}
    by_id['CW16_NFT_TRANSFER_SAFE']['detail'] = {'transactionHash': '0x' + 'a' * 64, 'block': '0x30', 'blockHash': '0x' + 'b' * 64, 'tokenId': '1', 'from': C, 'to': N, 'walletOwner': A,
        'A': '0', 'C': '0', 'B': '1', 'baselineAt': 1110000, 'httpAbsolute': 8300, 'httpAccessExp': 1160, 'httpCapabilityExp': 1160, 'grpcAccessExp': 1160, 'grpcCapabilityExp': 1160}
    by_id['CW14_NFT_MANIFEST_DENIED_HTTP_GRPC']['detail'] = {'safeObservedAt': 1111000, 'rejectedAt': 1140000, 'httpStatus': 403, 'grpcCode': 'PermissionDenied'}
    by_id['CW14_NFT_CAPABILITIES_REVOKED_WITH_LIVE_IDENTITY']['detail'] = {'safeObservedAt': 1111000, 'httpDeniedAt': 1140000, 'grpcDeniedAt': 1140000, 'httpElapsedMs': 29000, 'grpcElapsedMs': 29000, 'identityValid': True, 'nft': N, 'tokenId': '1'}
    samples = [{'at': (1000 + seconds) * 1000, 'currentTime': seconds + 1, 'tokenHash': str(seconds // 30 + 1) * 64,
                'iat': 1000 + seconds // 30 * 30, 'exp': 1060 + seconds // 30 * 30, 'absolute': 8200,
                'subject': C, 'chainId': 31337, 'kind': 'erc1271'} for seconds in (0, 10, 20, 30, 40, 50, 60, 71)]
    responses = [{'at': (1000 + seconds) * 1000, 'status': 200, 'tokenHash': str(index) * 64,
                  'claims': {'wallet_address': C, 'account_kind': 'erc1271', 'chain_id': 31337, 'content_id': 'fixture-content',
                             'contract': N, 'iat': 1000 + seconds, 'exp': 1060 + seconds,
                             'wallet_session_exp': 8200, 'token_type': 'playback'}} for index, seconds in ((1, 0), (2, 30))]
    report = dict(identity, component='LOCAL_WALLET_BROWSER', mode='monolith', testAdapter='EIP1193_LOCAL_ANVIL_REAL_SIGNATURE',
                  publicWalletSignoff=False, passed=True, errors=[], checks=checks, required=sorted(gate.REQUIRED),
                  signedMessages=[{'signer': A, 'digest': '0x' + '5' * 64}], samples=samples, mediaResponses=responses)
    report['grpcSamples'] = [{'at': (1000 + index * 30) * 1000, 'tokenHash': str(index + 1) * 64, 'sessionHash': '6' * 64, 'proofHash': '7' * 64,
        'subject': C, 'chainId': 31337, 'kind': 'erc1271', 'iat': 1000 + index * 30, 'exp': 1060 + index * 30, 'absolute': 8200} for index in range(3)]
    report['grpcExits'] = [{'method': method, 'exit': 0, 'code': 'OK'} for method in ('challenge', 'admit', 'refresh', 'verify', 'manifest', 'segment')]
    report['grpcBridgeSHA256'] = 'f' * 64
    report['grpcNFTSamples'] = [{'at': 1100000, 'tokenHash': 'd' * 64, 'sessionHash': 'a' * 64, 'proofHash': 'b' * 64,
        'subject': C, 'chainId': 31337, 'kind': 'erc1271', 'iat': 1100, 'exp': 1160, 'absolute': 8300}]
    report['nftProbes'] = [{'at': 1140000, 'httpManifestStatus': 403, 'grpcManifestCode': 'PermissionDenied', 'httpMediaStatus': 401, 'grpcMediaCode': 'Unauthenticated',
        'grpcIdentityBefore': True, 'grpcIdentityAfter': True, 'httpIdentityExp': 1160, 'httpTokenHash': 'c' * 64, 'grpcTokenHash': 'd' * 64}]
    return report, fixture, content


class BrowserComponentContract(unittest.TestCase):
    def validate(self, report, fixture, content, exit_code=0):
        gate.validate(report, fixture, content, CANDIDATE, 'monolith', exit_code)

    def test_complete_component_passes_with_explicit_local_boundary(self):
        self.validate(*evidence())

    def test_self_declared_empty_list_missing_or_failed_case_never_passes(self):
        for change in ('empty', 'missing', 'duplicate', 'failed', 'skip', 'errors', 'unrelated'):
            report, fixture, content = evidence()
            if change == 'empty': report['checks'] = []; report['required'] = []
            elif change == 'missing': report['checks'].pop()
            elif change == 'duplicate': report['checks'].append(copy.deepcopy(report['checks'][0]))
            elif change == 'failed': report['checks'][0]['ok'] = False
            elif change == 'skip': report['checks'][0]['ok'] = 'SKIP'
            elif change == 'errors': report['errors'] = ['page crashed after checks']
            else: report['checks'][0]['id'] = 'UNRELATED_PASS'
            with self.subTest(change=change), self.assertRaises(ValueError): self.validate(report, fixture, content)

    def test_identity_context_and_original_exit_are_external_requirements(self):
        for index in range(3):
            for field, value in (('candidateSHA', 'b' * 40), ('evidenceContext', 'DEVELOPMENT')):
                entries = evidence(); entries[index][field] = value
                with self.subTest(index=index, field=field), self.assertRaises(ValueError): self.validate(*entries)
        for field, value in (('mode', 'api-gateway'), ('publicWalletSignoff', True), ('testAdapter', 'REAL_WALLET_USER_OPERATED')):
            report, fixture, content = evidence(); report[field] = value
            with self.subTest(field=field), self.assertRaises(ValueError): self.validate(report, fixture, content)
        with self.assertRaises(ValueError): self.validate(*evidence(), exit_code=1)
        report, fixture, content = evidence(); content['freshUpload'] = False
        with self.assertRaises(ValueError): self.validate(report, fixture, content)

    def test_fabricated_lease_continuity_and_foreign_capabilities_fail(self):
        mutations = {
            'too_short': lambda r: r['samples'].pop(),
            'clock_reversed': lambda r: r['samples'][-1].update(at=r['samples'][0]['at']),
            'no_decode': lambda r: r['samples'][-1].update(currentTime=2),
            'lease_too_long': lambda r: r['samples'][0].update(exp=1061),
            'nan_advance': lambda r: r['samples'][-1].update(currentTime=float('nan')),
            'same_jwt': lambda r: [s.update(tokenHash='1' * 64) for s in r['samples']],
            'sliding_deadline': lambda r: r['samples'][-1].update(absolute=8201),
            'wrong_chain': lambda r: r['samples'][-1].update(chainId=1),
            'not_c': lambda r: r['mediaResponses'][0]['claims'].update(wallet_address=A),
            'wrong_content': lambda r: r['mediaResponses'][0]['claims'].update(content_id='other'),
            'wrong_nft': lambda r: r['mediaResponses'][0]['claims'].update(contract=A),
            'eoa_cap': lambda r: r['mediaResponses'][0]['claims'].update(account_kind='eoa'),
            'long_cap': lambda r: r['mediaResponses'][0]['claims'].update(exp=8300),
            'one_cap': lambda r: r['mediaResponses'][1].update(tokenHash='1' * 64),
            'no_signatures': lambda r: r.update(signedMessages=[]),
            'a_has_nft': lambda r: next(c for c in r['checks'] if c['id'] == 'CW01_INDEPENDENT_SAFE_OWNERSHIP')['detail'].update(A='1'),
        }
        for name, mutate in mutations.items():
            report, fixture, content = evidence(); mutate(report)
            with self.subTest(name=name), self.assertRaises(ValueError): self.validate(report, fixture, content)

    def test_cli_missing_or_malformed_evidence_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            report, fixture, content = evidence()
            for name, value in (('report', report), ('fixture', fixture), ('content', content)):
                (base / name).write_text(json.dumps(value))
            (base / 'exit').write_text('0\n')
            command = [sys.executable, str(SCRIPT), '--candidate', CANDIDATE, '--mode', 'monolith']
            for flag, name in (('report', 'report'), ('fixture', 'fixture'), ('content', 'content'), ('exit-file', 'exit')):
                command += ['--' + flag, str(base / name)]
            self.assertEqual(0, subprocess.run(command, capture_output=True).returncode)
            for name in ('report', 'fixture', 'content', 'exit'):
                path = base / name; raw = path.read_bytes(); path.unlink()
                with self.subTest(missing=name): self.assertEqual(1, subprocess.run(command, capture_output=True).returncode)
                path.write_bytes(raw)
            (base / 'report').write_text('{}')
            self.assertEqual(1, subprocess.run(command, capture_output=True).returncode)

    def test_policy_revocation_cannot_be_replaced_with_transfer_expiry_or_login_success(self):
        for key, value in (('elapsedMs', 70001), ('rejectedAt', 1150000), ('C', '0'), ('oldAccessStatus', 200), ('oldSignerLoginStatus', 200), ('tokenId', 'other')):
            report, fixture, content = evidence()
            next(c for c in report['checks'] if c['id'] == 'CW10_POLICY_REVOKES_WITH_UNCHANGED_NFT')['detail'][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError): self.validate(report, fixture, content)

    def test_grpc_transport_cannot_be_replaced_by_http_claims_or_jti_rotation(self):
        for change in ('missing', 'process-fail', 'changed-admission', 'no-renewal', 'wrong-kind', 'wrong-bytes', 'new-login-valid', 'refresh-accepted'):
            report, fixture, content = evidence()
            if change == 'missing': report['grpcExits'] = []
            elif change == 'process-fail': report['grpcExits'][0]['exit'] = 1
            elif change == 'changed-admission': report['grpcSamples'][-1]['sessionHash'] = 'f' * 64
            elif change == 'no-renewal': report['grpcSamples'] = report['grpcSamples'][:1]
            elif change == 'wrong-kind': report['grpcSamples'][-1]['kind'] = 'eoa'
            elif change == 'wrong-bytes': next(c for c in report['checks'] if c['id'] == 'CW14_GRPC_REAL_MEDIA')['detail']['sha256'] = 'f' * 64
            else:
                item = next(c for c in report['checks'] if c['id'] == 'CW10_GRPC_POLICY_REVOKES_WITH_UNCHANGED_NFT')['detail']
                item['newLoginValid' if change == 'new-login-valid' else 'refreshCode'] = True if change == 'new-login-valid' else 'OK'
            with self.subTest(change=change), self.assertRaises(ValueError): self.validate(report, fixture, content)

    def test_nft_revocation_needs_live_identity_both_transports_and_safe_ownership(self):
        for change in ('session-ended', 'no-probes', 'http-accepted', 'grpc-accepted', 'dependency', 'identity-expired', 'asset-still-C', 'policy-changed'):
            report, fixture, content = evidence()
            if change == 'session-ended': report['nftProbes'][-1]['grpcIdentityAfter'] = False
            elif change == 'no-probes': report['nftProbes'] = []
            elif change == 'http-accepted': report['nftProbes'][-1]['httpMediaStatus'] = 200
            elif change == 'grpc-accepted': report['nftProbes'][-1]['grpcMediaCode'] = 'OK'
            elif change == 'dependency': report['nftProbes'][-1]['grpcMediaCode'] = 'Unavailable'
            elif change == 'identity-expired': report['nftProbes'][-1]['httpIdentityExp'] = 1139
            else:
                item = next(c for c in report['checks'] if c['id'] == 'CW16_NFT_TRANSFER_SAFE')['detail']
                item['C' if change == 'asset-still-C' else 'walletOwner'] = '1' if change == 'asset-still-C' else N
            with self.subTest(change=change), self.assertRaises(ValueError): self.validate(report, fixture, content)


if __name__ == '__main__':
    unittest.main()
