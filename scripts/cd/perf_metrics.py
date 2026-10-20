#!/usr/bin/env python3
"""Keep the real application and worker Prometheus snapshots; missing probes fail."""
import argparse
import json
import math
import os
from pathlib import Path
import urllib.request


def snapshot(url, output):
    headers = {}
    if os.environ.get("STREAMGATE_METRICS_TOKEN"):
        headers["Authorization"] = "Bearer " + os.environ["STREAMGATE_METRICS_TOKEN"]
    request = urllib.request.Request(url, headers=headers)
    with urllib.request.urlopen(request, timeout=5) as response:
        body = response.read(4 * 1024 * 1024 + 1)
    validate_observations(body)
    output.write_bytes(body)


def validate_observations(body):
    if len(body) > 4 * 1024 * 1024 or not body.endswith(b"\n"):
        raise ValueError("metrics response is truncated or oversized")
    observations = {}
    for line in body.splitlines():
        fields = line.split()
        if not fields or fields[0] not in (b"process_cpu_seconds_total", b"process_resident_memory_bytes"):
            continue
        if len(fields) != 2 or fields[0] in observations:
            raise ValueError("invalid or duplicate process observation")
        value = float(fields[1])
        if not math.isfinite(value) or value < 0 or (fields[0] == b"process_resident_memory_bytes" and value == 0):
            raise ValueError("invalid process observation value")
        observations[fields[0]] = value
    if len(observations) != 2:
        raise ValueError("metrics response is missing live process observations")
    return observations


def _series_value(line, name, labels):
    """Return the float value of a prometheus sample line belonging to `name`.

    `labels` optionally narrows the match to series carrying exactly those
    label pairs (e.g. {"cache": "segment_index"}). Help/type comment lines are
    not samples. Values must be finite and nonnegative for every metric this
    module reasons about — a NaN or negative counter is corrupt evidence.
    """
    prefix = name.encode()
    if not (line.startswith(prefix + b" ") or line.startswith(prefix + b"{")):
        return None
    fields = line.split()
    if len(fields) != 2:
        raise ValueError(f"invalid sample for {name}")
    if fields[0] != prefix:
        rest = fields[0][len(prefix):]
        if not (rest.startswith(b"{") and rest.endswith(b"}")):
            return None
        present = {}
        for pair in rest[1:-1].split(b","):
            key, _, value = pair.partition(b"=")
            present[key.decode()] = value.decode().strip('"')
        if labels is not None and present != labels:
            return None
    elif labels:
        return None
    try:
        value = float(fields[1])
    except ValueError:
        raise ValueError(f"invalid sample value for {name}") from None
    if not math.isfinite(value) or value < 0:
        raise ValueError(f"invalid sample value for {name}")
    return value


def metric_total(body, name, labels=None, required=False):
    """Sum every series of exactly `name` (optionally label-filtered); 0.0 when absent."""
    total = 0.0
    seen = set()
    for line in body.splitlines():
        if not line or line.startswith(b"#"):
            continue
        value = _series_value(line, name, labels)
        if value is not None:
            identity = line.rsplit(None, 1)[0]
            if identity in seen:
                raise ValueError(f"duplicate series for {name}")
            total += value
            seen.add(identity)
    if not seen and required:
        raise ValueError(f"required live metric is missing: {name}")
    return total


FAILOVER_METRIC = "streamgate_rpc_failover_total"


def failover_totals(before, after):
    """(before, after, delta) of the RPC failover counter across both snapshots."""
    total_before = metric_total(before, FAILOVER_METRIC)
    total_after = metric_total(after, FAILOVER_METRIC)
    return total_before, total_after, total_after - total_before


