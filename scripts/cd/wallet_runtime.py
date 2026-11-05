"""Read-only attestation of the prepared, running same-candidate Sepolia stack."""
from datetime import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess


def require(condition, reason):
    if not condition:
        raise ValueError(reason)


def command(arguments, env=None):
    result = subprocess.run(arguments, capture_output=True, timeout=60, env=env)
    if result.returncode:
        raise FileNotFoundError('owned public stack or immutable image is unavailable')
    return result.stdout


def inspect_prepared(directory, candidate, capsule, source_bytes, source_h5_paths, proxy_config, original_ci):
    preparation = json.loads((directory / 'preparation.json').read_text())
    manifest = json.loads((capsule / 'candidate.json').read_text())
    require(preparation.get('candidateSHA') == candidate and preparation.get('candidateArchiveSHA256') == manifest['archiveSHA256'] and
            preparation.get('originalCI') == original_ci and preparation.get('completeAcceptance') is False, 'preparation did not use original same-C acceptance')
    require((directory / 'compose.yml').read_bytes() == source_bytes('deploy/docker/compose.w3sepolia.yml'), 'prepared compose differs from candidate')
    for mode in ('monolith', 'api-gateway'):
        require((directory / (mode + '.nginx.conf')).read_text() == proxy_config(mode), 'public proxy differs from the prepared same-origin route')
    env_path = directory / 'stack.env.json'
    require(env_path.stat().st_mode & 0o077 == 0, 'private runtime input has permissive permissions')
    variables = json.loads(env_path.read_text()); project = variables.get('W3_PROJECT')
    require(re.fullmatch('sg-w3-[a-z0-9-]{8,64}', str(project)) and variables.get('W3_CANDIDATE_SHA') == candidate, 'foreign runtime project/candidate')
    for role, key in [('monolith', 'W3_MONOLITH_IMAGE'), ('api-gateway', 'W3_API_GATEWAY_IMAGE'), ('transcoder', 'W3_TRANSCODER_IMAGE')]:
        require(variables.get(key) == (manifest['images'][role].get('daemonImageID') or manifest['images'][role]['imageID']), 'runtime image differs from original accepted image')
    environment = {key: value for key, value in os.environ.items() if not key.startswith(('W3_', 'SEPOLIA_', 'STREAMGATE_', 'WEB3_', 'COMPOSE_'))}
    environment.update(variables)
    config = json.loads(command(['docker', 'compose', '-p', project, '-f', str(directory / 'compose.yml'), '--env-file', '/dev/null', 'config', '--format', 'json'], environment))
    expected_services = {'postgres', 'redis', 'minio', 'nats', 'consul', 'monolith', 'api-gateway', 'transcoder', 'monolith-h5', 'gateway-h5'}
    require(set(config['services']) == expected_services and all('build' not in service for service in config['services'].values()), 'unexpected public stack services or build')
    expected_h5 = {path.removeprefix('h5-demo/'): hashlib.sha256(source_bytes(path)).hexdigest() for path in source_h5_paths}
    require(expected_h5 and preparation['sourceH5SHA256'] == expected_h5, 'preparation omitted or changed candidate H5 files')
    actual_h5 = {}
    frontend = Path(variables['W3_H5_DIR']).resolve()
    require(frontend == (directory / 'h5').resolve(), 'frontend points outside prepared original image extraction')
    for path in frontend.rglob('*'):
        require(not path.is_symlink(), 'frontend contains a symlink')
        if path.is_file(): actual_h5[str(path.relative_to(frontend))] = hashlib.sha256(path.read_bytes()).hexdigest()
    require(actual_h5 == expected_h5 and all(actual_h5[path] == hashlib.sha256(source_bytes('h5-demo/' + path)).hexdigest() for path in actual_h5), 'served H5 differs from accepted candidate')
    for service in config['services'].values():
        require(all(port.get('host_ip') == '127.0.0.1' for port in service.get('ports', [])), 'prepared service publishes a non-loopback port')
    for name, mode in [('monolith-h5', 'monolith'), ('gateway-h5', 'api-gateway')]:
        mounts = {mount['target']: mount for mount in config['services'][name].get('volumes', [])}
        targets = {'/usr/share/nginx/html': frontend, '/etc/nginx/nginx.conf': directory / (mode + '.nginx.conf'),
                   '/testnet.config.json': directory / 'testnet.config.json', '/fixture-build': directory / 'fixture-build'}
        require(set(mounts) == set(targets), 'prepared public proxy has missing or extra mounts')
        require(all(Path(mounts[target]['source']).resolve() == path.resolve() and mounts[target].get('read_only') is True
                    for target, path in targets.items()), 'prepared proxy points outside original same-C files')
    return dict(variables=variables, environment=environment, config=config, project=project, frontend=frontend, services=expected_services)


