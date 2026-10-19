import copy
import hashlib
import json
import sys
from pathlib import Path
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).parent))
from perf_protocol import expected_matrix, validate_layout, validate_result, compare_rounds, validate_directory, read_json
from resource_monitor import summarize, timestamp


class ProtocolTests(unittest.TestCase):
    def record(self, scenario="warm_cache", viewers=1):
        denied = scenario == "rpc_all_down"
        hashes = {"/api/v1/streaming/11111111-1111-4111-8111-111111111111/segment/segment_000.ts": "e" * 64}
        media_digest = hashlib.sha256(json.dumps(hashes, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
        return {"commit": "a" * 40, "harness_revision": "b" * 40, "scenario": scenario,
                "fixture_hash": "c" * 64, "media_hashes_sha256": media_digest,
                "content_id": "11111111-1111-4111-8111-111111111111", "quality": "640x360",
                "pace_by_extinf": True, "warmup_sec": 10, "duration_sec": 30,
                "started_at": "2026-10-19T01:00:00Z", "finished_at": "2026-10-19T01:00:30Z",
                "wallet_pool_size": viewers, "login_paced": True, "logins_failed": 0, "viewers": viewers,
                "success_count": 0 if denied else viewers * 2, "error_count": viewers if denied else 0,
                "aborted_count": 0, "total_bytes": 0 if denied else viewers * 100,
                "verified_media_count": 0 if denied else viewers, "media_hash_error_count": 0,
                "viewer_samples": [{"viewer_id": i, "success_count": 0 if denied else 2,
                                    "error_count": 1 if denied else 0, "aborted_count": 0,
                                    "verified_media_count": 0 if denied else 1} for i in range(viewers)],
                "http_statuses": {"503" if denied else "200": viewers if denied else viewers * 2},
                "ttfb_ms": {"p50": 0 if denied else 1, "p95": 0 if denied else 2, "p99": 0 if denied else 3}}

    def test_exact_full_matrix(self):
        expected = expected_matrix()
        self.assertEqual(len(expected), 48)
        validate_layout(set(expected), "matrix")
        for missing in expected:
            with self.assertRaises(ValueError): validate_layout(set(expected) - {missing}, "matrix")

    def test_extra_round_and_stale_root_fail(self):
        for extra in ["warm_cache-viewers-1.json", "round-4/warm_cache-viewers-1.json", "round-1/cold_cache/cold_cache-viewers-2.json"]:
            with self.assertRaises(ValueError): validate_layout(set(expected_matrix()) | {extra}, "matrix")

    def test_comparison_requires_exact_three_rounds(self):
        paths = {f"round-{i}/warm_cache-viewers-50.json" for i in range(1, 4)}
        validate_layout(paths, "comparison")
        for rows in [set(), paths - {"round-2/warm_cache-viewers-50.json"}, paths | {"round-4/warm_cache-viewers-50.json"}]:
            with self.assertRaises(ValueError): validate_layout(rows, "comparison")

    def test_valid_record_including_total_denial(self):
        for scenario in ["warm_cache", "cold_cache", "rpc_primary_down", "rpc_all_down"]:
            validate_result(self.record(scenario), "a" * 40, "b" * 40)

    def test_missing_viewer_and_zero_completed_fail(self):
        for mutation in [lambda r: r["viewer_samples"].clear(), lambda r: r["viewer_samples"][0].update(success_count=0)]:
            record = self.record(); mutation(record)
            with self.assertRaises(ValueError): validate_result(record, "a" * 40, "b" * 40)

    def test_identity_protocol_and_counts_fail_closed(self):
        for key, value in [("commit", "e" * 40), ("harness_revision", "e" * 40), ("quality", ""),
                           ("fixture_hash", ""), ("media_hashes_sha256", ""), ("pace_by_extinf", False),
                           ("warmup_sec", 0), ("duration_sec", 1), ("wallet_pool_size", 0),
                           ("login_paced", False), ("logins_failed", 1), ("success_count", 0),
                           ("verified_media_count", 0), ("media_hash_error_count", 1), ("http_statuses", {}), ("finished_at", "2026-10-19T01:00:01Z")]:
            with self.subTest(key=key):
                record = self.record(); record[key] = value
                with self.assertRaises(ValueError): validate_result(record, "a" * 40, "b" * 40)

    def test_auth_failures_remain_in_denominator(self):
        record = self.record(viewers=50)
        record["error_count"] = 2; record["viewer_samples"][0]["error_count"] = 2; record["http_statuses"]["401"] = 2
        with self.assertRaises(ValueError): validate_result(record, "a" * 40, "b" * 40)

    def test_all_down_cannot_serve_bytes(self):
        record = self.record("rpc_all_down"); record["total_bytes"] = 1
        with self.assertRaises(ValueError): validate_result(record, "a" * 40, "b" * 40)

    def test_capacity_requires_observable_overload(self):
        record = self.record(viewers=100)
        record["error_count"] = 20; record["viewer_samples"][0]["error_count"] = 20; record["http_statuses"]["429"] = 20
        validate_result(record, "a" * 40, "b" * 40)
        record["http_statuses"]["401"] = record["http_statuses"].pop("429")
        with self.assertRaises(ValueError): validate_result(record, "a" * 40, "b" * 40)

    def test_three_round_median_limit(self):
        reference = [self.record(viewers=50) for _ in range(3)]
        candidate = copy.deepcopy(reference)
        for row in candidate: row["ttfb_ms"]["p95"] = 2.4
        self.assertEqual(compare_rounds(reference, candidate)["ratio"], 1.2)
        candidate[0]["ttfb_ms"]["p95"] = 2.5; candidate[1]["ttfb_ms"]["p95"] = 2.5
        with self.assertRaises(ValueError): compare_rounds(reference, candidate)
        with self.assertRaises(ValueError): compare_rounds(reference[:1], candidate[:1])

    def artifacts(self, root):
        protocol = read_json("config/performance.acceptance.json")
        image = "sha256:" + "f" * 64
        base = self.record()
        env = {key: base[key] for key in ("commit", "harness_revision", "fixture_hash", "content_id", "quality", "media_hashes_sha256")}
        env.update(compose_project="own-project", run="own-run", applicationImages={"api": image, "worker": image},
                   loader={"kind": "docker", "imageID": image, "binarySHA256": "f" * 64, "limits": protocol["resources"]["loader"]},
                   services={service: {"limits": limits} for service, limits in protocol["infrastructureResources"].items()},
                   protocolSHA256=hashlib.sha256(Path("config/performance.acceptance.json").read_bytes()).hexdigest(),
                   composeSHA256=hashlib.sha256(Path("deploy/docker/compose.verify.yml").read_bytes()).hexdigest())
        (root / "environment.json").write_text(json.dumps(env))
        (root / "loader-build.json").write_text(json.dumps({"harnessRevision": base["harness_revision"], "imageID": image, "binarySHA256": "f" * 64}))
        hashes = {f'/api/v1/streaming/{base["content_id"]}/segment/segment_000.ts': "e" * 64}
        fixture = root / "media-hashes.json"; fixture.write_text(json.dumps(hashes))
        fixture.with_suffix(".provenance.json").write_text(json.dumps({"source": "MinIO published generation", "contentID": base["content_id"], "generation": 1,
            "quality": base["quality"], "hashFileSHA256": hashlib.sha256(fixture.read_bytes()).hexdigest(),
            "objects": [{"objectKey": f'streams/{base["content_id"]}/g1/{base["quality"]}/segment_000.ts', "sha256": "e" * 64, "bytes": 100}]}))
        for rel, (scenario, viewers) in expected_matrix().items():
            path = root / rel; path.parent.mkdir(parents=True, exist_ok=True)
            record = self.record(scenario, viewers); path.write_text(json.dumps(record))
            started = timestamp(record["started_at"])
            raw = [{"role": role, "containerID": role + "-id", "timestamp": started + t,
                    "cpu_nanoseconds": t * 1000000, "rss_bytes": 100, "memory_usage_bytes": 200}
                   for role in ("api", "worker", "loader") for t in range(31)]
            raw_path = path.with_name(f"cgroup-{scenario}-{viewers}.jsonl")
            raw_path.write_text("".join(json.dumps(row) + "\n" for row in raw))
            runtime = {"Running": True, "OOMKilled": False, "StartedAt": "one-start", "RestartCount": 0}
            identities = {role: {**limits, "imageID": image, "containerID": role + "-id", "revision": "b" * 40 if role == "loader" else "a" * 40,
                                 "project": "own-project", "run": "own-run", "runtimeBefore": runtime, "runtimeAfter": runtime}
                          for role, limits in protocol["resources"].items()}
            raw_path.with_suffix(".containers.json").write_text(json.dumps(identities))
            resources = {"commit": record["commit"], "scenario": scenario, "viewers": viewers, "containers": identities,
                         "resources": summarize(raw, started, started + 30, ["api", "worker", "loader"])}
            path.with_name(f"resources-{scenario}-{viewers}.json").write_text(json.dumps(resources))
            phase = path.parent / f"phase-{scenario}-{viewers}-unitfixture"; phase.mkdir()
            (phase / "ready.json").write_text("{}\n"); (phase / "release").touch()
            for moment in ("before", "after"):
                for role in ("api", "worker"):
                    (phase / f"{moment}.{role}.prom").write_text("process_cpu_seconds_total 1\nprocess_resident_memory_bytes 100\n")
            package = "github.com/rtcdance/streamgate/test/load"; test = "TestLoad_AuthorizedMedia"
            events = [{"Action": "start", "Package": package}, {"Action": "run", "Package": package, "Test": test},
                      {"Action": "pass", "Package": package, "Test": test}, {"Action": "pass", "Package": package}]
            path.with_suffix(".jsonl").write_text("".join(json.dumps(event) + "\n" for event in events))
            path.with_suffix(".execution.json").write_text(json.dumps({"exitCode": 0, "attachExitCode": 0, "testID": "TestLoad_AuthorizedMedia"}))

    def test_complete_artifact_schema_and_missing_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); self.artifacts(root)
            _, rows = validate_directory(root, "matrix", "a" * 40, "b" * 40)
            self.assertEqual(len(rows), 48)
            path = root / "round-3/rpc_all_down/cgroup-rpc_all_down-100.jsonl"
            path.write_text("")
            with self.assertRaises(ValueError): validate_directory(root, "matrix", "a" * 40, "b" * 40)

    def test_forged_resource_summary_is_recomputed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); self.artifacts(root)
            path = root / "round-1/resources-warm_cache-1.json"
            report = read_json(path); report["resources"]["api"]["cpu_seconds_delta"] = 0
            path.write_text(json.dumps(report))
            with self.assertRaises(ValueError): validate_directory(root, "matrix", "a" * 40, "b" * 40)

    def test_duplicate_and_nonfinite_json_fail(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "bad.json"
            for raw in ['{"count": 1, "count": 2}', '{"count": NaN}', '{"count": 1']:
                path.write_text(raw)
                with self.assertRaises(ValueError): read_json(path)

    def test_fixture_hash_set_cannot_be_replaced(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); self.artifacts(root)
            path = root / "media-hashes.json"; path.write_text('{"/wrong.ts":"' + "f" * 64 + '"}')
            with self.assertRaises(ValueError): validate_directory(root, "matrix", "a" * 40, "b" * 40)


if __name__ == "__main__": unittest.main()
