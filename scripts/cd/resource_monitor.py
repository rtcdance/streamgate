#!/usr/bin/env python3
"""Docker cgroup observations, restricted to the loader's playback sample window."""
import argparse
from datetime import datetime
import http.client
import json
import math
from pathlib import Path
import re
import signal
import socket
import time
from urllib.parse import quote


def integer(value, label):
    if type(value) is not int or value < 0:
        raise ValueError(f"missing or invalid {label}")
    return value


def normalize(stats):
    cpu = integer(stats.get("cpu_stats", {}).get("cpu_usage", {}).get("total_usage"), "CPU counter")
    memory = stats.get("memory_stats", {})
    usage = integer(memory.get("usage"), "memory usage")
    fields = memory.get("stats", {})
    rss = fields.get("total_rss", fields.get("rss"))
    if rss is None:
        rss = integer(fields.get("anon"), "cgroup v2 anonymous pages") + integer(fields.get("file_mapped"), "cgroup v2 mapped file pages")
    return {"cpu_nanoseconds": cpu, "rss_bytes": integer(rss, "resident memory"), "memory_usage_bytes": usage}


def summarize(samples, started, finished, roles):
    duration = finished - started
    if not math.isfinite(duration) or duration <= 0:
        raise ValueError("invalid playback window")
    result = {}
    for role in roles:
        rows = sorted((s for s in samples if s.get("role") == role and started <= s["timestamp"] <= finished), key=lambda s: s["timestamp"])
        if len(rows) < 2 or any("error" in s for s in rows):
            raise ValueError(f"{role}: missing or failed resource observations")
        if len({s["containerID"] for s in rows}) != 1:
            raise ValueError(f"{role}: container replaced during playback")
        observed = rows[-1]["timestamp"] - rows[0]["timestamp"]
        if observed < duration * 0.90:
            raise ValueError(f"{role}: resource observations cover less than 90% of playback")
        for previous, current in zip(rows, rows[1:]):
            if not 0 < current["timestamp"] - previous["timestamp"] <= 2.5:
                raise ValueError(f"{role}: missing or duplicate resource samples")
            if current["cpu_nanoseconds"] < previous["cpu_nanoseconds"]:
                raise ValueError(f"{role}: CPU counter reset")
        rss = max(integer(s["rss_bytes"], "resident memory") for s in rows)
        if rss <= 0:
            raise ValueError(f"{role}: no resident memory recorded")
        result[role] = {"containerID": rows[0]["containerID"], "sample_count": len(rows),
                        "observed_seconds": observed, "coverage_fraction": observed / duration,
                        "cpu_seconds_delta": (rows[-1]["cpu_nanoseconds"] - rows[0]["cpu_nanoseconds"]) / 1e9,
                        "peak_rss_bytes": rss, "peak_memory_usage_bytes": max(s["memory_usage_bytes"] for s in rows)}
    return result


def validate_identities(identities, playback, budgets):
    projects, runs = set(), set()
    for role in ("api", "worker", "loader"):
        record = identities[role]
        if not re.fullmatch(r"sha256:[0-9a-f]{64}", record["imageID"]):
            raise ValueError(f"{role}: missing image identity")
        for key in ("nanoCPUs", "memoryBytes"):
            if record[key] != budgets[role][key]:
                raise ValueError(f"{role}: runtime resource limit differs from the acceptance configuration")
        revision = playback["harness_revision"] if role == "loader" else playback["commit"]
        if record.get("revision") != revision:
            raise ValueError(f"{role}: runtime revision differs from the recorded workload")
        projects.add(record.get("project")); runs.add(record.get("run"))
        if role != "loader":
            before, after = record["runtimeBefore"], record["runtimeAfter"]
            if not before["Running"] or not after["Running"] or after["OOMKilled"] or before["StartedAt"] != after["StartedAt"] or before["RestartCount"] != after["RestartCount"]:
                raise ValueError(f"{role}: application restarted or stopped during the workload")
    if len(projects) != 1 or None in projects or len(runs) != 1 or None in runs:
        raise ValueError("resource identities belong to different or unknown projects")


class UnixConnection(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__("localhost", timeout=5)
        self.path = path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.path)


class Engine:
    def __init__(self, path):
        self.path = path
        self.version = self.get("/version")["ApiVersion"]
        if not re.fullmatch(r"[0-9]+\.[0-9]+", self.version):
            raise ValueError("invalid Docker API version")

    def get(self, path):
        connection = UnixConnection(self.path)
        try:
            connection.request("GET", path)
            response = connection.getresponse()
            if response.status != 200:
                raise ValueError(f"Docker API returned {response.status}")
            return json.loads(response.read(4 * 1024 * 1024))
        finally:
            connection.close()

    def container(self, identity, suffix):
        return self.get(f"/v{self.version}/containers/{quote(identity, safe='')}/{suffix}")


