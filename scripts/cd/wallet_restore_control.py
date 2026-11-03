#!/usr/bin/env python3
"""Restore only the owned durability scratch database while a CW test is paused."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time
import uuid

DATABASE = 'streamgate_durability'


def atomic_json(path, value):
    temporary = path.with_name(path.name + '.pending')
    with open(temporary, 'w', opener=lambda p, flags: os.open(p, flags, 0o600)) as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')
    temporary.replace(path)


def validate_owner(value, project):
    if not re.fullmatch('[a-z0-9][a-z0-9_-]{0,80}', project):
        raise ValueError('invalid controller project identity')
    labels = value.get('Config', {}).get('Labels') or {}
    if labels.get('com.docker.compose.project') != project or labels.get('com.streamgate.verify.run') != project or not value.get('Name', '').startswith('/' + project + '-postgres-') or not value.get('State', {}).get('Running'):
        raise ValueError('refusing a database outside the running owned verification project')


def run(args):
    report = {'scope': 'CW_SCRATCH_POSTGRES_RESTORE_CONTROL', 'candidateSHA': args.candidate, 'project': args.project,
              'database': DATABASE, 'completeAcceptance': False, 'ok': False, 'steps': [],
              'at': datetime.now(timezone.utc).isoformat(), 'toolSHA256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}
    barrier = args.barrier.resolve()
    allowed_barrier = '.private' in barrier.parts and barrier.is_dir()
    try:
        if not allowed_barrier or not re.fullmatch('[0-9a-f]{40}', args.candidate):
            raise ValueError('an existing private barrier and exact candidate are required')
        ready = json.loads((barrier / 'ready.json').read_text())
        if ready.get('phase') != 'READY' or ready.get('chainId') != 31337 or ready.get('tokenExpiresAt', 0) < time.time() + 10:
            raise ValueError('the real local CW sample is absent or too near expiry')
        old = str(uuid.UUID(ready['oldEpoch']))
        observed = subprocess.run(['docker', 'inspect', args.postgres], capture_output=True, text=True, timeout=10)
        if observed.returncode:
            raise ValueError('owned Postgres inspection failed')
        value = json.loads(observed.stdout)[0]
        validate_owner(value, args.project)
        container = value['Id']
        report.update(containerID=container, oldEpoch=old)

        def command(name, program, source=None, destination=None):
            cmd = ['docker', 'exec'] + (['-i'] if source is not None else []) + [container] + program
            stderr_path = barrier / (name + '.stderr')
            step = {'name': name, 'command': cmd, 'exit': None}
            report['steps'].append(step)
            with stderr_path.open('wb') as errors:
                try:
                    result = subprocess.run(cmd, stdin=source, stdout=destination if destination is not None else subprocess.PIPE, stderr=errors, timeout=15)
                except subprocess.TimeoutExpired:
                    step['timedOut'] = True
                    raise
            step['exit'] = result.returncode
            if result.returncode:
                raise ValueError('scratch database ' + name + ' failed; no retry was dispatched')
            return result.stdout.decode().strip() if destination is None else None

        def read_epoch(name):
            return command(name, ['psql', '-U', 'postgres', '-d', DATABASE, '-At', '-v', 'ON_ERROR_STOP=1', '-c', 'SELECT restore_epoch FROM auth_control_state WHERE id = 1'])

        if read_epoch('before') != old:
            raise ValueError('scratch database and paused credential have different epochs')
        dump = barrier / 'postgres.sql'
        with dump.open('wb') as destination:
            command('dump', ['pg_dump', '-U', 'postgres', '--clean', '--if-exists', '--no-owner', '--no-acl', DATABASE], destination=destination)
        dump.chmod(0o600)
        if dump.stat().st_size == 0:
            raise ValueError('the original pg_dump output is empty')
        with dump.open('rb') as stream:
            report['dumpSHA256'] = hashlib.file_digest(stream, 'sha256').hexdigest()
        report['dumpBytes'] = dump.stat().st_size
        with dump.open('rb') as source:
            command('restore', ['psql', '-U', 'postgres', '-d', DATABASE, '--single-transaction', '-v', 'ON_ERROR_STOP=1'], source=source)
        if read_epoch('restored') != old:
            raise ValueError('the SQL snapshot did not restore the original authority')
        new = str(uuid.uuid4())
        command('rotate', ['psql', '-U', 'postgres', '-d', DATABASE, '-v', 'ON_ERROR_STOP=1', '-c',
                          "UPDATE auth_control_state SET restore_epoch = '" + new + "', updated_at = NOW() WHERE id = 1"])
        if read_epoch('rotated') != new or ready['tokenExpiresAt'] <= time.time() + 5:
            raise ValueError('rotated epoch or still-live credential could not be confirmed')
        report.update(ok=True, newEpoch=new, completedAt=datetime.now(timezone.utc).isoformat())
    except (OSError, ValueError, KeyError, TypeError, IndexError, AttributeError, subprocess.SubprocessError) as error:
        report['reason'] = str(error)
    finally:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        atomic_json(args.output, report)
        if allowed_barrier:
            # The test resumes only after restore and rotation; a failure also
            # releases it explicitly so it cannot mistake a timeout for PASS.
            atomic_json(barrier / 'done.json', {key: report.get(key) for key in ('ok', 'oldEpoch', 'newEpoch')})
    print(('PASS' if report['ok'] else 'FAIL') + ': owned CW scratch restore control; complete acceptance=false')
    return 0 if report['ok'] else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--barrier', type=Path, required=True)
    parser.add_argument('--postgres', required=True)
    parser.add_argument('--project', required=True)
    parser.add_argument('--candidate', required=True)
    parser.add_argument('--output', type=Path, required=True)
    return run(parser.parse_args())


if __name__ == '__main__':
    raise SystemExit(main())
