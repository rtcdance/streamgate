#!/usr/bin/env bash
# Fetch an evidence directory from the remote dev machine and verify it.
# Usage:
#   bash remote-fetch.sh build/acceptance/v1.0.2/evidence-pack [local-dir]
set -euo pipefail

HOST="${REMOTE_HOST:-prod-srs}"
DIR="${REMOTE_DIR:-/home/streamgate}"
REMOTE_PATH="${1:-}"
[ -n "$REMOTE_PATH" ] || { echo "usage: $0 <remote-path> [local-dir]" >&2; exit 2; }
LOCAL="${2:-build/remote-evidence/$(basename "$REMOTE_PATH")}"

mkdir -p "$(dirname "$LOCAL")"
rsync -az "$HOST:$DIR/$REMOTE_PATH/" "$LOCAL/"
echo "fetched $HOST:$DIR/$REMOTE_PATH -> $LOCAL"

if [ -f "$LOCAL/manifest.json" ]; then
  echo "-- manifest --"
  jq '{complete, missing, artifacts:(.artifacts|length), provenance:.provenance.commit_sha, tree_dirty:.provenance.tree_dirty}' "$LOCAL/manifest.json" 2>/dev/null || true
fi
if [ -f "$LOCAL/checksums.txt" ]; then
  echo "-- checksums --"
  (cd "$LOCAL" && shasum -a 256 -c checksums.txt | tail -5)
fi
