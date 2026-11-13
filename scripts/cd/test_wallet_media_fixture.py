import base64
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import time
import unittest
import urllib.error
from unittest.mock import patch

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location(
    "media_fixture", Path(__file__).resolve().parents[1] / "wallet-testnet" / "media_fixture.py")
fixture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixture)

CONTROLLER = "0x" + "a1" * 20
NFT = "0x" + "b2" * 20
WALLET = "0x" + "c3" * 20


def jwt(**claims):
    def part(value):
        return base64.urlsafe_b64encode(json.dumps(value).encode()).rstrip(b"=").decode()
    return f"{part({'alg': 'ES256'})}.{part(claims)}.c2lnbmF0dXJl"


class FakeAPI:
    def __init__(self, base_url, token, statuses=("processing", "ready")):
        self.token = token
        self.statuses = list(statuses)
        self.calls = []

    def upload(self, media):
        self.calls.append("upload")
        return "up-1"

    def complete(self, upload_id):
        self.calls.append(("complete", upload_id))
        return "content-1"

    def gate(self, content_id, nft):
        self.calls.append(("gate", content_id, nft))
        return "rule-1"

    def wait_ready(self, content_id, seconds):
        return "ready"


class MediaFixtureTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.token = jwt(token_type="access", wallet_address=CONTROLLER.upper().replace("0X", "0x"),
                         chain_id=11155111, exp=int(time.time()) + 3600)
        self.token_file = self.write_private("token", self.token)
        self.manifest = self.root / "deployment-manifest.json"
        self.manifest.write_text(json.dumps(self.manifest_doc()))
        self.media = self.root / "clip.mp4"
        self.media.write_bytes(b"clip")

    def write_private(self, name, text):
        path = self.root / name
        path.write_text(text)
        os.chmod(path, 0o600)
        return path

    def manifest_doc(self, **updates):
        document = {"schemaVersion": 2, "deploymentVerified": True, "chainId": 11155111, "controller": CONTROLLER,
                    "contracts": [{"role": "wallet", "address": WALLET}, {"role": "nft", "address": NFT}]}
        document.update(updates)
        return document

    def args(self, output="report.json"):
        return fixture.argparse.Namespace(
            base_url="http://localhost:18080", controller=CONTROLLER, token_file=self.token_file,
            manifest=self.manifest, output=self.root / output, media=self.media, ready_seconds=30)

    def build(self, api=FakeAPI, output="report.json"):
        described = {"sha256": "0" * 64, "bytes": 4, "encoder": "provided"}
        with patch.object(fixture, "describe_media", return_value=described), \
                contextlib.redirect_stdout(io.StringIO()):
            code = fixture.build(self.args(output), api_factory=api)
        return code, json.loads((self.root / output).read_text())

    def test_build_seeds_upload_rule_and_ready_content_as_controller(self):
        code, report = self.build()
        self.assertEqual(code, 0)
        self.assertEqual(report["status"], "PASS")
        self.assertEqual((report["contentID"], report["ruleID"], report["nft"]), ("content-1", "rule-1", NFT))
        self.assertFalse(report["completeAcceptance"])

    def test_report_never_contains_the_access_token(self):
        _, report = self.build()
        text = json.dumps(report)
        self.assertNotIn(self.token, text)
        for segment in self.token.split("."):
            self.assertNotIn(segment, text)

    def test_session_of_another_wallet_fails_before_any_upload(self):
        self.token_file.unlink()
        self.token_file = self.write_private("token", jwt(wallet_address=WALLET, chain_id=11155111))
        calls = []

        class Recording(FakeAPI):
            def upload(self, media):
                calls.append("upload")
                return super().upload(media)

        code, report = self.build(api=Recording)
        self.assertEqual((code, report["status"]), (1, "FAIL"))
        self.assertIn("another wallet", report["reason"])
        self.assertEqual(calls, [])

    def test_session_for_another_chain_or_kind_fails(self):
        for claims, reason in (({"chain_id": 1}, "Sepolia"), ({"token_type": "playback"}, "access token")):
            with self.subTest(reason=reason), self.assertRaisesRegex(fixture.FixtureError, reason):
                fixture.bind_session({"wallet_address": CONTROLLER, **claims}, CONTROLLER)

    def test_expiring_session_is_blocked_not_failed(self):
        with self.assertRaisesRegex(fixture.BlockedError, "sign in again"):
            fixture.bind_session({"wallet_address": CONTROLLER, "exp": time.time() + 10}, CONTROLLER)

    @unittest.skipUnless(os.name == "posix", "POSIX file modes")
    def test_readable_token_file_is_refused(self):
        os.chmod(self.token_file, 0o644)
        with self.assertRaisesRegex(fixture.BlockedError, "0600"):
            fixture.read_token(self.token_file)

    def test_token_file_must_hold_a_jwt_and_may_carry_a_bearer_prefix(self):
        self.assertEqual(fixture.read_token(self.write_private("bearer", "Bearer " + self.token + "\n")), self.token)
        with self.assertRaisesRegex(fixture.BlockedError, "JWT"):
            fixture.read_token(self.write_private("junk", "not-a-token"))

    def test_manifest_must_match_network_controller_and_one_nft(self):
        cases = (
            ({"chainId": 31337}, "another network"),
            ({"controller": WALLET}, "another network or controller"),
            ({"deploymentVerified": False}, "schema-v2"),
            ({"contracts": [{"role": "nft", "address": NFT}, {"role": "nft", "address": WALLET}]}, "exactly one NFT"),
        )
        for updates, reason in cases:
            with self.subTest(reason=reason):
                self.manifest.write_text(json.dumps(self.manifest_doc(**updates)))
                with self.assertRaisesRegex(fixture.FixtureError, reason):
                    fixture.manifest_nft(self.manifest, CONTROLLER)
        self.manifest.write_text(json.dumps(self.manifest_doc()))
        self.assertEqual(fixture.manifest_nft(self.manifest, CONTROLLER), NFT)

    def test_gating_rule_binds_the_recorded_nft_on_sepolia(self):
        sent = []

        def opener(request, timeout):
            sent.append((request.get_method(), request.full_url, json.loads(request.data)))
            return io.BytesIO(json.dumps({"id": "rule-9"}).encode())

        api = fixture.API("http://localhost:18080", self.token, opener=self.wrap(opener))
        self.assertEqual(api.gate("content-1", NFT), "rule-9")
        method, url, body = sent[0]
        self.assertEqual((method, url), ("POST", "http://localhost:18080/api/v1/content/content-1/gating-rules"))
        self.assertEqual(body, {"contract_address": NFT, "chain_id": 11155111, "standard": "erc721", "min_balance": 1})

    def wrap(self, opener):
        class Response(io.BytesIO):
            status = 200

            def __enter__(self):
                return self

            def __exit__(self, *exc):
                return False

        def call(request, timeout):
            return Response(opener(request, timeout).getvalue())
        return call

    def test_refused_request_fails_and_unavailable_dependency_blocks(self):
        for code, error in ((401, fixture.FixtureError), (403, fixture.FixtureError), (503, fixture.BlockedError)):
            def opener(request, timeout, code=code):
                raise urllib.error.HTTPError(request.full_url, code, "x", {}, io.BytesIO(b"denied"))
            with self.subTest(code=code), self.assertRaises(error):
                fixture.API("http://localhost:18080", self.token, opener=opener).complete("up-1")

    def test_wait_ready_distinguishes_ready_failed_and_timeout(self):
        def api(statuses):
            sequence = iter(statuses)
            client = fixture.API("http://localhost:18080", self.token)
            client.call = lambda method, path, body=None, content_type=None: (200, {"status": next(sequence)})
            return client

        self.assertEqual(api(["processing", "ready"]).wait_ready("c", 30, sleep=lambda s: None), "ready")
        with self.assertRaisesRegex(fixture.FixtureError, "failed"):
            api(["processing", "failed"]).wait_ready("c", 30, sleep=lambda s: None)
        clock = iter([0, 10, 40]).__next__
        with self.assertRaisesRegex(fixture.BlockedError, "after 30s"):
            api(["processing", "processing"]).wait_ready("c", 30, sleep=lambda s: None, clock=clock)

    def test_upload_requires_a_completed_upload(self):
        api = fixture.API("http://localhost:18080", self.token)
        api.call = lambda *a, **k: (200, {"upload_id": "u", "status": "pending"})
        with self.assertRaisesRegex(fixture.FixtureError, "did not complete"):
            api.upload(self.media)

    def test_base_url_must_be_an_origin(self):
        with self.assertRaises(fixture.FixtureError):
            fixture.API("http://localhost:18080/api/v1", self.token)

    def test_existing_output_is_refused_before_any_work(self):
        (self.root / "report.json").write_text("{}")
        argv = ["--base-url", "http://localhost:18080", "--controller", CONTROLLER, "--token-file", str(self.token_file),
                "--manifest", str(self.manifest), "--output", str(self.root / "report.json")]
        with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
            fixture.main(argv)
        self.assertEqual((self.root / "report.json").read_text(), "{}")

    def test_missing_ffmpeg_is_blocked(self):
        with patch.object(fixture.subprocess, "run", side_effect=FileNotFoundError("ffmpeg")):
            with self.assertRaisesRegex(fixture.BlockedError, "no skip by design"):
                fixture.encoder()


if __name__ == "__main__":
    unittest.main()