def validate_scenario_metrics(before_api, after_api, before_worker, after_worker, scenario):
    """Require live domain observations and reject work outside the protocol."""
    api_metrics = ("streamgate_streaming_cache_hits_total", "streamgate_streaming_manifests_total",
                   "streamgate_rpc_latency_seconds_count", "streamgate_rpc_latency_seconds_sum", "streamgate_media_bytes_total")
    worker_metrics = ("streamgate_transcoding_queue_depth", "streamgate_transcoding_rtf_count", "streamgate_transcoding_rtf_sum",
                      "streamgate_transcoding_duration_seconds_count", "streamgate_transcoding_duration_seconds_sum")
    for role, before, after, names in [("api", before_api, after_api, api_metrics), ("worker", before_worker, after_worker, worker_metrics)]:
        for name in names:
            # A freshly restarted cold API has not created lazy Vec series
            # yet. The post-sampling observation is still mandatory.
            # rpc_primary_down now restarts the API at the sampling barrier too
            # (fresh RPC state at the injected fault), so its baseline carries
            # the same fresh-process shape.
            fresh_vector = scenario in ("cold_cache", "rpc_primary_down") and role == "api" and name != "streamgate_streaming_manifests_total"
            first = metric_total(before, name, required=not fresh_vector)
            last = metric_total(after, name, required=True)
            if name != "streamgate_transcoding_queue_depth" and last < first:
                raise ValueError(f"counter reset inside sampling: {name}")
            if name.startswith("streamgate_transcoding_") and name != "streamgate_transcoding_queue_depth" and (first <= 0 or last != first):
                raise ValueError("missing positive precomputed transcoding observation or transcoding overlapped playback")
    hits_delta = metric_total(after_api, "streamgate_streaming_cache_hits_total") - metric_total(before_api, "streamgate_streaming_cache_hits_total")
    manifests_delta = metric_total(after_api, "streamgate_streaming_manifests_total") - metric_total(before_api, "streamgate_streaming_manifests_total")
    _, _, failover_delta = failover_totals(before_api, after_api)
    queue_depth_after = metric_total(after_worker, "streamgate_transcoding_queue_depth")
    rtf_sum_delta, rtf_count_delta = (metric_total(after_worker, "streamgate_transcoding_rtf_sum") - metric_total(before_worker, "streamgate_transcoding_rtf_sum"),
                                      metric_total(after_worker, "streamgate_transcoding_rtf_count") - metric_total(before_worker, "streamgate_transcoding_rtf_count"))
    latency_sum_delta, latency_count_delta = (metric_total(after_api, "streamgate_rpc_latency_seconds_sum") - metric_total(before_api, "streamgate_rpc_latency_seconds_sum"),
                                              metric_total(after_api, "streamgate_rpc_latency_seconds_count") - metric_total(before_api, "streamgate_rpc_latency_seconds_count"))
    # Every manifest request consults at most one of the two hit-counted
    # caches, so hits_delta <= manifests_delta always; equality after a real
    # cache flush means the flush never happened.
    if scenario == "warm_cache" and manifests_delta > 0 and hits_delta <= 0:
        raise ValueError("warm-cache sampling served manifests but recorded zero streaming cache hits")
    if scenario == "cold_cache" and manifests_delta > 0 and hits_delta >= manifests_delta:
        raise ValueError("cold-cache sampling shows a hit-dominated manifest regime after the cache flush")
    if scenario == "cold_cache" and any(metric_total(before_api, name) != 0 for name in ("streamgate_streaming_cache_hits_total", "streamgate_streaming_manifests_total", "streamgate_media_bytes_total")):
        raise ValueError("cold-cache API already served media before sampling started")
    if scenario == "rpc_primary_down" and failover_delta <= 0:
        raise ValueError("primary RPC went down but the failover counter never incremented")
    if scenario == "rpc_all_down" and failover_delta > 0:
        raise ValueError("the RPC failover counter incremented although every endpoint is down")
    if queue_depth_after > 0:
        raise ValueError("worker reports a non-empty transcoding queue after a playback-only run")
    if metric_total(before_worker, "streamgate_transcoding_queue_depth", required=True) > 0:
        raise ValueError("worker reports a non-empty transcoding queue before playback")
    media_delta = metric_total(after_api, "streamgate_media_bytes_total", required=True) - metric_total(before_api, "streamgate_media_bytes_total", required=scenario not in ("cold_cache", "rpc_primary_down"))
    if scenario == "rpc_all_down" and (media_delta != 0 or manifests_delta != 0):
        raise ValueError("application served media while all RPC endpoints were down")
    if scenario != "rpc_all_down" and manifests_delta > 0 and media_delta <= 0:
        raise ValueError("served manifests have no measured media bytes")
    if rtf_count_delta > 0 and rtf_sum_delta <= 0:
        raise ValueError("new transcoding RTF observations are not positive")
    if latency_count_delta > 0 and latency_sum_delta < 0:
        raise ValueError("rpc latency sum decreased while its observation count grew")


def capture_failover_delta(phase_dir):
    """Record the streamgate_rpc_failover_total counter delta of one sampling phase.

    Reads the phase's real before/after api snapshots; never fabricates values.
    """
    phase = Path(phase_dir)
    before = (phase / "before.api.prom").read_bytes()
    after = (phase / "after.api.prom").read_bytes()
    total_before, total_after, delta = failover_totals(before, after)
    evidence = {"metric": FAILOVER_METRIC, "before": total_before, "after": total_after, "delta": delta}
    (phase / "after.rpc-failover.json").write_text(json.dumps(evidence, sort_keys=True) + "\n")
    return evidence


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-prefix", type=Path)
    parser.add_argument("--failover-delta", type=Path, metavar="PHASE_DIR",
                        help="record the RPC failover counter delta of a finished phase's snapshots")
    args = parser.parse_args()
    if args.failover_delta is not None:
        capture_failover_delta(args.failover_delta)
        return
    if args.output_prefix is None:
        parser.error("--output-prefix or --failover-delta is required")
    for role, url in [("api", "http://monolith:8080/metrics"), ("worker", "http://transcoder:8080/metrics")]:
        snapshot(url, Path(str(args.output_prefix) + "." + role + ".prom"))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError) as exc:
        raise SystemExit(f"required metrics capture failed: {exc}")
