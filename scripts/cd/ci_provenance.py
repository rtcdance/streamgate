"""Read-only binding between a local acceptance payload and its original CI job."""
from datetime import datetime
import json
from pathlib import Path
import re
import subprocess

ACCEPTANCE_JOB = 'Complete Docker Acceptance (M0-M8, one candidate)'


class UnavailableError(ValueError):
    pass


def gh_json(endpoint, paginated=False):
    command = ['gh', 'api', endpoint]
    if paginated: command += ['--paginate', '--slurp']
    result = subprocess.run(command, capture_output=True, text=True, timeout=60)
    if result.returncode: raise UnavailableError('authoritative GitHub CI metadata is unavailable')
    try: return json.loads(result.stdout)
    except (ValueError, TypeError): raise UnavailableError('authoritative GitHub CI response is malformed') from None


def default_run(repository, run_id, candidate):
    if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', repository) or not re.fullmatch('[1-9][0-9]*', str(run_id)) or not re.fullmatch('[0-9a-f]{40}', candidate):
        raise ValueError('explicit repository, run and full candidate SHA are required')
    value = gh_json(f'repos/{repository}/actions/runs/{run_id}')
    if value.get('id') != int(run_id) or type(value.get('run_attempt')) is not int or value['run_attempt'] < 1 or value.get('head_sha') != candidate or value.get('status') != 'completed' or value.get('conclusion') != 'success' or value.get('path') != '.github/workflows/ci.yml':
        raise ValueError('selected default CI run is not a successful run of this candidate')
    expected_url = f'https://github.com/{repository}/actions/runs/{run_id}'
    if str(value.get('html_url', '')).lower() != expected_url.lower(): raise ValueError('CI run URL does not identify the selected repository/run')
    return {key: value[key] for key in ('id', 'run_attempt', 'head_sha', 'html_url', 'conclusion')}


def producer_job(repository, run, producer):
    attempt = str(producer.get('runAttempt', ''))
    if not re.fullmatch('[1-9][0-9]*', attempt) or int(attempt) > run['run_attempt']: raise ValueError('invalid producing attempt')
    pages = gh_json(f'repos/{repository}/actions/runs/{run["id"]}/attempts/{attempt}/jobs?per_page=100', paginated=True)
    if not isinstance(pages, list) or not pages: raise UnavailableError('producing CI jobs are unavailable')
    jobs = [job for page in pages for job in page.get('jobs', []) if job.get('name') == ACCEPTANCE_JOB]
    if len(jobs) != 1: raise ValueError('exact original acceptance job is missing or ambiguous')
    job = jobs[0]
    if job.get('run_id') != run['id'] or job.get('head_sha') != run['head_sha'] or job.get('status') != 'completed' or job.get('conclusion') != 'success' or type(job.get('id')) is not int:
        raise ValueError('original producing acceptance job was not successful for this candidate/run')
    for key in ('started_at', 'completed_at'):
        if not isinstance(job.get(key), str): raise ValueError('original job timestamps are missing')
        if datetime.fromisoformat(job[key].replace('Z', '+00:00')).tzinfo is None: raise ValueError('job time lacks timezone')
    return job


def parse_upload_record(lines, producer):
    matches = []
    for line in lines:
        # GitHub adds a timestamp before each original stdout line. The tool
        # emits one JSON object; never copy the surrounding job log to stdout.
        _, separator, suffix = line.partition('{')
        if not separator: continue
        try: value = json.loads('{' + suffix)
        except ValueError: continue
        if isinstance(value, dict) and value.get('name') == producer['name'] and value.get('status') == 'pushed': matches.append(value)
    if len(matches) != 1: raise ValueError('exact original evidence upload summary is missing or duplicated')
    value = matches[0]
    for key, expected in [('sourceSHA', producer['sourceSHA']), ('runID', str(producer['runID'])), ('runAttempt', str(producer['runAttempt'])), ('bundleSHA256', producer['bundleSHA256'])]:
        if str(value.get(key)) != expected: raise ValueError('local payload differs from the original CI upload identity')
    entries = producer['entries']
    if type(value.get('entries')) is not int or value['entries'] != len(entries) or type(value.get('bytes')) is not int or value['bytes'] != sum(entry['size'] for entry in entries):
        raise ValueError('original CI upload byte/file counts differ from the local payload')
    digest = value.get('digest')
    tag = f'g{producer["runID"]}-a{producer["runAttempt"]}-{producer["name"]}'.lower()
    expected = 'ghcr.io/rtcdance/streamgate-evidence:' + tag
    if not re.fullmatch('sha256:[0-9a-f]{64}', str(digest)) or value.get('image') != expected:
        raise ValueError('original CI upload lacks the expected immutable registry identity')
    return {key: value[key] for key in ('name', 'sourceSHA', 'runID', 'runAttempt', 'bundleSHA256', 'image', 'digest', 'entries', 'bytes')}


def original_upload(repository, job, producer):
    # Spool the original log privately and scan it once. This is a GET only;
    # never retry, re-dispatch, cancel or otherwise mutate a valid CI run.
    import tempfile
    with tempfile.TemporaryFile(mode='w+', encoding='utf-8') as log:
        result = subprocess.run(['gh', 'api', f'repos/{repository}/actions/jobs/{job["id"]}/logs'], stdout=log,
                                stderr=subprocess.PIPE, text=True, timeout=120)
        if result.returncode: raise UnavailableError('original producing job log is unavailable or expired')
        if log.tell() > 128 * 1024 * 1024: raise UnavailableError('original CI job log exceeds the bounded read budget')
        log.seek(0)
        return parse_upload_record(log, producer)


def bind_upload(repository, run, producer):
    job = producer_job(repository, run, producer)
    upload = original_upload(repository, job, producer)
    return dict(jobID=job['id'], jobURL=job['html_url'], startedAt=job['started_at'], completedAt=job['completed_at'], upload=upload)
