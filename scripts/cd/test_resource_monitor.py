from datetime import datetime, timezone
from pathlib import Path
from types import SimpleNamespace
import json
import sys
import tempfile
import threading
import time
import unittest
from unittest.mock import MagicMock, patch

sys.path.insert(0, str(Path(__file__).parent))
from resource_monitor import monitor, normalize, summarize, validate_identities


class ResourceTests(unittest.TestCase):
    def test_ready_marker_follows_first_valid_observation_of_every_role(self):
        inspected = {"Id": "container", "Image": "sha256:" + "a" * 64, "Config": {"Labels": {}},
                     "HostConfig": {"NanoCpus": 1, "Memory": 1000}, "RestartCount": 0,
                     "State": {"Running": True, "OOMKilled": False, "StartedAt": "one"}}
        stats = {"read": "2026-10-19T01:00:00Z", "cpu_stats": {"cpu_usage": {"total_usage": 1}},
                 "memory_stats": {"usage": 300, "stats": {"total_rss": 100}}}
        engine = MagicMock(); engine.container.side_effect = lambda identity, suffix: inspected if suffix == "json" else stats
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            args = SimpleNamespace(target=[role + "=" + role for role in ("api", "worker", "loader")], socket="unused",
                                   output=root / "stats.jsonl", stop_file=root / "stop", ready_file=root / "ready.json", timeout=5, interval=1)
            with patch("resource_monitor.Engine", return_value=engine), patch("resource_monitor.signal.signal"), patch("resource_monitor.time.sleep", side_effect=lambda _: args.stop_file.touch()):
                monitor(args)
            self.assertTrue(args.ready_file.is_file(), "sampling cannot be released before every role is observed")

    def inspected(self):
        return {"Id": "container", "Image": "sha256:" + "a" * 64, "Config": {"Labels": {}},
                "HostConfig": {"NanoCpus": 1, "Memory": 1000}, "RestartCount": 0,
                "State": {"Running": True, "OOMKilled": False, "StartedAt": "one"}}

    def timed_monitor(self, root, interval, limit, stats_for):
        calls = {}

        def container(identity, suffix):
            if suffix == "json":
                return self.inspected()
            calls[identity] = calls.get(identity, 0) + 1
            return stats_for(identity, calls[identity])

        engine = MagicMock(); engine.container.side_effect = container
        args = SimpleNamespace(target=[role + "=" + role for role in ("api", "worker", "loader")], socket="unused",
                               output=root / "stats.jsonl", stop_file=root / "stop", ready_file=root / "ready.json",
                               timeout=30, interval=interval)
        watchdog = threading.Timer(limit, args.stop_file.touch)
        watchdog.start()
        try:
            with patch("resource_monitor.Engine", return_value=engine), patch("resource_monitor.signal.signal"):
                monitor(args)
        finally:
            watchdog.join()
        return [json.loads(line) for line in args.output.read_text().splitlines()]

    def iso(self, seconds):
        return datetime.fromtimestamp(seconds, timezone.utc).isoformat().replace("+00:00", "Z")

    def test_slow_role_does_not_starve_the_other_roles_cadence(self):
        # Every one-shot stats call is slow under load (the 100-viewer scenario).
        # Sampling roles sequentially makes their cadence the SUM of all calls,
        # so the 2.5s gate that summarize enforces is broken by construction;
        # each role must be sampled on its own schedule.
        def stats_for(identity, call):
            time.sleep(0.5)
            return {"read": self.iso(time.time()), "cpu_stats": {"cpu_usage": {"total_usage": call}},
                    "memory_stats": {"usage": 300, "stats": {"total_rss": 100}}}

        with tempfile.TemporaryDirectory() as directory:
            rows = self.timed_monitor(Path(directory), 0.1, 1.9, stats_for)
        for role in ("worker", "loader"):
            stamps = sorted(row["timestamp"] for row in rows if row.get("role") == role and "error" not in row)
            self.assertGreaterEqual(len(stamps), 3, role + " was sampled too rarely")
            gaps = [b - a for a, b in zip(stamps, stamps[1:])]
            self.assertLess(max(gaps), 1.0, role + " cadence was starved by the slow api role")

    def test_repeated_daemon_reads_are_not_written_as_duplicate_samples(self):
        # The daemon can serve the same read twice; writing it verbatim creates
        # a zero-delta duplicate that the cadence gate rejects. A read that does
        # not advance is the same sample, not a new observation.
        base = time.time()

        def stats_for(identity, call):
            advance = 0 if (identity == "worker" and call <= 2) else call
            return {"read": self.iso(base + advance), "cpu_stats": {"cpu_usage": {"total_usage": call}},
                    "memory_stats": {"usage": 300, "stats": {"total_rss": 100}}}

        with tempfile.TemporaryDirectory() as directory:
            rows = self.timed_monitor(Path(directory), 0.02, 0.6, stats_for)
        stamps = [row["timestamp"] for row in rows if row.get("role") == "worker" and "error" not in row]
        self.assertGreaterEqual(len(stamps), 3, "worker was sampled too rarely")
        self.assertTrue(all(b > a for a, b in zip(stamps, stamps[1:])), "a repeated daemon read was recorded as a new sample")

    def test_cgroup_v1_cpu_and_rss(self):
        record = normalize({"cpu_stats": {"cpu_usage": {"total_usage": 2000000000}}, "memory_stats": {"usage": 300, "stats": {"total_rss": 100}}})
        self.assertEqual(record, {"cpu_nanoseconds": 2000000000, "rss_bytes": 100, "memory_usage_bytes": 300})

    def test_cgroup_v2_resident_pages(self):
        record = normalize({"cpu_stats": {"cpu_usage": {"total_usage": 0}}, "memory_stats": {"usage": 300, "stats": {"anon": 150, "file_mapped": 30}}})
        self.assertEqual(record["rss_bytes"], 180)

    def test_missing_data_fails(self):
        for stats in [{}, {"cpu_stats": {"cpu_usage": {"total_usage": 1}}, "memory_stats": {"usage": 30, "stats": {}}}]:
            with self.assertRaises(ValueError): normalize(stats)

    def samples(self):
        return [{"role": role, "containerID": role + "-id", "timestamp": t, "cpu_nanoseconds": t * 1000000000, "rss_bytes": t + 100, "memory_usage_bytes": 500} for role in ["api", "worker", "loader"] for t in range(46)]

    def test_uses_playback_window_not_login(self):
        samples = self.samples(); samples[0]["rss_bytes"] = 1000000
        result = summarize(samples, 10, 40, ["api", "worker", "loader"])
        self.assertEqual(result["api"]["cpu_seconds_delta"], 30)
        self.assertEqual(result["api"]["peak_rss_bytes"], 140)
        self.assertEqual(result["api"]["sample_count"], 31)

    def test_missing_role_or_sample_gaps_fail(self):
        for rows in [[], [s for s in self.samples() if s["role"] != "worker"], [s for s in self.samples() if not (s["role"] == "api" and 15 < s["timestamp"] < 25)]]:
            with self.assertRaises(ValueError): summarize(rows, 10, 40, ["api", "worker", "loader"])

    def test_reset_or_replaced_container_fails(self):
        for field, value in [("cpu_nanoseconds", 0), ("containerID", "other")]:
            rows = self.samples(); next(s for s in rows if s["role"] == "api" and s["timestamp"] == 20)[field] = value
            with self.assertRaises(ValueError): summarize(rows, 10, 40, ["api", "worker", "loader"])

    def test_only_outside_window_errors_can_be_ignored(self):
        rows = self.samples() + [{"role": "api", "timestamp": 1, "error": "setup not started"}]
        summarize(rows, 10, 40, ["api", "worker", "loader"])
        rows.append({"role": "api", "timestamp": 20.5, "error": "stats unavailable"})
        with self.assertRaises(ValueError): summarize(rows, 10, 40, ["api", "worker", "loader"])

    def test_incomplete_window_fails(self):
        rows = [s for s in self.samples() if 15 <= s["timestamp"] <= 35]
        with self.assertRaises(ValueError): summarize(rows, 10, 40, ["api", "worker", "loader"])

    def identities(self):
        config = {"api": {"nanoCPUs": 2, "memoryBytes": 20}, "worker": {"nanoCPUs": 2, "memoryBytes": 10}, "loader": {"nanoCPUs": 1, "memoryBytes": 5}}
        runtime = {"Running": True, "OOMKilled": False, "StartedAt": "one", "RestartCount": 0}
        identities = {role: {**budget, "imageID": "sha256:" + "a" * 64, "revision": "harness" if role == "loader" else "target", "project": "project", "run": "run", "runtimeBefore": dict(runtime), "runtimeAfter": dict(runtime)} for role, budget in config.items()}
        return identities, {"commit": "target", "harness_revision": "harness"}, config

    def test_exact_runtime_limits_and_revision(self):
        identities, playback, config = self.identities(); validate_identities(identities, playback, config)
        for key, value in [("nanoCPUs", 0), ("memoryBytes", 0), ("revision", "other"), ("project", "foreign"), ("run", "foreign")]:
            records, playback, config = self.identities(); records["loader"][key] = value
            with self.assertRaises(ValueError): validate_identities(records, playback, config)

    def test_core_process_restart_fails(self):
        for key, value in [("Running", False), ("OOMKilled", True), ("StartedAt", "different"), ("RestartCount", 1)]:
            records, playback, config = self.identities(); records["api"]["runtimeAfter"][key] = value
            with self.assertRaises(ValueError): validate_identities(records, playback, config)


if __name__ == "__main__": unittest.main()
