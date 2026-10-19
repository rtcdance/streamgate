"""The fixed SG-07 protocol. PR smoke never satisfies the candidate matrix."""
import argparse
import hashlib
import json
import math
from pathlib import Path
import re
import statistics
import subprocess
import uuid

from assert_required_tests import validate as validate_tests
from perf_metrics import FAILOVER_METRIC, failover_totals, validate_observations, validate_scenario_metrics
from resource_monitor import summarize, timestamp, validate_identities

PROTOCOL_PATH = Path(__file__).resolve().parent.parent.parent / "config" / "performance.acceptance.json"
PROXY_CONTAINERS = ("anvil-proxy", "anvil-fallback-proxy")
PRIMARY_RPC_SLOT = "rpc://31337/0"
FALLBACK_RPC_SLOT = "rpc://31337/1"


def acceptance_config():
    try:
        data = json.loads(PROTOCOL_PATH.read_text())
    except (OSError, ValueError) as exc:
        raise ValueError("performance acceptance configuration is missing or unreadable") from exc
    required = {"scenarios": ["warm_cache", "cold_cache", "rpc_primary_down", "rpc_all_down"],
                "viewers": [1, 10, 50, 100], "repeats": 3, "comparisonViewers": 50,
                "comparisonRounds": 3, "p95RegressionLimit": 1.20, "warmupSeconds": 10, "sampleSeconds": 30}
    if type(data) is not dict or any(type(data.get(key)) is not type(value) or data[key] != value for key, value in required.items()):
        raise ValueError("performance acceptance configuration differs from the required fixed protocol")
    config = {key: data[key] for key in required}
    config["scenarios"], config["viewers"] = tuple(config["scenarios"]), tuple(config["viewers"])
    return config


ACCEPTANCE = acceptance_config()
SCENARIOS = ACCEPTANCE["scenarios"]
VIEWERS = ACCEPTANCE["viewers"]
REPEATS = ACCEPTANCE["repeats"]
COMPARISON_VIEWERS = ACCEPTANCE["comparisonViewers"]
COMPARISON_ROUNDS = ACCEPTANCE["comparisonRounds"]
P95_REGRESSION_LIMIT = ACCEPTANCE["p95RegressionLimit"]
WARMUP_SECONDS = ACCEPTANCE["warmupSeconds"]
SAMPLE_SECONDS = ACCEPTANCE["sampleSeconds"]
COUNTS = ("success_count", "error_count", "aborted_count", "verified_media_count")


def expected_matrix():
    return {f"round-{i}/" + ("" if scenario == "warm_cache" else scenario + "/") + f"{scenario}-viewers-{viewers}.json": (scenario, viewers)
            for i in range(1, REPEATS + 1) for scenario in SCENARIOS for viewers in VIEWERS}


def validate_layout(paths, mode):
    if mode not in ("matrix", "comparison", "reference", "smoke"):
        raise ValueError("unknown performance evidence mode")
    expected = set(expected_matrix()) if mode == "matrix" else {f"round-{i}/warm_cache-viewers-{COMPARISON_VIEWERS}.json" for i in range(1, COMPARISON_ROUNDS + 1)}
    if mode == "smoke":
        expected = {"round-1/" + ("" if scenario == "warm_cache" else scenario + "/") + scenario + "-viewers-1.json" for scenario in SCENARIOS}
    if mode == "reference":
        expected.add("warm_cache-viewers-1.json")  # Explicit pool-setup smoke, never a comparison round.
    if set(paths) != expected:
        raise ValueError(f"incomplete or stale {mode} results: missing={sorted(expected - set(paths))}, extra={sorted(set(paths) - expected)}")


def number(value):
    if type(value) not in (float, int) or not math.isfinite(value) or value < 0:
        raise ValueError("missing or invalid nonnegative observation")
    return value


def count(value):
    if type(value) is not int or value < 0:
        raise ValueError("missing or invalid count")
    return value


