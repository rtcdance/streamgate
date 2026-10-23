#!/usr/bin/env python3
"""Refuse a formal acceptance run on an environment that cannot carry it.

EFF-00: the final all / full perf / release path needs a dedicated, native
amd64 Linux runner (>=8 CPU, >=32 GiB RAM, >=100 GiB free on the Docker
filesystem) with no other acceptance running on the same daemon. Checking
this before the tool image is built turns a two-hour OOM or a
cross-architecture timing comparison into an immediate, explainable refusal.

Targeted stages (unit, media, ops, ...) keep running on smaller machines:
they are the repair loop, and the plan keeps them available there.

Usage:
  verify_environment.py [--stage all|perf|release|...] [--project NAME]
                        [--root PATH] [--probe FILE]

--probe reads a JSON document instead of querying Docker, so the negative
cases are testable without a 32 GiB machine.
"""
import argparse
import json
import shutil
import subprocess
import sys
from pathlib import Path

GATED_STAGES = ("all", "perf", "release")
MIN_CPU = 8
MIN_MEMORY = 32 * 1024 ** 3
MIN_FREE = 100 * 1024 ** 3
NATIVE_ARCHES = ("x86_64", "amd64")


def run(command):
    result = subprocess.run(command, capture_output=True, text=True)
    if result.returncode != 0:
        raise RuntimeError(f"{' '.join(command)} failed: {result.stderr.strip()}")
    return result.stdout


def free_bytes(path):
    probe = Path(path)
    while not probe.exists() and probe != probe.parent:
        probe = probe.parent
    usage = shutil.disk_usage(probe)
    return usage.free


def collect(project):
    info = json.loads(run(["docker", "info", "--format", "{{json .}}"]))
    projects = run([
        "docker", "ps", "--filter", "label=com.streamgate.verify.run",
        "--format", '{{.Label "com.streamgate.verify.run"}}',
    ]).split()
    return {
        "os_type": info.get("OSType", ""),
        "architecture": info.get("Architecture", ""),
        "ncpu": info.get("NCPU", 0),
        "mem_total_bytes": info.get("MemTotal", 0),
        "docker_root_dir": info.get("DockerRootDir", "/"),
        "free_bytes": free_bytes(info.get("DockerRootDir") or "/"),
        "running_projects": sorted({p for p in projects if p and p != project}),
    }


def evaluate(probe):
    """Return the list of unmet requirements; empty means qualified."""
    failures = []
    if probe.get("os_type") not in ("linux", None, ""):
        failures.append(
            f"daemon OS is {probe.get('os_type')!r}; a native linux/amd64 runner is required"
        )
    arch = str(probe.get("architecture", "")).lower()
    if arch not in NATIVE_ARCHES:
        failures.append(
            f"daemon architecture is {probe.get('architecture')!r}; "
            "native linux/amd64 is required (no emulation)"
        )
    ncpu = int(probe.get("ncpu") or 0)
    if ncpu < MIN_CPU:
        failures.append(f"{ncpu} CPUs; at least {MIN_CPU} are required")
    memory = int(probe.get("mem_total_bytes") or 0)
    if memory < MIN_MEMORY:
        failures.append(
            f"{memory / 1024 ** 3:.1f} GiB RAM; at least {MIN_MEMORY / 1024 ** 3:.0f} GiB are required"
        )
    free = int(probe.get("free_bytes") or 0)
    if free < MIN_FREE:
        failures.append(
            f"{free / 1024 ** 3:.1f} GiB free on the Docker filesystem "
            f"({probe.get('docker_root_dir')}); at least {MIN_FREE / 1024 ** 3:.0f} GiB are required"
        )
    others = [p for p in probe.get("running_projects") or [] if p]
    if others:
        failures.append(
            "another acceptance is already running on this daemon: " + ", ".join(others)
        )
    return failures


def gated(stage):
    return stage in GATED_STAGES or any(
        part in GATED_STAGES for part in stage.split(",")
    )


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--stage", default="all")
    parser.add_argument("--project", default="")
    parser.add_argument("--probe", type=Path)
    args = parser.parse_args()

    if not gated(args.stage):
        print(f"[env] stage={args.stage}: no dedicated-runner requirement; skipping capacity gate")
        return 0

    if args.probe:
        probe = json.loads(args.probe.read_text())
    else:
        try:
            probe = collect(args.project)
        except (RuntimeError, OSError, ValueError) as error:
            print(f"[env:FAIL] could not read the Docker environment: {error}", file=sys.stderr)
            return 1

    failures = evaluate(probe)
    if not failures:
        print(
            "[env] qualified runner: "
            f"{probe.get('ncpu')} CPU, {int(probe.get('mem_total_bytes') or 0) / 1024 ** 3:.1f} GiB, "
            f"{int(probe.get('free_bytes') or 0) / 1024 ** 3:.1f} GiB free, {probe.get('architecture')}"
        )
        return 0

    print(
        "[env:FAIL] this environment cannot carry VERIFY_STAGE=" + args.stage + ":",
        file=sys.stderr,
    )
    for failure in failures:
        print(f"  - {failure}", file=sys.stderr)
    print(
        "  terminal acceptance stays BLOCKED here; run targeted stages "
        "(unit, media, ops, ...) on this machine instead",
        file=sys.stderr,
    )
    return 1


if __name__ == "__main__":
    sys.exit(main())
