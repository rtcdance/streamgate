from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).parent))
from resource_monitor import normalize, summarize, validate_identities


class ResourceTests(unittest.TestCase):
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
