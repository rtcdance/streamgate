#!/usr/bin/env bash
# ============================================================
# StreamGate SG-07 — perf run manifest
# ============================================================
# Usage: perf-run-manifest.sh <result_dir> <compose_project> [compose_file]
#
# Writes <result_dir>/environment.json: the environment the perf numbers were
# produced in. SG-07 step 2 requires the measurement to record "平台 / 架构、
# 所有资源限额、工具 / 镜像版本、提交 SHA、媒体参数与 hash" and §7 requires a
# structured manifest; the result JSONs carry only commit / fixture hash / media
# parameters, so the rest was previously unrecorded — a throughput number whose
# resource budget could not be looked up.
#
# Limits are READ BACK FROM THE RUNNING CONTAINERS (docker inspect), not copied
# from the compose file: the manifest must describe what was actually measured,
# so an unpinned service shows its real (unbounded) values instead of the file's
# intention.
# ============================================================
set -euo pipefail

RESULT_DIR="${1:?usage: perf-run-manifest.sh <result_dir> <compose_project> [compose_file]}"
PROJECT="${2:?usage: perf-run-manifest.sh <result_dir> <compose_project> [compose_file]}"
COMPOSE_FILE="${3:-deploy/docker/compose.verify.yml}"
[ -d "$RESULT_DIR" ] || { echo "perf-run-manifest: FAIL: $RESULT_DIR does not exist" >&2; exit 1; }

HOST_OS="$(uname -s 2>/dev/null || echo unknown)"
HOST_ARCH="$(uname -m 2>/dev/null || echo unknown)"
HOST_KERNEL="$(uname -r 2>/dev/null || echo unknown)"
COMMIT="$(git rev-parse HEAD)" || { echo "perf-run-manifest: FAIL: cannot resolve HEAD" >&2; exit 1; }

# `docker info` describes the VM the containers actually run in, which is the
# budget the stack really had; the host's own CPU count is not it (Docker
# Desktop on macOS runs containers in a VM with its own allocation).
DOCKER_INFO="$(docker info --format '{{.OSType}} {{.Architecture}} {{.NCPU}} {{.MemTotal}} {{.ServerVersion}}' 2>/dev/null)" \
  || DOCKER_INFO="unknown unknown 0 0 unknown"

# One inspection per container of THIS run's project, labelled by compose
# service so the numbers can be attributed to monolith vs worker vs infra.
# `-a`, not just running: the fault matrix deliberately STOPS the anvil and its
# proxy, and a stopped container still carries the limits it ran under — using
# only running containers would silently drop the services the faults touched.
CONTAINERS="$(
  for id in $(docker ps -aq --filter "label=com.docker.compose.project=${PROJECT}"); do
    docker inspect \
      --format '{{index .Config.Labels "com.docker.compose.service"}}|{{.Config.Image}}|{{.Image}}|{{.HostConfig.NanoCpus}}|{{.HostConfig.Memory}}' \
      "$id"
  done
)"
[ -n "$CONTAINERS" ] || { echo "perf-run-manifest: FAIL: no running containers for project ${PROJECT}" >&2; exit 1; }

# Tool versions: whichever of the named tools this runner has. A missing tool is
# recorded as such rather than silently omitted — "recorded versions" must be
# readable, not inferred.
tool_version() {
  if command -v "$1" >/dev/null 2>&1; then
    { "$@" 2>/dev/null || true; } | head -1
  else
    echo "not found"
  fi
}
GO_VERSION="$(tool_version go version)"
FFMPEG_VERSION="$(tool_version ffmpeg -version)"
FORGE_VERSION="$(tool_version forge --version)"
NODE_VERSION="$(tool_version node --version)"
DOCKER_CLI_VERSION="$(tool_version docker --version)"

CONTAINERS="$CONTAINERS" DOCKER_INFO="$DOCKER_INFO" \
COMMIT="$COMMIT" HOST_OS="$HOST_OS" HOST_ARCH="$HOST_ARCH" HOST_KERNEL="$HOST_KERNEL" \
COMPOSE_FILE="$COMPOSE_FILE" PROJECT="$PROJECT" \
GO_VERSION="$GO_VERSION" FFMPEG_VERSION="$FFMPEG_VERSION" FORGE_VERSION="$FORGE_VERSION" \
NODE_VERSION="$NODE_VERSION" DOCKER_CLI_VERSION="$DOCKER_CLI_VERSION" \
python3 - "$RESULT_DIR/environment.json" <<'PY'
import json, os, sys

out = sys.argv[1]
docker = os.environ["DOCKER_INFO"].split()
services = {}
for line in os.environ["CONTAINERS"].splitlines():
    if not line.strip():
        continue
    service, image, image_id, nano_cpus, memory = line.split("|")
    services[service] = {
        "image": image,
        "image_id": image_id,
        "limits": {
            # Docker reports "no limit" as 0; record it as unbounded rather than
            # as a zero budget.
            "cpus": (int(nano_cpus) / 1e9) or "unbounded",
            "memory_bytes": int(memory) if int(memory) else "unbounded",
        },
    }

manifest = {
    "commit": os.environ["COMMIT"],
    "compose_file": os.environ["COMPOSE_FILE"],
    "compose_project": os.environ["PROJECT"],
    "host": {
        "os": os.environ["HOST_OS"],
        "arch": os.environ["HOST_ARCH"],
        "kernel": os.environ["HOST_KERNEL"],
        "docker_platform": docker[0] + "/" + docker[1],
        "docker_vm_cpus": int(docker[2]),
        "docker_vm_memory_bytes": int(docker[3]),
    },
    "services": services,
    "tools": {
        "go": os.environ["GO_VERSION"],
        "ffmpeg": os.environ["FFMPEG_VERSION"],
        "forge": os.environ["FORGE_VERSION"],
        "node": os.environ["NODE_VERSION"],
        "docker_cli": os.environ["DOCKER_CLI_VERSION"],
    },
    # The load generator runs on the host, not as a container: its "budget" is
    # the host's own CPU, and the harness caps itself at 2 concurrent media
    # requests per viewer (SG-07 step 2) rather than by a cgroup.
    "loader": {
        "kind": "host go test -tags load (test/load)",
        "budget": "host CPU, no cgroup limit",
        "max_parallel_media_requests_per_viewer": 2,
    },
}

with open(out, "w") as fh:
    json.dump(manifest, fh, indent=2, sort_keys=True)
    fh.write("\n")
print(f"perf-run-manifest: wrote {out} ({len(services)} service(s), commit {os.environ['COMMIT'][:12]})")
PY
