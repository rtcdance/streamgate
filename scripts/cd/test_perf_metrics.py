import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import MagicMock, patch

sys.path.insert(0, str(Path(__file__).parent))
from perf_metrics import capture_failover_delta, failover_totals, metric_total, snapshot, validate_scenario_metrics


def prom(*lines):
    return ("\n".join(lines) + "\n").encode()


BASE = prom(
    "process_cpu_seconds_total 2", "process_resident_memory_bytes 300",
    "streamgate_streaming_cache_hits_total 5", 'streamgate_streaming_cache_hits_total{cache="manifest"} 3',
    "streamgate_streaming_manifests_total 10", "streamgate_streaming_segments_total 20",
    "streamgate_rpc_failover_total 0", "streamgate_transcoding_queue_depth 0",
    "streamgate_transcoding_rtf_count 4", "streamgate_transcoding_rtf_sum 2",
    "streamgate_rpc_latency_seconds_count 6", "streamgate_rpc_latency_seconds_sum 1",
    "streamgate_media_bytes_total 1000", "streamgate_transcoding_duration_seconds_count 4", "streamgate_transcoding_duration_seconds_sum 8",
)


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

    def test_metric_total_sums_series_and_ignores_similar_names(self):
        body = prom('streamgate_streaming_cache_hits_total{cache="manifest"} 2',
                    'streamgate_streaming_cache_hits_total{cache="segment_index"} 3',
                    "streamgate_streaming_cache_hits_total_extra 99")
        self.assertEqual(metric_total(body, "streamgate_streaming_cache_hits_total"), 5.0)
        self.assertEqual(metric_total(body, "streamgate_streaming_cache_hits_total", {"cache": "manifest"}), 2.0)
        self.assertEqual(metric_total(body, "streamgate_streaming_cache_hits_total", {"cache": "absent"}), 0.0)
        self.assertEqual(metric_total(body, "streamgate_streaming_missing_total"), 0.0)
        with self.assertRaises(ValueError): metric_total(prom("streamgate_streaming_cache_hits_total NaN"), "streamgate_streaming_cache_hits_total")
        with self.assertRaises(ValueError): metric_total(prom("streamgate_streaming_cache_hits_total -1"), "streamgate_streaming_cache_hits_total")

    def snapshots(self, scenario):
        before = BASE
        after = BASE.replace(b"process_cpu_seconds_total 2", b"process_cpu_seconds_total 3")
        after = after.replace(b"streamgate_rpc_latency_seconds_count 6", b"streamgate_rpc_latency_seconds_count 12")
        after = after.replace(b"streamgate_rpc_latency_seconds_sum 1", b"streamgate_rpc_latency_seconds_sum 2")
        if scenario != "rpc_all_down":
            after = after.replace(b'cache="manifest"} 3', b'cache="manifest"} 9')
            after = after.replace(b"streamgate_streaming_manifests_total 10", b"streamgate_streaming_manifests_total 20")
            after = after.replace(b"streamgate_media_bytes_total 1000", b"streamgate_media_bytes_total 2000")
        if scenario == "rpc_primary_down":
            after = after.replace(b"streamgate_rpc_failover_total 0", b"streamgate_rpc_failover_total 1")
        if scenario == "cold_cache":
            before = before.replace(b'streamgate_streaming_cache_hits_total 5', b'streamgate_streaming_cache_hits_total 0').replace(b'cache="manifest"} 3', b'cache="manifest"} 0')
            before = before.replace(b"streamgate_streaming_manifests_total 10", b"streamgate_streaming_manifests_total 0").replace(b"streamgate_media_bytes_total 1000", b"streamgate_media_bytes_total 0")
        return before, after

    def test_scenario_metrics_accept_domain_consistent_snapshots(self):
        for scenario in ("warm_cache", "cold_cache", "rpc_primary_down", "rpc_all_down"):
            before, after = self.snapshots(scenario)
            with self.subTest(scenario=scenario):
                validate_scenario_metrics(before, after, before, after, scenario)

    def test_warm_cache_without_hits_fails(self):
        before, after = self.snapshots("warm_cache")
        after = after.replace(b'cache="manifest"} 9', b'cache="manifest"} 3')
        with self.assertRaises(ValueError): validate_scenario_metrics(before, after, before, after, "warm_cache")

    def test_cold_cache_hit_dominated_regime_fails(self):
        before, after = self.snapshots("cold_cache")
        after = after.replace(b'cache="manifest"} 9', b'cache="manifest"} 19')
        with self.assertRaises(ValueError): validate_scenario_metrics(before, after, before, after, "cold_cache")

    def test_failover_counter_contradicting_the_scenario_fails(self):
        before, _ = self.snapshots("rpc_primary_down")
        with self.assertRaises(ValueError): validate_scenario_metrics(before, BASE, before, BASE, "rpc_primary_down")
        after_up = BASE.replace(b"streamgate_rpc_failover_total 0", b"streamgate_rpc_failover_total 1")
        with self.assertRaises(ValueError): validate_scenario_metrics(before, after_up, before, after_up, "rpc_all_down")

    def test_nonempty_queue_and_nonpositive_rtf_fail(self):
        before, after = self.snapshots("warm_cache")
        busy = after.replace(b"streamgate_transcoding_queue_depth 0", b"streamgate_transcoding_queue_depth 2")
        with self.assertRaises(ValueError): validate_scenario_metrics(before, busy, before, busy, "warm_cache")
        bad_rtf = after.replace(b"streamgate_transcoding_rtf_count 4", b"streamgate_transcoding_rtf_count 8").replace(b"streamgate_transcoding_rtf_sum 2", b"streamgate_transcoding_rtf_sum 1")
        with self.assertRaises(ValueError): validate_scenario_metrics(before, bad_rtf, before, bad_rtf, "warm_cache")

    def test_absent_domain_metrics_fail_instead_of_being_skipped(self):
        body = prom("process_cpu_seconds_total 2", "process_resident_memory_bytes 300")
        for scenario in ("warm_cache", "cold_cache", "rpc_all_down"):
            with self.subTest(scenario=scenario), self.assertRaises(ValueError):
                validate_scenario_metrics(body, body, body, body, scenario)

    def test_capture_failover_delta_reads_real_snapshots(self):
        before, after = self.snapshots("rpc_primary_down")
        with tempfile.TemporaryDirectory() as root:
            phase = Path(root)
            (phase / "before.api.prom").write_bytes(before)
            (phase / "after.api.prom").write_bytes(after)
            evidence = capture_failover_delta(phase)
            self.assertEqual(evidence, {"metric": "streamgate_rpc_failover_total", "before": 0.0, "after": 1.0, "delta": 1.0})
            self.assertEqual(json.loads((phase / "after.rpc-failover.json").read_text()), evidence)
        self.assertEqual(failover_totals(before, after), (0.0, 1.0, 1.0))


if __name__ == "__main__":
    unittest.main()