def validate_result(record, target, harness):
    if record["commit"] != target or record["harness_revision"] != harness:
        raise ValueError("application or harness revision mismatch")
    for name in ("fixture_hash", "media_hashes_sha256"):
        if not re.fullmatch(r"[0-9a-f]{64}", record[name]):
            raise ValueError("missing fixture hash identity")
    if str(uuid.UUID(record["content_id"])) != record["content_id"] or not record["quality"]:
        raise ValueError("invalid content or rendition")
    if record["pace_by_extinf"] is not True or record["warmup_sec"] != WARMUP_SECONDS or record["duration_sec"] != SAMPLE_SECONDS:
        raise ValueError(f"playback protocol differs from {WARMUP_SECONDS}s warmup / {SAMPLE_SECONDS}s sampling / EXTINF pacing")
    if count(record["media_hash_error_count"]) != 0:
        raise ValueError("a media response failed independent hash verification")
    elapsed = timestamp(record["finished_at"]) - timestamp(record["started_at"])
    if not SAMPLE_SECONDS <= elapsed <= SAMPLE_SECONDS + 2:
        raise ValueError("actual sampling interval differs from protocol")
    viewers = count(record["viewers"])
    if viewers not in VIEWERS or record["wallet_pool_size"] != viewers or record["login_paced"] is not True or record["logins_failed"] != 0:
        raise ValueError("missing independent, successfully logged-in viewer identities")
    rows = record["viewer_samples"]
    if len(rows) != viewers:
        raise ValueError("missing viewer samples")
    for index, row in enumerate(rows):
        for field in COUNTS: count(row[field])
        if row["viewer_id"] != index or row["success_count"] + row["error_count"] == 0 or row["verified_media_count"] > row["success_count"]:
            raise ValueError("invalid or empty viewer samples")
    for field in COUNTS:
        if count(record[field]) != sum(row[field] for row in rows):
            raise ValueError("viewer totals differ from aggregate")
    statuses = record["http_statuses"]
    if not statuses or sum(count(v) for v in statuses.values()) != sum(record[f] for f in COUNTS[:3]):
        raise ValueError("HTTP status totals differ from sample totals")
    successes, errors = record["success_count"], record["error_count"]
    scenario = record["scenario"]
    if scenario not in SCENARIOS:
        raise ValueError("unknown performance scenario")
    if scenario == "rpc_all_down":
        if successes or count(record["total_bytes"]) or record["verified_media_count"]:
            raise ValueError("media served while every RPC endpoint is down")
    else:
        if viewers <= 50 or scenario == "rpc_primary_down":
            if successes / (successes + errors) < .99:
                raise ValueError("success rate below 99%, including every authentication and service error")
        elif any(status not in ("200", "206", "429", "transport_error") and not re.fullmatch(r"5[0-9]{2}", status) for status in statuses):
            raise ValueError("capacity exploration contains a correctness failure")
        if successes and (not count(record["total_bytes"]) or not record["verified_media_count"]):
            raise ValueError("successes contain no verified media bytes")
        percentiles = [number(record["ttfb_ms"][p]) for p in ("p50", "p95", "p99")]
        if successes and (not percentiles[1] or percentiles != sorted(percentiles)):
            raise ValueError("invalid first-byte percentiles")


def compare_rounds(reference, candidate):
    if len(reference) != COMPARISON_ROUNDS or len(candidate) != COMPARISON_ROUNDS:
        raise ValueError(f"comparison requires exactly {COMPARISON_ROUNDS} rounds on each side")
    ref, cand = [statistics.median(number(r["ttfb_ms"]["p95"]) for r in rows) for rows in (reference, candidate)]
    if ref <= 0 or cand <= 0 or cand > ref * P95_REGRESSION_LIMIT:
        raise ValueError(f"candidate median p95 exceeds reference x {P95_REGRESSION_LIMIT} or has no samples")
    return {"referenceMedianP95MS": ref, "candidateMedianP95MS": cand, "ratio": cand / ref, "limit": P95_REGRESSION_LIMIT}


def compare_environments(reference, candidate):
    for key in ("runner", "harness_revision", "protocolSHA256", "composeSHA256", "tools", "fixture_hash", "quality", "loader"):
        if not reference.get(key) or reference[key] != candidate[key]:
            raise ValueError("reference and candidate differ in runner, tooling, protocol, fixture or loader identity")
    for service in set(reference["services"]) | set(candidate["services"]):
        ref, cand = reference["services"][service], candidate["services"][service]
        if ref["limits"] != cand["limits"] or (service not in ("monolith", "transcoder") and ref["imageID"] != cand["imageID"]):
            raise ValueError("reference and candidate service infrastructure differs")