def timestamp(value):
    return datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()


def monitor(args):
    targets = {}
    for target in args.target:
        role, identity = target.split("=", 1)
        if role in targets or not re.fullmatch(r"[a-z][a-z0-9_-]{0,31}", role) or not identity:
            raise ValueError("duplicate or invalid resource target")
        targets[role] = identity
    engine = Engine(args.socket)
    metadata = {}
    def runtime(inspected):
        state = inspected["State"]
        return {"Running": state["Running"], "OOMKilled": state["OOMKilled"], "StartedAt": state["StartedAt"], "RestartCount": inspected["RestartCount"]}
    for role, identity in targets.items():
        inspected = engine.container(identity, "json")
        labels = inspected["Config"].get("Labels") or {}
        metadata[role] = {"containerID": inspected["Id"], "imageID": inspected["Image"],
                          "nanoCPUs": inspected["HostConfig"]["NanoCpus"], "memoryBytes": inspected["HostConfig"]["Memory"],
                          "revision": labels.get("org.opencontainers.image.revision"),
                          "project": labels.get("com.docker.compose.project"), "run": labels.get("com.streamgate.verify.run"), "runtimeBefore": runtime(inspected)}
    args.output.with_suffix(".containers.json").write_text(json.dumps(metadata, sort_keys=True, indent=2) + "\n")
    stopped = False

    def stop(_signal, _frame):
        nonlocal stopped
        stopped = True

    signal.signal(signal.SIGTERM, stop)
    deadline = time.monotonic() + args.timeout
    ready_roles = set()
    with args.output.open("w") as output:
        while not stopped and not args.stop_file.exists():
            if time.monotonic() >= deadline:
                raise ValueError("resource monitor timed out")
            cycle = time.monotonic()
            for role, identity in targets.items():
                if stopped or args.stop_file.exists():
                    break
                row = {"role": role, "containerID": metadata[role]["containerID"], "timestamp": time.time()}
                try:
                    stats = engine.container(identity, "stats?stream=false&one-shot=true")
                    row.update(normalize(stats))
                    row["timestamp"] = timestamp(stats["read"])
                    if row["rss_bytes"] > 0:
                        ready_roles.add(role)
                except (ValueError, OSError, KeyError, http.client.HTTPException) as exc:
                    row["error"] = type(exc).__name__
                output.write(json.dumps(row, sort_keys=True) + "\n")
                output.flush()
            if args.ready_file is not None and ready_roles == set(targets) and not args.ready_file.exists():
                args.ready_file.write_text(json.dumps({"event": "resources.ready", "roles": sorted(ready_roles), "timestamp": time.time()}) + "\n")
            time.sleep(max(0, args.interval - (time.monotonic() - cycle)))
    for role, identity in targets.items():
        metadata[role]["runtimeAfter"] = runtime(engine.container(identity, "json"))
    args.output.with_suffix(".containers.json").write_text(json.dumps(metadata, sort_keys=True, indent=2) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    capture = commands.add_parser("monitor")
    capture.add_argument("--target", action="append", required=True)
    capture.add_argument("--socket", default="/var/run/docker.sock")
    capture.add_argument("--output", required=True, type=Path)
    capture.add_argument("--stop-file", required=True, type=Path)
    capture.add_argument("--ready-file", type=Path)
    capture.add_argument("--timeout", type=float, default=3600)
    capture.add_argument("--interval", type=float, default=1)
    summary = commands.add_parser("summarize")
    summary.add_argument("--raw", required=True, type=Path)
    summary.add_argument("--result", required=True, type=Path)
    summary.add_argument("--output", required=True, type=Path)
    summary.add_argument("--roles", nargs="+", default=["api", "worker", "loader"])
    args = parser.parse_args()
    if args.command == "monitor":
        if not 0 < args.interval <= 1 or args.timeout <= 0:
            raise ValueError("resource monitor needs a positive timeout and interval <=1s")
        monitor(args)
    else:
        playback = json.loads(args.result.read_text())
        rows = [json.loads(line) for line in args.raw.read_text().splitlines()]
        resources = summarize(rows, timestamp(playback["started_at"]), timestamp(playback["finished_at"]), args.roles)
        identities = json.loads(args.raw.with_suffix(".containers.json").read_text())
        config = json.loads(Path("config/performance.acceptance.json").read_text())
        validate_identities(identities, playback, config["resources"])
        args.output.write_text(json.dumps({"commit": playback["commit"], "scenario": playback["scenario"], "viewers": playback["viewers"],
                                           "rss_definition": "cgroup total_rss; cgroup v2 anon+file_mapped", "containers": identities,
                                           "resources": resources}, sort_keys=True, indent=2) + "\n")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, http.client.HTTPException) as exc:
        raise SystemExit(f"resource evidence failed: {exc}")
