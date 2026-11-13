import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
REQUIRED = ('W3_PROJECT', 'W3_CANDIDATE_SHA', 'W3_JWT_SECRET', 'SEPOLIA_RPC_URL', 'SEPOLIA_RPC_FALLBACK_URL',
            'W3_MONOLITH_IMAGE', 'W3_API_GATEWAY_IMAGE', 'W3_TRANSCODER_IMAGE', 'W3_H5_DIR',
            'W3_MONOLITH_NGINX_CONFIG', 'W3_GATEWAY_NGINX_CONFIG', 'W3_CONTRACT_BUNDLE_DIR', 'W3_TESTNET_CONFIG')


class SepoliaComposeContract(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.directory = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.directory.cleanup)
        cls.env = dict(os.environ, W3_PROJECT='sg-w3-config-only', W3_CANDIDATE_SHA='a' * 40,
            W3_JWT_SECRET='unit-only-sepolia-stack-secret-at-least-32', SEPOLIA_RPC_URL='https://primary.example', SEPOLIA_RPC_FALLBACK_URL='https://fallback.example',
            W3_MONOLITH_IMAGE='sha256:' + '1' * 64, W3_API_GATEWAY_IMAGE='sha256:' + '2' * 64, W3_TRANSCODER_IMAGE='sha256:' + '3' * 64,
            W3_H5_DIR=cls.directory.name, W3_MONOLITH_NGINX_CONFIG=cls.directory.name + '/mono.conf', W3_GATEWAY_NGINX_CONFIG=cls.directory.name + '/gateway.conf',
            W3_CONTRACT_BUNDLE_DIR=cls.directory.name + '/bundle', W3_TESTNET_CONFIG=cls.directory.name + '/testnet.config.json', VERIFY_MINIO_IMAGE='')
        result = cls.config(cls.env)
        if result.returncode: raise AssertionError(result.stderr)
        cls.value = json.loads(result.stdout)

    @staticmethod
    def config(env, file='compose.w3sepolia.yml'):
        # config is client-only: this does not start services or contact RPCs.
        return subprocess.run(['docker', 'compose', '-p', 'sg-w3-config-only', '-f', str(ROOT / 'deploy/docker' / file), 'config', '--format', 'json'],
                              env=env, capture_output=True, text=True, timeout=15)

    def test_exact_local_stack_no_build_anvil_or_external_storage_ports(self):
        services = self.value['services']
        self.assertEqual({'monolith', 'api-gateway', 'transcoder', 'postgres', 'redis', 'minio', 'nats', 'consul', 'monolith-h5', 'gateway-h5'}, set(services))
        for name, service in services.items():
            with self.subTest(service=name):
                self.assertNotIn('build', service)
                self.assertNotIn('com.streamgate.verify.run', service['labels'])
                self.assertEqual('a' * 40, service['labels']['com.streamgate.w3.candidate'])
                self.assertLessEqual(float(service['cpus']), 1)
                self.assertTrue(all(port['host_ip'] == '127.0.0.1' for port in service.get('ports', [])))
                if name in ('postgres', 'redis', 'minio', 'nats', 'consul', 'transcoder'): self.assertFalse(service.get('ports'))
        self.assertTrue(self.value['networks']['data']['internal'])

    def test_only_api_processes_receive_sepolia_access_and_distinct_page_binding(self):
        domains = set()
        for role in ('monolith', 'api-gateway'):
            service = self.value['services'][role]; env = service['environment']
            self.assertEqual({'data', 'rpc-egress'}, set(service['networks']))
            self.assertEqual('11155111', env['WEB3_CHAIN_ID'])
            self.assertEqual('safe', env['WEB3_BLOCK_TAG'])
            self.assertEqual('w3sepolia', env['STREAMGATE_ENV'])
            for key in ('STREAMGATE_ANVIL_DEPLOYER_KEY', 'STREAMGATE_PRIVATE_KEY_HEX', 'CONTENT_REGISTRY_ADDRESS', 'STREAMGATE_CONTENT_REGISTRY_ADDRESS'): self.assertEqual('', env[key])
            self.assertEqual('false', env['FEATURES_DEMO_TX_SIGNER'])
            domain = env['STREAMGATE_SIWE_DOMAIN']; domains.add(domain)
            self.assertEqual('http://' + domain + '/login', env['STREAMGATE_SIWE_URI'])
        self.assertEqual({'localhost:18500', 'localhost:18501'}, domains)
        worker = self.value['services']['transcoder']
        self.assertEqual({'data'}, set(worker['networks']))
        self.assertEqual('w3sepolia-worker', worker['environment']['STREAMGATE_ENV'])
        for key in worker['environment']:
            self.assertFalse(any(value in key for value in ('JWT', 'SEPOLIA', 'ETH_RPC', 'DEPLOYER', 'PRIVATE_KEY')))
        self.assertTrue(worker['read_only']); self.assertEqual(['ALL'], worker['cap_drop'])

    def test_h5_is_external_candidate_extraction_not_current_checkout(self):
        for role in ('monolith-h5', 'gateway-h5'):
            service = self.value['services'][role]
            mounts = {mount['target']: mount for mount in service['volumes']}
            self.assertEqual(self.directory.name, mounts['/usr/share/nginx/html']['source'])
            self.assertTrue(mounts['/usr/share/nginx/html']['read_only'])
            self.assertNotEqual(str(ROOT / 'h5-demo'), mounts['/usr/share/nginx/html']['source'])
            self.assertTrue(mounts['/fixture-build']['read_only'])
            self.assertTrue(mounts['/testnet.config.json']['read_only'])

    def test_runtime_dependency_pins_match_the_existing_accepted_stack(self):
        reference = self.config(self.env, 'compose.verify.yml')
        self.assertEqual(0, reference.returncode, reference.stderr)
        reference = json.loads(reference.stdout)['services']
        for role in ('postgres', 'redis', 'minio', 'nats', 'consul'):
            self.assertEqual(reference[role]['image'], self.value['services'][role]['image'])

    def test_missing_required_identity_or_network_input_is_not_silently_defaulted(self):
        for key in REQUIRED:
            env = dict(self.env); env.pop(key)
            with self.subTest(missing=key):
                result = self.config(env)
                self.assertNotEqual(0, result.returncode)
                self.assertIn(key, result.stderr)


if __name__ == '__main__':
    unittest.main()
