"""Synthetic CI controls; local manifests do not authenticate themselves."""
import copy
import json
from pathlib import Path
import sys
import unittest
from unittest import mock

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parent))
import ci_provenance as tool

SHA = 'a' * 40
PRODUCER = dict(name='ci-acceptance-' + SHA, sourceSHA=SHA, runID='123', runAttempt='1', bundleSHA256='b' * 64,
                entries=[dict(path='original', size=2, sha256='c' * 64)])


def upload():
    return dict(name=PRODUCER['name'], status='pushed', sourceSHA=SHA, runID='123', runAttempt='1', bundleSHA256='b' * 64,
        entries=1, bytes=2, digest='sha256:' + 'd' * 64, image='ghcr.io/rtcdance/streamgate-evidence:g123-a1-' + PRODUCER['name'])


class CIProvenanceContract(unittest.TestCase):
    def test_timestamped_original_upload_binds_all_local_payload_bytes(self):
        value = tool.parse_upload_record(['unrelated log\n', '2026-11-03T08:00:00Z ' + json.dumps(upload()) + '\n'], PRODUCER)
        self.assertEqual('b' * 64, value['bundleSHA256']); self.assertEqual(2, value['bytes'])

    def test_self_consistent_local_manifest_is_not_authority_over_original_ci(self):
        producer = copy.deepcopy(PRODUCER); producer['bundleSHA256'] = 'f' * 64
        with self.assertRaisesRegex(ValueError, 'original CI upload'): tool.parse_upload_record([json.dumps(upload())], producer)

    def test_wrong_sha_attempt_run_image_digest_byte_count_or_duplicate_upload_refuses(self):
        for change in [dict(sourceSHA='e' * 40), dict(runID='124'), dict(runAttempt='3'), dict(image='ghcr.io/foreign/image:tag'),
            dict(digest=None), dict(bytes=3), dict(entries=2), dict(status='prepared')]:
            value = dict(upload(), **change)
            with self.subTest(change=change), self.assertRaises(ValueError): tool.parse_upload_record([json.dumps(value)], PRODUCER)
        with self.assertRaisesRegex(ValueError, 'duplicated'): tool.parse_upload_record([json.dumps(upload())] * 2, PRODUCER)

    def test_preserves_real_earlier_producer_attempt_and_requires_exact_successful_job(self):
        job = dict(id=789, run_id=123, head_sha=SHA, name=tool.ACCEPTANCE_JOB, status='completed', conclusion='success',
            html_url='https://github.com/rtcdance/streamgate/actions/runs/123/job/789', started_at='2026-11-03T07:00:00Z', completed_at='2026-11-03T08:00:00Z')
        run = dict(id=123, run_attempt=3, head_sha=SHA)
        with mock.patch.object(tool, 'gh_json', return_value=[dict(jobs=[job])]) as request:
            self.assertEqual(789, tool.producer_job('rtcdance/streamgate', run, PRODUCER)['id'])
            self.assertIn('/attempts/1/jobs', request.call_args.args[0])
        for change in [dict(head_sha='f' * 40), dict(run_id=124), dict(conclusion='failure'), dict(status='in_progress')]:
            with self.subTest(change=change), mock.patch.object(tool, 'gh_json', return_value=[dict(jobs=[dict(job, **change)])]):
                with self.assertRaises(ValueError): tool.producer_job('rtcdance/streamgate', run, PRODUCER)

    def test_missing_or_ambiguous_original_job_cannot_be_replaced_by_some_green_job(self):
        run = dict(id=123, run_attempt=3, head_sha=SHA)
        for jobs in [[], [dict(name='Unit Tests', conclusion='success')], [dict(name=tool.ACCEPTANCE_JOB)] * 2]:
            with self.subTest(jobs=jobs), mock.patch.object(tool, 'gh_json', return_value=[dict(jobs=jobs)]):
                with self.assertRaises(ValueError): tool.producer_job('rtcdance/streamgate', run, PRODUCER)


if __name__ == '__main__':
    unittest.main()
