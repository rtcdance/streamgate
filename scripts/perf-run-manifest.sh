#!/usr/bin/env bash
set -euo pipefail
python3 scripts/cd/perf_environment.py "${1:?result directory required}" "${2:?project required}" "${3:-deploy/docker/compose.verify.yml}"