def read_json(path):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result: raise ValueError("duplicate JSON field")
            result[key] = value
        return result
    return json.loads(Path(path).read_text(), object_pairs_hook=unique, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("nonfinite JSON number")))


def validate_fixture(root, env):
    path = Path(root) / "media-hashes.json"
    hashes = read_json(path)
    provenance = read_json(path.with_suffix(".provenance.json"))
    if provenance["source"] != "MinIO published generation" or provenance["contentID"] != env["content_id"] or provenance["quality"] != env["quality"] or count(provenance["generation"]) == 0:
        raise ValueError("independent fixture identity differs from the measured content")
    if provenance["hashFileSHA256"] != hashlib.sha256(path.read_bytes()).hexdigest():
        raise ValueError("independent hash file differs from the storage capture")
    encoded = json.dumps(hashes, sort_keys=True, separators=(",", ":")).encode()
    if not hashes or hashlib.sha256(encoded).hexdigest() != env["media_hashes_sha256"]:
        raise ValueError("workload hash set differs from independent storage evidence")
    expected = {}
    prefix = f'streams/{env["content_id"]}/g{provenance["generation"]}/{env["quality"]}/'
    for obj in provenance["objects"]:
        name = obj["objectKey"].removeprefix(prefix)
        if not obj["objectKey"].startswith(prefix) or not re.fullmatch(r"[A-Za-z0-9_-]+\.ts", name) or not re.fullmatch(r"[0-9a-f]{64}", obj["sha256"]) or count(obj["bytes"]) == 0:
            raise ValueError("invalid independent storage object evidence")
        key = f'/api/v1/streaming/{env["content_id"]}/segment/{name}'
        if key in expected: raise ValueError("duplicate fixture object")
        expected[key] = obj["sha256"]
    if hashes != expected:
        raise ValueError("fixture paths/digests differ from the stored rendition")
    validate_transcoding(read_json(Path(root) / "transcoding-metadata.json"), env, provenance["generation"])


def validate_transcoding(evidence, env, generation):
    task = evidence["task"]
    if (evidence["candidateSHA"] != env["commit"] or task["content_id"] != env["content_id"]
            or task["output_generation"] != generation or task["status"] != "completed"
            or str(uuid.UUID(task["task_id"])) != task["task_id"]):
        raise ValueError("transcoding metadata does not identify this candidate's published fixture")
    values = [number(task[field]) for field in ("media_duration_seconds", "probe_duration_seconds", "transcode_duration_seconds", "transcoding_rtf")]
    if any(value <= 0 for value in values) or not math.isclose(values[3], values[2] / values[0], rel_tol=1e-6):
        raise ValueError("transcoding metadata lacks real positive probe/wall measurements or has an inconsistent RTF")


def rpc_slots(snapshot):
    data = read_json(snapshot)
    chains = data.get("chains")
    if not isinstance(chains, list):
        raise ValueError("rpc-status snapshot has no chains")
    for chain in chains:
        if chain.get("chain_id") == 31337:
            rpcs = chain.get("rpcs")
            if not isinstance(rpcs, list) or [rpc.get("url") for rpc in rpcs] != [PRIMARY_RPC_SLOT, FALLBACK_RPC_SLOT]:
                raise ValueError("rpc-status snapshot does not expose the two expected chain-31337 endpoint slots")
            for rpc in rpcs:
                if type(rpc.get("is_active")) is not bool or type(rpc.get("failures")) is not int or rpc.get("failures") < 0:
                    raise ValueError("rpc-status snapshot carries invalid endpoint state")
            return {rpc["url"]: rpc for rpc in rpcs}
    raise ValueError("rpc-status snapshot does not report chain 31337")


