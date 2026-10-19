from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import MagicMock, patch

sys.path.insert(0, str(Path(__file__).parent))
from perf_metrics import snapshot


class MetricsTests(unittest.TestCase):
    def test_retains_actual_response(self):
        body = b"process_cpu_seconds_total 2\nprocess_resident_memory_bytes 300\n"
        response = MagicMock(); response.__enter__.return_value.read.return_value = body
        with tempfile.TemporaryDirectory() as root, patch("perf_metrics.urllib.request.urlopen", return_value=response):
            output = Path(root) / "metrics.prom"; snapshot("http://monolith:8080/metrics", output)
            self.assertEqual(output.read_bytes(), body)

    def test_missing_or_truncated_metrics_fail(self):
        for body in [b"", b"# HELP process_cpu_seconds_total empty\n", b"process_cpu_seconds_total 2\nprocess_resident_memory_bytes 3"]:
            response = MagicMock(); response.__enter__.return_value.read.return_value = body
            with tempfile.TemporaryDirectory() as root, patch("perf_metrics.urllib.request.urlopen", return_value=response):
                with self.assertRaises(ValueError): snapshot("http://monolith:8080/metrics", Path(root) / "metrics.prom")

    def test_auth_is_a_header_not_a_result(self):
        body = b"process_cpu_seconds_total 2\nprocess_resident_memory_bytes 300\n"
        response = MagicMock(); response.__enter__.return_value.read.return_value = body
        with tempfile.TemporaryDirectory() as root, patch.dict("os.environ", {"STREAMGATE_METRICS_TOKEN": "private-test-token"}), patch("perf_metrics.urllib.request.urlopen", return_value=response) as request:
            output = Path(root) / "metrics.prom"; snapshot("http://monolith:8080/metrics", output)
            self.assertEqual(request.call_args.args[0].get_header("Authorization"), "Bearer private-test-token")
            self.assertNotIn(b"private-test-token", output.read_bytes())

    def test_definitions_and_invalid_samples_are_not_observations(self):
        bodies = [
            b"# HELP process_cpu_seconds_total CPU\n# HELP process_resident_memory_bytes RSS\n",
            b"process_cpu_seconds_total NaN\nprocess_resident_memory_bytes 300\n",
            b"process_cpu_seconds_total -1\nprocess_resident_memory_bytes 300\n",
            b"process_cpu_seconds_total 1\nprocess_resident_memory_bytes +Inf\n",
            b"process_cpu_seconds_total 1\nprocess_resident_memory_bytes 0\n",
            b"process_cpu_seconds_total invalid\nprocess_resident_memory_bytes 300\n",
            b"process_cpu_seconds_total 1\nprocess_cpu_seconds_total 2\nprocess_resident_memory_bytes 300\n",
        ]
        for body in bodies:
            with self.subTest(body=body):
                response = MagicMock(); response.__enter__.return_value.read.return_value = body
                with tempfile.TemporaryDirectory() as root, patch("perf_metrics.urllib.request.urlopen", return_value=response):
                    with self.assertRaises(ValueError): snapshot("http://monolith:8080/metrics", Path(root) / "metrics.prom")


if __name__ == "__main__": unittest.main()
