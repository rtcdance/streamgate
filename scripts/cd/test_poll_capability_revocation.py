#!/usr/bin/env python3
"""Exercise the private, monotonic HTTP capability-revocation probe."""
from contextlib import redirect_stdout
from http.server import BaseHTTPRequestHandler, HTTPServer
import io
from pathlib import Path
import tempfile
import threading
import unittest
from unittest.mock import patch

import poll_capability_revocation as probe


class _Handler(BaseHTTPRequestHandler):
    status = 403
    seen_range = None

    def do_GET(self):
        type(self).seen_range = self.headers.get("Range")
        self.send_response(type(self).status)
        self.end_headers()
        self.wfile.write(b"x")

    def log_message(self, *_args):
        pass


class PollCapabilityRevocationTests(unittest.TestCase):
    def setUp(self):
        self.server = HTTPServer(("127.0.0.1", 0), _Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.thread.join, 2)
        self.addCleanup(self.server.shutdown)
        self.url = f"http://127.0.0.1:{self.server.server_port}/segment.ts?playback_token=private-test-token"

    def _private_file(self, url):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        path = Path(temp.name) / "capability-url"
        path.write_text(url)
        path.chmod(0o600)
        return path

    def test_real_403_revocation_deletes_secret_file_and_redacts_output(self):
        _Handler.status = 403
        path = self._private_file(self.url)
        output = io.StringIO()
        with patch("sys.argv", ["probe", str(path), "--deadline", "2"]), redirect_stdout(output):
            self.assertEqual(probe.main(), 0)
        self.assertFalse(path.exists())
        self.assertEqual(_Handler.seen_range, "bytes=0-0")
        self.assertIn("M3_HTTP_TRANSFER_70S_PASS", output.getvalue())
        self.assertNotIn("private-test-token", output.getvalue())

    def test_http_200_does_not_pass_before_deadline(self):
        _Handler.status = 200
        path = self._private_file(self.url)
        with patch("sys.argv", ["probe", str(path), "--deadline", "0.3"]), redirect_stdout(io.StringIO()):
            with self.assertRaisesRegex(SystemExit, "remained accepted or unverifiable"):
                probe.main()
        self.assertFalse(path.exists())

    def test_non_private_file_permissions_are_rejected_and_secret_removed(self):
        path = self._private_file(self.url)
        path.chmod(0o644)
        with patch("sys.argv", ["probe", str(path)]):
            with self.assertRaisesRegex(SystemExit, "permissions must be 0600"):
                probe.main()
        self.assertTrue(path.exists(), "do not read or delete a credential file with unsafe permissions")

    def test_deadline_is_bounded_by_derived_maximum(self):
        with self.assertRaisesRegex(ValueError, "\(0, 70\]"):
            probe.poll_revocation(self.url, 71)


if __name__ == "__main__":
    unittest.main()