def validate_rpc_failover(phase):
    for name in ("before.rpc-status.json", "after.rpc-status.json", "after.rpc-failover.json"):
        if not (phase / name).is_file():
            raise ValueError(f"missing {name} failover evidence")
    before = rpc_slots(phase / "before.rpc-status.json")
    after = rpc_slots(phase / "after.rpc-status.json")
    if not before[PRIMARY_RPC_SLOT]["is_active"]:
        raise ValueError("the primary RPC endpoint was not serving before the fault was injected")
    if after[PRIMARY_RPC_SLOT]["is_active"]:
        raise ValueError("the primary RPC endpoint is still serving after it was stopped")
    if not after[FALLBACK_RPC_SLOT]["is_active"]:
        raise ValueError("the fallback RPC endpoint never took over in the post-run snapshot")
    recorded = read_json(phase / "after.rpc-failover.json")
    snapshot_before = (phase / "before.api.prom").read_bytes()
    snapshot_after = (phase / "after.api.prom").read_bytes()
    totals_before, totals_after, delta = failover_totals(snapshot_before, snapshot_after)
    if recorded != {"metric": FAILOVER_METRIC, "before": totals_before, "after": totals_after, "delta": delta}:
        raise ValueError("recorded failover counter delta differs from the prometheus snapshots")
    if delta <= 0:
        raise ValueError("the failover counter did not increment although the primary endpoint went down")


def validate_proxy_evidence(phase, scenario):
    for name in PROXY_CONTAINERS:
        state_path = phase / f"{name}.state.json"
        if not state_path.is_file() or not (phase / f"{name}.logs.txt").is_file():
            raise ValueError(f"{name} state or recent logs are missing from the sampling phase")
        state = read_json(state_path)
        if type(state.get("State")) is not dict or not state["State"].get("Status"):
            raise ValueError(f"{name} state evidence lacks container state")
        stopped = scenario == "rpc_all_down" or (scenario == "rpc_primary_down" and name == "anvil-proxy")
        if state["State"]["Status"] != ("exited" if stopped else "running"):
            raise ValueError(f"{name} observed state contradicts the injected fault")


