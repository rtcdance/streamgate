import copy
import hashlib
import json
import sys
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
from perf_protocol import (COMPARISON_VIEWERS, REPEATS, SCENARIOS, VIEWERS, expected_matrix, validate_layout,
                           validate_result, compare_rounds, validate_directory, read_json, acceptance_config,
                           compare_environments)
from resource_monitor import summarize, timestamp


class ProtocolTests(unittest.TestCase):
    def test_fixed_protocol_cannot_silently_shrink_or_default(self):
        configured = read_json("config/performance.acceptance.json")
        changes = [("viewers", [1]), ("scenarios", ["warm_cache"]), ("repeats", 1),
                   ("warmupSeconds", 1), ("sampleSeconds", 1), ("comparisonRounds", 1),
                   ("comparisonViewers", 1), ("p95RegressionLimit", 10)]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "acceptance.json"
            for key, value in changes:
                data = copy.deepcopy(configured); data[key] = value; path.write_text(json.dumps(data))
                with patch("perf_protocol.PROTOCOL_PATH", path), self.subTest(key=key), self.assertRaises(ValueError): acceptance_config()
            for raw in ["{}", "{"]:
                path.write_text(raw)
                with patch("perf_protocol.PROTOCOL_PATH", path), self.assertRaises(ValueError): acceptance_config()
            path.unlink()
            with patch("perf_protocol.PROTOCOL_PATH", path), self.assertRaises(ValueError): acceptance_config()

    def rpc_status(self, primary_active, fallback_active):
        return {"chains": [{"chain_id": 31337, "name": "anvil", "rpcs": [
            {"url": "rpc://31337/0", "is_active": primary_active, "failures": 0 if primary_active else 3},
            {"url": "rpc://31337/1", "is_active": fallback_active, "failures": 0}]}]}

    def prom_body(self, moment, role, scenario):
        if role == "worker":
            return ("process_cpu_seconds_total 1\nprocess_resident_memory_bytes 100\nstreamgate_transcoding_queue_depth 0\n"
                    "streamgate_transcoding_rtf_count 1\nstreamgate_transcoding_rtf_sum 0.1\n"
                    "streamgate_transcoding_duration_seconds_count 1\nstreamgate_transcoding_duration_seconds_sum 1\n")
        hits, manifests = (5, 5) if moment == "before" or scenario == "rpc_all_down" else (8, 20)
        failover = 1 if moment == "after" and scenario == "rpc_primary_down" else 0
        media = 1000 if moment == "before" or scenario == "rpc_all_down" else 2000
        if moment == "before" and scenario == "cold_cache": hits, manifests, media = 0, 0, 0
        return (f"process_cpu_seconds_total 1\nprocess_resident_memory_bytes 100\n"
                f"streamgate_streaming_cache_hits_total {hits}\nstreamgate_streaming_manifests_total {manifests}\n"
                f"streamgate_rpc_failover_total {failover}\nstreamgate_media_bytes_total {media}\n"
                "streamgate_rpc_latency_seconds_count 1\nstreamgate_rpc_latency_seconds_sum 0.01\n")
    def record(self, scenario="warm_cache", viewers=1):
        denied = scenario == "rpc_all_down"
        hashes = {"/api/v1/streaming/11111111-1111-4111-8111-111111111111/segment/segment_000.ts": "e" * 64}
        media_digest = hashlib.sha256(json.dumps(hashes, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
        return {"commit": "a" * 40, "harness_revision": "b" * 40, "scenario": scenario,
                "fixture_hash": "c" * 64, "media_hashes_sha256": media_digest,
                "content_id": "11111111-1111-4111-8111-111111111111", "quality": "640x360",
                "pace_by_extinf": True, "warmup_sec": 10, "duration_sec": 30,
                "started_at": "2026-10-19T01:00:00Z", "finished_at": "2026-10-19T01:00:30Z",
                "sampling_seconds": 30.0,
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

    def test_smoke_covers_all_four_scenarios_at_one_viewer_only(self):
        paths = {"round-1/" + ("" if scenario == "warm_cache" else scenario + "/") + scenario + "-viewers-1.json" for scenario in SCENARIOS}
        validate_layout(paths, "smoke")
        for missing in paths:
            with self.assertRaises(ValueError): validate_layout(paths - {missing}, "smoke")
        with self.assertRaises(ValueError): validate_layout(paths, "matrix")

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
                           ("verified_media_count", 0), ("media_hash_error_count", 1), ("http_statuses", {}),
                           ("sampling_seconds", 1.0), ("sampling_seconds", None),
                           ("finished_at", "2026-10-19T00:59:59Z")]:
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

    def test_sampling_window_cannot_be_even_fractionally_short(self):
        record = self.record("rpc_all_down")
        record["sampling_seconds"] = 29.999720
        with self.assertRaisesRegex(ValueError, "sampling interval"):
            validate_result(record, "a" * 40, "b" * 40)

    def test_wall_clock_skew_does_not_shorten_the_sampled_window(self):
        # Regression for a real failure: a platform that slews its wall clock
        # made the recorded timestamps differ by 29.998970s for a 30s sampling
        # deadline, which failed the reference evidence as if the run had been
        # shortened. The sampler's own monotonic measure is the window.
        record = self.record("rpc_all_down")
        record["finished_at"] = "2026-10-19T01:00:29.998970Z"
        validate_result(record, "a" * 40, "b" * 40)

    def distributed(self, scenario, viewers, successes, errors, aborted, statuses):
        """A record whose per-viewer rows add up to the given totals."""
        record = self.record(scenario, viewers=viewers)
        def spread(total):
            base, extra = divmod(total, viewers)
            return [base + (1 if i < extra else 0) for i in range(viewers)]
        rows = []
        for i, (s, e, a) in enumerate(zip(spread(successes), spread(errors), spread(aborted))):
            rows.append({"viewer_id": i, "success_count": s, "error_count": e,
                         "aborted_count": a, "verified_media_count": s})
        record["viewer_samples"] = rows
        record["success_count"], record["error_count"] = successes, errors
        record["aborted_count"], record["verified_media_count"] = aborted, successes
        record["total_bytes"] = successes * 100
        record["http_statuses"] = dict(statuses)
        return record

    def test_fallback_takes_the_same_tiers_as_the_healthy_scenarios(self):
        # The harness and this checker must not disagree about which scenario
        # gets which bar: ≥99% at 50 viewers, the capacity exploration above it.
        # Both measured rows below are real (round failures of the matrix).
        short = self.distributed("rpc_primary_down", 50, 566, 16, 0, {"200": 566, "503": 16})
        with self.assertRaisesRegex(ValueError, "success rate below 99%"):
            validate_result(short, "a" * 40, "b" * 40)
        exploration = self.distributed("rpc_primary_down", 100, 1064, 23, 15,
                                      {"200": 1064, "503": 23, "transport_error": 15})
        validate_result(exploration, "a" * 40, "b" * 40)
        denied = self.distributed("rpc_primary_down", 100, 1064, 24, 0, {"200": 1064, "503": 23, "403": 1})
        with self.assertRaisesRegex(ValueError, "correctness failure"):
            validate_result(denied, "a" * 40, "b" * 40)

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

    def environments(self):
        protocol = read_json("config/performance.acceptance.json")
        image = "sha256:" + "f" * 64
        base = {"runner": {"ID": "runner-1", "Architecture": "x86_64"},
                "protocolSHA256": hashlib.sha256(Path("config/performance.acceptance.json").read_bytes()).hexdigest(),
                "composeSHA256": hashlib.sha256(Path("deploy/docker/compose.verify.yml").read_bytes()).hexdigest(),
                "tools": {"go": "go version go1.26"},
                "fixture_hash": "9" * 64, "quality": "640x360",
                "services": {service: {"limits": limits, "imageID": image}
                             for service, limits in protocol["infrastructureResources"].items()},
                "loader": {"kind": "docker", "imageID": image, "binarySHA256": "e" * 64,
                           "limits": protocol["resources"]["loader"]}}
        reference = dict(base, harness_revision="b" * 40)
        candidate = dict(base, harness_revision="c" * 40)
        return reference, candidate

    def test_a_reused_reference_must_be_declared_and_share_the_loader_binary(self):
        # A cached reference measured by an earlier harness may be reused only
        # when the caller DECLARES that harness and the loader binary is
        # byte-identical; otherwise the comparison is between different
        # measuring instruments and must fail.
        reference, candidate = self.environments()
        with self.assertRaises(ValueError):
            compare_environments(reference, candidate)
        compare_environments(reference, candidate, accept_harness="b" * 40)
        # The tool image is rebuilt each run, so a different image ID alone must
        # not reject a reused reference — the load binary identity is the pin.
        rebuilt = copy.deepcopy(reference)
        rebuilt["loader"]["imageID"] = "sha256:" + "a" * 64
        compare_environments(rebuilt, candidate, accept_harness="b" * 40)
        reused = copy.deepcopy(reference)
        reused["loader"]["binarySHA256"] = "d" * 64
        with self.assertRaises(ValueError):
            compare_environments(reused, candidate, accept_harness="b" * 40)

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
        (root / "transcoding-metadata.json").write_text(json.dumps({"candidateSHA": base["commit"], "task": {
            "task_id": "22222222-2222-4222-8222-222222222222", "content_id": base["content_id"], "output_generation": 1,
            "status": "completed", "media_duration_seconds": 10, "probe_duration_seconds": .1,
            "transcode_duration_seconds": 1, "transcoding_rtf": .1}}))
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
            runtime = {"Running": True, "OOMKilled": False, "StartedAt": "2026-10-19T00:59:59Z", "RestartCount": 0}
            identities = {role: {**limits, "imageID": image, "containerID": role + "-id", "revision": "b" * 40 if role == "loader" else "a" * 40,
                                 "project": "own-project", "run": "own-run", "runtimeBefore": runtime, "runtimeAfter": runtime}
                          for role, limits in protocol["resources"].items()}
            raw_path.with_suffix(".containers.json").write_text(json.dumps(identities))
            resources = {"commit": record["commit"], "scenario": scenario, "viewers": viewers, "containers": identities,
                         "resources": summarize(raw, started, started + 30, ["api", "worker", "loader"])}
            path.with_name(f"resources-{scenario}-{viewers}.json").write_text(json.dumps(resources))
            phase = path.parent / f"phase-{scenario}-{viewers}-unitfixture"; phase.mkdir()
            (phase / "ready.json").write_text('{"event":"sampling.ready","timestamp":"2026-10-19T00:59:58Z"}\n'); (phase / "release").touch()
            (phase / "resources.ready.json").write_text(json.dumps({"event": "resources.ready", "roles": ["api", "loader", "worker"], "timestamp": started - .1}))
            for moment in ("before", "after"):
                for role in ("api", "worker"):
                    (phase / f"{moment}.{role}.prom").write_text(self.prom_body(moment, role, scenario))
            for name in ("anvil-proxy", "anvil-fallback-proxy"):
                stopped = scenario == "rpc_all_down" or (scenario == "rpc_primary_down" and name == "anvil-proxy")
                (phase / f"{name}.state.json").write_text(json.dumps({"State": {"Status": "exited" if stopped else "running"}}))
                (phase / f"{name}.logs.txt").write_text("")
            if scenario == "rpc_primary_down":
                (phase / "before.rpc-status.json").write_text(json.dumps(self.rpc_status(True, False)))
                (phase / "after.rpc-status.json").write_text(json.dumps(self.rpc_status(False, True)))
                (phase / "after.rpc-failover.json").write_text(json.dumps(
                    {"metric": "streamgate_rpc_failover_total", "before": 0.0, "after": 1.0, "delta": 1.0}, sort_keys=True))
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

    def test_real_task_probe_metadata_is_required(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); self.artifacts(root)
            (root / "transcoding-metadata.json").unlink()
            with self.assertRaises((ValueError, OSError)):
                validate_directory(root, "matrix", "a" * 40, "b" * 40)

    def test_matrix_shape_is_derived_from_the_acceptance_protocol(self):
        expected = expected_matrix()
        self.assertEqual(len(expected), REPEATS * len(SCENARIOS) * len(VIEWERS))
        self.assertEqual(len(expected), 48)
        self.assertEqual(SCENARIOS, ("warm_cache", "cold_cache", "rpc_primary_down", "rpc_all_down"))
        self.assertEqual(VIEWERS, (1, 10, 50, 100))
        validate_layout({f"round-{i}/warm_cache-viewers-{COMPARISON_VIEWERS}.json" for i in range(1, 4)}, "comparison")

    def phase(self, root, scenario, viewers=1):
        phases = [p for p in Path(root).rglob(f"phase-{scenario}-{viewers}-*") if "round-1" in p.parts]
        self.assertEqual(len(phases), 1)
        return phases[0]

    def test_failover_evidence_is_mandatory_and_must_be_real(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); self.artifacts(root)
            phase = self.phase(root, "rpc_primary_down")
            for missing in ("before.rpc-status.json", "after.rpc-status.json", "after.rpc-failover.json"):
                saved = (phase / missing).read_bytes(); (phase / missing).unlink()
                with self.subTest(missing=missing), self.assertRaises(ValueError):
                    validate_directory(root, "matrix", "a" * 40, "b" * 40)
                (phase / missing).write_bytes(saved)
            still_active = self.rpc_status(True, True)
            (phase / "after.rpc-status.json").write_text(json.dumps(still_active))
            with self.assertRaises(ValueError): validate_directory(root, "matrix", "a" * 40, "b" * 40)
            never_switched = self.rpc_status(False, False)
            (phase / "after.rpc-status.json").write_text(json.dumps(never_switched))
            with self.assertRaises(ValueError): validate_directory(root, "matrix", "a" * 40, "b" * 40)
            (phase / "after.rpc-status.json").write_text(json.dumps(self.rpc_status(False, True)))
            zero = {"metric": "streamgate_rpc_failover_total", "before": 0.0, "after": 0.0, "delta": 0.0}
            (phase / "after.rpc-failover.json").write_text(json.dumps(zero, sort_keys=True))
            (phase / "after.api.prom").write_text(self.prom_body("before", "api", "rpc_primary_down"))
            with self.assertRaises(ValueError): validate_directory(root, "matrix", "a" * 40, "b" * 40)

    def test_recorded_failover_delta_must_match_the_snapshots(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); self.artifacts(root)
            phase = self.phase(root, "rpc_primary_down")
            forged = {"metric": "streamgate_rpc_failover_total", "before": 0.0, "after": 9.0, "delta": 9.0}
            (phase / "after.rpc-failover.json").write_text(json.dumps(forged, sort_keys=True))
            with self.assertRaises(ValueError): validate_directory(root, "matrix", "a" * 40, "b" * 40)

    def test_pre_moved_slot_and_flat_window_are_rejected_with_the_real_shape(self):
        # The exact sg-all-1019-f round-1 rpc_primary_down @50 evidence: setup-
        # phase RPC transients moved the active slot before the injected fault,
        # the pre-fault snapshot already showed the fallback active, and the
        # sampling window recorded a flat cumulative counter (6->6). Both facts
        # must fail the gate.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); self.artifacts(root)
            phase = self.phase(root, "rpc_primary_down")
            flat = self.prom_body("before", "api", "rpc_primary_down").replace(
                "streamgate_rpc_failover_total 0", "streamgate_rpc_failover_total 6")
            (phase / "before.api.prom").write_text(flat)
            (phase / "after.api.prom").write_text(flat)
            (phase / "after.rpc-failover.json").write_text(json.dumps(
                {"metric": "streamgate_rpc_failover_total", "before": 6.0, "after": 6.0, "delta": 0.0}, sort_keys=True))
            (phase / "before.rpc-status.json").write_text(json.dumps(self.rpc_status(False, True)))
            with self.assertRaises(ValueError): validate_directory(root, "matrix", "a" * 40, "b" * 40)

    def test_failover_baseline_must_come_from_a_process_started_at_the_barrier(self):
        # "primary down -> real failover observed" is only deterministic when the
        # RPC state is fresh AT the fault injection: a process that predates the
        # sampling barrier can have migrated the active slot during the wallet-
        # setup phase (sg-all-1019-f round-1 @50), leaving the window flat. The
        # phase therefore must not be accepted when the API was not restarted
        # between the barrier and sampling.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); self.artifacts(root)
            stale = {"Running": True, "OOMKilled": False, "StartedAt": "2026-10-19T00:59:00Z", "RestartCount": 0}
            path = root / "round-1/rpc_primary_down/cgroup-rpc_primary_down-1.containers.json"
            identities = read_json(path)
            identities["api"]["runtimeBefore"] = stale
            identities["api"]["runtimeAfter"] = stale
            path.write_text(json.dumps(identities))
            resources_path = root / "round-1/rpc_primary_down/resources-rpc_primary_down-1.json"
            resources = read_json(resources_path)
            resources["containers"] = identities
            resources_path.write_text(json.dumps(resources))
            with self.assertRaises(ValueError): validate_directory(root, "matrix", "a" * 40, "b" * 40)

    def test_fresh_process_baseline_needs_no_lazy_series(self):
        # The barrier restart leaves the api without its lazy Vec series at the
        # pre-fault snapshot (the cold_cache shape). rpc_primary_down now takes
        # the same fresh baseline, so its before-snapshot must be accepted with
        # only the eagerly registered counters present.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); self.artifacts(root)
            phase = self.phase(root, "rpc_primary_down")
            fresh = (self.prom_body("before", "api", "rpc_primary_down")
                     .replace("streamgate_streaming_cache_hits_total 5\n", "")
                     .replace("streamgate_media_bytes_total 1000\n", "")
                     .replace("streamgate_rpc_latency_seconds_count 1\nstreamgate_rpc_latency_seconds_sum 0.01\n", ""))
            (phase / "before.api.prom").write_text(fresh)
            (phase / "after.rpc-failover.json").write_text(json.dumps(
                {"metric": "streamgate_rpc_failover_total", "before": 0.0, "after": 1.0, "delta": 1.0}, sort_keys=True))
            validate_directory(root, "matrix", "a" * 40, "b" * 40)

    def test_proxy_state_and_logs_are_mandatory(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); self.artifacts(root)
            phase = self.phase(root, "warm_cache")
            for name in ("anvil-proxy.state.json", "anvil-proxy.logs.txt", "anvil-fallback-proxy.state.json"):
                saved = (phase / name).read_bytes(); (phase / name).unlink()
                with self.subTest(missing=name), self.assertRaises(ValueError):
                    validate_directory(root, "matrix", "a" * 40, "b" * 40)
                (phase / name).write_bytes(saved)
            (phase / "anvil-proxy.state.json").write_text(json.dumps({"Config": {}}))
            with self.assertRaises(ValueError): validate_directory(root, "matrix", "a" * 40, "b" * 40)

    def test_domain_metric_regressions_fail_the_scenario(self):
        mutations = [
            ("warm_cache", "after.api.prom", "streamgate_streaming_cache_hits_total 8", "streamgate_streaming_cache_hits_total 5"),
            ("cold_cache", "after.api.prom", "streamgate_streaming_cache_hits_total 8", "streamgate_streaming_cache_hits_total 25"),
            ("rpc_all_down", "after.api.prom", "streamgate_rpc_failover_total 0", "streamgate_rpc_failover_total 1"),
            ("warm_cache", "after.worker.prom", "streamgate_transcoding_queue_depth 0", "streamgate_transcoding_queue_depth 1"),
        ]
        for scenario, name, old, new in mutations:
            with tempfile.TemporaryDirectory() as directory, self.subTest(scenario=scenario, mutation=new):
                root = Path(directory); self.artifacts(root)
                phase = self.phase(root, scenario)
                path = phase / name
                path.write_text(path.read_text().replace(old, new))
                with self.assertRaises(ValueError): validate_directory(root, "matrix", "a" * 40, "b" * 40)


if __name__ == "__main__": unittest.main()
