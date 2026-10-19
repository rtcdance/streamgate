#!/usr/bin/env python3
"""Keep the real application and worker Prometheus snapshots; missing probes fail."""
import argparse
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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-prefix", required=True, type=Path)
    args = parser.parse_args()
    for role, url in [("api", "http://monolith:8080/metrics"), ("worker", "http://transcoder:8080/metrics")]:
        snapshot(url, Path(str(args.output_prefix) + "." + role + ".prom"))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError) as exc:
        raise SystemExit(f"required metrics capture failed: {exc}")