def validate_directory(root, mode, target, harness):
    root = Path(root)
    paths = {str(p.relative_to(root)) for p in root.rglob("*-viewers-*.json") if not p.name.endswith(".execution.json")}
    validate_layout(paths, mode)
    env = read_json(root / "environment.json")
    if env["commit"] != target or env["harness_revision"] != harness or env["loader"]["kind"] != "docker":
        raise ValueError("environment target, harness or loader differs from protocol")
    validate_fixture(root, env)
    protocol = read_json("config/performance.acceptance.json")
    for service, limits in protocol["infrastructureResources"].items():
        if env["services"][service]["limits"] != limits:
            raise ValueError("infrastructure runtime budget differs from protocol")
    for field, file in [("protocolSHA256", "config/performance.acceptance.json"), ("composeSHA256", "deploy/docker/compose.verify.yml")]:
        if env[field] != hashlib.sha256(Path(file).read_bytes()).hexdigest():
            raise ValueError("environment configuration hash mismatch")
    if env["loader"]["limits"] != protocol["resources"]["loader"] or not re.fullmatch(r"[0-9a-f]{64}", env["loader"]["binarySHA256"]):
        raise ValueError("loader budget or binary identity missing")
    if read_json(root / "loader-build.json") != {"harnessRevision": harness, "imageID": env["loader"]["imageID"], "binarySHA256": env["loader"]["binarySHA256"]}:
        raise ValueError("loader environment differs from its build manifest")
    results = {}
    for rel in sorted(paths):
        path = root / rel
        record = read_json(path)
        validate_result(record, target, harness)
        scenario, viewers = record["scenario"], record["viewers"]
        if path.name != f"{scenario}-viewers-{viewers}.json" or (mode == "matrix" and (scenario, viewers) != expected_matrix()[rel]):
            raise ValueError("result does not match its scenario/round slot")
        if any(record[key] != env[key] for key in ("fixture_hash", "content_id", "quality", "media_hashes_sha256")):
            raise ValueError("fixture or rendition identity changed within one run")
        resources = read_json(path.with_name(f"resources-{scenario}-{viewers}.json"))
        raw = path.with_name(f"cgroup-{scenario}-{viewers}.jsonl")
        identities = read_json(raw.with_suffix(".containers.json"))
        validate_identities(identities, record, protocol["resources"])
        calculated = summarize([json.loads(line) for line in raw.read_text().splitlines()], timestamp(record["started_at"]), timestamp(record["finished_at"]), ["api", "worker", "loader"])
        if resources["resources"] != calculated or resources["containers"] != identities or any(resources[k] != record[k] for k in ("commit", "scenario", "viewers")):
            raise ValueError("resource summary differs from raw sampling evidence")
        for role in ("api", "worker", "loader"):
            expected_image = env["loader"]["imageID"] if role == "loader" else env["applicationImages"][role]
            if identities[role]["imageID"] != expected_image or identities[role]["project"] != env["compose_project"] or identities[role]["run"] != env["run"]:
                raise ValueError("resource identity differs from the environment")
        phases = list(path.parent.glob(f"phase-{scenario}-{viewers}-*"))
        if len(phases) != 1 or not (phases[0] / "ready.json").is_file() or not (phases[0] / "release").is_file():
            raise ValueError("missing or duplicate sampling phase evidence")
        snapshots = {}
        for moment in ("before", "after"):
            for role in ("api", "worker"):
                body = (phases[0] / f"{moment}.{role}.prom").read_bytes()
                validate_observations(body)
                snapshots[moment, role] = body
        validate_scenario_metrics(snapshots["before", "api"], snapshots["after", "api"], snapshots["before", "worker"], snapshots["after", "worker"], scenario)
        validate_proxy_evidence(phases[0], scenario)
        ready = read_json(phases[0] / "ready.json")
        resources_ready = read_json(phases[0] / "resources.ready.json")
        if (ready.get("event") != "sampling.ready" or resources_ready.get("event") != "resources.ready"
                or resources_ready.get("roles") != ["api", "loader", "worker"]
                or not timestamp(ready["timestamp"]) <= number(resources_ready["timestamp"]) <= timestamp(record["started_at"])):
            raise ValueError("sampling was released without all resource probes being ready")
        if scenario == "cold_cache" and not timestamp(ready["timestamp"]) < timestamp(identities["api"]["runtimeBefore"]["StartedAt"]) < timestamp(record["started_at"]):
            raise ValueError("cold-cache API was not restarted between warmup and sampling")
        if scenario == "rpc_primary_down":
            validate_rpc_failover(phases[0])
        execution = read_json(path.with_suffix(".execution.json"))
        if execution != {"exitCode": 0, "attachExitCode": 0, "testID": "TestLoad_AuthorizedMedia"}:
            raise ValueError("loader command exited unsuccessfully or lacks original process status")
        errors, _ = validate_tests(path.with_suffix(".jsonl"), [{"package": "github.com/rtcdance/streamgate/test/load", "test": "TestLoad_AuthorizedMedia"}], execution["exitCode"])
        if errors: raise ValueError("live test evidence did not complete successfully: " + "; ".join(errors))
        results[rel] = record
    return env, results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path)
    parser.add_argument("--mode", choices=("matrix", "comparison", "reference", "smoke"), default="matrix")
    parser.add_argument("--target")
    parser.add_argument("--reference-root", type=Path)
    args = parser.parse_args()
    harness = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    env, rows = validate_directory(args.root, args.mode, args.target or harness, harness)
    if args.reference_root:
        if args.mode != "comparison": raise ValueError("reference comparison requires comparison mode")
        baseline = read_json("config/performance.acceptance.json")["safetyBaselineRevision"]
        ref_env, ref_rows = validate_directory(args.reference_root, "reference", baseline, harness)
        compare_environments(ref_env, env)
        paths = [f"round-{i}/warm_cache-viewers-{COMPARISON_VIEWERS}.json" for i in range(1, COMPARISON_ROUNDS + 1)]
        reference, candidate = [ref_rows[p] for p in paths], [rows[p] for p in paths]
        if max(timestamp(r["finished_at"]) for r in reference) > min(timestamp(r["started_at"]) for r in candidate):
            raise ValueError("reference and candidate did not run sequentially on the runner")
        comparison = compare_rounds(reference, candidate)
        (args.root / "comparison.json").write_text(json.dumps(comparison, sort_keys=True, indent=2) + "\n")
        print(json.dumps(comparison, sort_keys=True))
    print(f"performance evidence: PASS ({len(rows)} exact {args.mode} cases)")


if __name__ == "__main__":
    try: main()
    except (OSError, ValueError, KeyError, TypeError, ZeroDivisionError) as exc:
        raise SystemExit(f"performance evidence failed: {exc}")