def inspect_stack(directory, candidate, capsule, source_bytes, source_h5_paths, proxy_config, original_ci):
    prepared = inspect_prepared(directory, candidate, capsule, source_bytes, source_h5_paths, proxy_config, original_ci)
    config, project, frontend, expected_services = (prepared[key] for key in ('config', 'project', 'frontend', 'services'))
    ids = command(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project=' + project]).decode().split()
    require(len(ids) == len(expected_services), 'running public project has missing or extra containers')
    containers = json.loads(command(['docker', 'inspect', *ids])); seen = set(); identities = {}
    for item in containers:
        labels = item['Config']['Labels']; service = labels.get('com.docker.compose.service')
        require(service in expected_services and service not in seen and labels.get('com.streamgate.w3.candidate') == candidate and
                labels.get('com.streamgate.w3.testnet') == project and item['State']['Running'], 'foreign, duplicated or stopped public stack container')
        seen.add(service); expected = config['services'][service]
        image = json.loads(command(['docker', 'image', 'inspect', '--platform', 'linux/amd64', expected['image']]))[0]
        require(item['Image'] == image['Id'], 'running public container is from another image')
        if expected.get('healthcheck'):
            require(item['State'].get('Health', {}).get('Status') == 'healthy', 'required public service is not healthy')
        actual_env = dict(value.split('=', 1) for value in item['Config'].get('Env', []) if '=' in value)
        expected_env = {key: str(value) for key, value in expected.get('environment', {}).items()}
        require(all(actual_env.get(key) == value for key, value in expected_env.items()), 'running public environment differs from prepared profile')
        require(not any(key.startswith(('STREAMGATE_', 'WEB3_', 'SEPOLIA_')) and key not in expected_env for key in actual_env), 'undeclared public application override')
        expected_networks = {config['networks'][name]['name'] for name in expected.get('networks', {})}
        require(set(item['NetworkSettings']['Networks']) == expected_networks, 'public service has missing or unexpected network access')
        expected_ports = {(str(port['target']) + '/' + port.get('protocol', 'tcp'), port['host_ip'], str(port['published']))
                          for port in expected.get('ports', [])}

        def published(bindings):
            result = set()
            for target, entries in (bindings or {}).items():
                for entry in entries or []:
                    require(entry.get('HostIp') == '127.0.0.1', 'public stack published a non-loopback port')
                    result.add((target, entry['HostIp'], entry['HostPort']))
            return result

        # HostConfig records intent even when an internal-only network leaves
        # the requested port unpublished. Both intent and live bindings matter.
        require(published(item['HostConfig'].get('PortBindings')) == expected_ports and
                published(item['NetworkSettings'].get('Ports')) == expected_ports,
                'public stack requested or live port bindings differ from the prepared profile')
        if service in ('monolith', 'api-gateway', 'transcoder'):
            require(not any(mount.get('Destination', '').startswith('/app') for mount in item.get('Mounts', [])), 'runtime mount replaced accepted app/config bytes')
            require(item['HostConfig'].get('NanoCpus') == 1_000_000_000 and item['HostConfig'].get('Memory') == 1024**3, 'public application resource budget changed')
            identities[service] = dict(containerID=item['Id'], imageID=item['Image'],
                startedAtMs=int(datetime.fromisoformat(item['State']['StartedAt'].replace('Z', '+00:00')).timestamp() * 1000))
    for service in ('monolith-h5', 'gateway-h5'):
        item = next(item for item in containers if item['Config']['Labels']['com.docker.compose.service'] == service)
        mounts = {mount['Destination']: mount for mount in item.get('Mounts', [])}
        require(Path(mounts['/usr/share/nginx/html']['Source']).resolve() == frontend and mounts['/usr/share/nginx/html']['RW'] is False,
                'public proxy is serving a different frontend')
        mode = 'monolith' if service == 'monolith-h5' else 'api-gateway'
        for target, relative in [('/etc/nginx/nginx.conf', mode + '.nginx.conf'), ('/testnet.config.json', 'testnet.config.json'), ('/fixture-build', 'fixture-build')]:
            require(Path(mounts[target]['Source']).resolve() == (directory / relative).resolve() and mounts[target]['RW'] is False,
                    'public proxy replaced prepared configuration or original contract bundle')
    return identities
