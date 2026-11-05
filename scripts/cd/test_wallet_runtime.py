"""Docker-inspect schema controls; these fixtures never establish a live stack."""
import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import wallet_runtime as gate

CANDIDATE = 'a' * 40
PROJECT = 'sg-w3-aaaaaaaa-test0001'
SERVICES = ('postgres', 'redis', 'minio', 'nats', 'consul', 'monolith', 'api-gateway', 'transcoder', 'monolith-h5', 'gateway-h5')


class RuntimeIdentityTests(unittest.TestCase):
    def run_fixture(self, mutation=None):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); directory = root / 'prepared'; capsule = root / 'ci'
            directory.mkdir(); capsule.mkdir(); frontend = directory / 'h5'; frontend.mkdir()
            (frontend / 'index.html').write_bytes(b'candidate H5')
            source = {'deploy/docker/compose.w3sepolia.yml': b'candidate compose', 'h5-demo/index.html': b'candidate H5'}
            (directory / 'compose.yml').write_bytes(source['deploy/docker/compose.w3sepolia.yml'])
            for mode in ('monolith', 'api-gateway'): (directory / (mode + '.nginx.conf')).write_text(mode + ' original proxy')
            manifest = dict(archiveSHA256='d' * 64, images={role: dict(imageID='sha256:' + str(index) * 64) for index, role in enumerate(('monolith', 'api-gateway', 'transcoder'), 1)})
            (capsule / 'candidate.json').write_text(json.dumps(manifest))
            original_ci = dict(jobID=123, upload=dict(bundleSHA256='e' * 64))
            preparation = dict(candidateSHA=CANDIDATE, candidateArchiveSHA256='d' * 64, originalCI=copy.deepcopy(original_ci), completeAcceptance=False,
                               sourceH5SHA256={'index.html': hashlib.sha256(b'candidate H5').hexdigest()})
            variables = dict(W3_PROJECT=PROJECT, W3_CANDIDATE_SHA=CANDIDATE, W3_H5_DIR=str(frontend),
                W3_MONOLITH_IMAGE=manifest['images']['monolith']['imageID'], W3_API_GATEWAY_IMAGE=manifest['images']['api-gateway']['imageID'],
                W3_TRANSCODER_IMAGE=manifest['images']['transcoder']['imageID'])
            config = dict(services={}, networks={name: dict(name=PROJECT + '_' + name) for name in ('data', 'rpc-egress')})
            containers = []
            for service in SERVICES:
                image = manifest['images'][service]['imageID'] if service in manifest['images'] else service + '@sha256:' + '9' * 64
                networks = ['data', 'rpc-egress'] if service in ('monolith', 'api-gateway') else ['data']
                config['services'][service] = dict(image=image, environment={'STREAMGATE_ENV': 'w3sepolia'} if service in ('monolith', 'api-gateway') else {},
                    networks={name: {} for name in networks}, healthcheck=dict(test=['CMD', 'true']))
                mounts = []
                if service in ('monolith-h5', 'gateway-h5'):
                    mode = 'monolith' if service == 'monolith-h5' else 'api-gateway'
                    mounts = [dict(Destination=target, Source=str(path), RW=False) for target, path in [
                        ('/usr/share/nginx/html', frontend), ('/etc/nginx/nginx.conf', directory / (mode + '.nginx.conf')),
                        ('/testnet.config.json', directory / 'testnet.config.json'), ('/fixture-build', directory / 'fixture-build')]]
                    config['services'][service]['volumes'] = [dict(type='bind', target=mount['Destination'], source=mount['Source'], read_only=True) for mount in mounts]
                containers.append(dict(Id=service, Image=image, Config=dict(Env=['STREAMGATE_ENV=w3sepolia'] if service in ('monolith', 'api-gateway') else [],
                    Labels={'com.docker.compose.service': service, 'com.streamgate.w3.candidate': CANDIDATE, 'com.streamgate.w3.testnet': PROJECT}),
                    State=dict(Running=True, Health=dict(Status='healthy'), StartedAt='2026-11-03T00:00:00Z'), Mounts=mounts,
                    HostConfig=dict(PortBindings={}, NanoCpus=1_000_000_000, Memory=1024**3),
                    NetworkSettings=dict(Networks={PROJECT + '_' + name: {} for name in networks}, Ports={})))
                if service in ('monolith-h5', 'gateway-h5', 'monolith', 'api-gateway'):
                    target = 8080 if service.endswith('-h5') else 9090
                    published = str(18500 + SERVICES.index(service))
                    config['services'][service]['ports'] = [dict(target=target, published=published, protocol='tcp', host_ip='127.0.0.1')]
                    binding = {str(target) + '/tcp': [dict(HostIp='127.0.0.1', HostPort=published)]}
                    containers[-1]['HostConfig']['PortBindings'] = copy.deepcopy(binding)
                    containers[-1]['NetworkSettings']['Ports'] = copy.deepcopy(binding)
            if mutation: mutation(directory, preparation, variables, config, containers)
            (directory / 'preparation.json').write_text(json.dumps(preparation))
            env = directory / 'stack.env.json'; env.write_text(json.dumps(variables)); env.chmod(0o600)

            def command(arguments, env=None):
                if arguments[:2] == ['docker', 'compose']: value = config
                elif arguments[:2] == ['docker', 'ps']: return '\n'.join(SERVICES).encode()
                elif arguments[:2] == ['docker', 'inspect']: value = containers
                elif arguments[:3] == ['docker', 'image', 'inspect']: value = [dict(Id=arguments[-1])]
                else: raise AssertionError('unexpected or mutating Docker command: ' + str(arguments))
                return json.dumps(value).encode()

            with mock.patch.object(gate, 'command', side_effect=command):
                return gate.inspect_stack(directory, CANDIDATE, capsule, source.__getitem__, ['h5-demo/index.html'], lambda mode: mode + ' original proxy', original_ci)

    def test_same_image_profile_proxy_and_full_frontend_identify_both_modes(self):
        result = self.run_fixture()
        self.assertEqual({'monolith', 'api-gateway', 'transcoder'}, set(result))
        self.assertTrue(all(value['startedAtMs'] > 0 for value in result.values()))

    def test_foreign_image_env_ports_mounts_networks_and_budget_are_refused(self):
        changes = [lambda d, p, e, c, rows: rows[5].update(Image='foreign'),
            lambda d, p, e, c, rows: rows[5]['Config']['Env'].append('WEB3_CHAIN_ID=31337'),
            lambda d, p, e, c, rows: rows[5]['HostConfig'].update(PortBindings={'8080/tcp': [dict(HostIp='0.0.0.0', HostPort='8080')]}),
            lambda d, p, e, c, rows: rows[5]['Mounts'].append(dict(Destination='/app/config', Source='/foreign')),
            lambda d, p, e, c, rows: rows[7]['NetworkSettings']['Networks'].update(unexpected={}),
            lambda d, p, e, c, rows: rows[7]['HostConfig'].update(NanoCpus=8_000_000_000),
            lambda d, p, e, c, rows: rows[5]['State']['Health'].update(Status='unhealthy'),
            lambda d, p, e, c, rows: c['services']['monolith-h5']['volumes'][0].update(source='/foreign/h5'),
            lambda d, p, e, c, rows: c['services'].update(anvil=dict(image='fake'))]
        for change in changes:
            with self.subTest(change=change), self.assertRaises(ValueError): self.run_fixture(change)

    def test_changed_original_producer_h5_or_proxy_cannot_be_self_declared_valid(self):
        changes = [lambda d, p, e, c, rows: p['originalCI'].update(jobID=999),
            lambda d, p, e, c, rows: (d / 'monolith.nginx.conf').write_text('proxy to unrelated stack'),
            lambda d, p, e, c, rows: p['sourceH5SHA256'].clear(),
            lambda d, p, e, c, rows: (d / 'h5/index.html').write_text('changed frontend'),
            lambda d, p, e, c, rows: rows[8]['Mounts'][0].update(Source='/another/h5')]
        for change in changes:
            with self.subTest(change=change), self.assertRaises(ValueError): self.run_fixture(change)

    def test_requested_ports_without_live_publication_or_with_foreign_bindings_are_refused(self):
        changes = [lambda d, p, e, c, rows: rows[8]['NetworkSettings']['Ports'].clear(),
            lambda d, p, e, c, rows: rows[9]['NetworkSettings']['Ports'].update({'8080/tcp': []}),
            lambda d, p, e, c, rows: rows[8]['NetworkSettings']['Ports']['8080/tcp'][0].update(HostPort='19999'),
            lambda d, p, e, c, rows: rows[8]['NetworkSettings']['Ports']['8080/tcp'][0].update(HostIp='0.0.0.0'),
            lambda d, p, e, c, rows: rows[0]['NetworkSettings']['Ports'].update({'5432/tcp': [dict(HostIp='127.0.0.1', HostPort='15432')]}),
            lambda d, p, e, c, rows: rows[8]['HostConfig']['PortBindings'].clear()]
        for change in changes:
            with self.subTest(change=change), self.assertRaises(ValueError): self.run_fixture(change)


if __name__ == '__main__':
    unittest.main()
