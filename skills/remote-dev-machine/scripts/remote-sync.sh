#!/usr/bin/env bash
# Sync the local clean worktree to the remote dev machine.
# Default is git mode and dry-run for rsync.
# Usage:
#   bash remote-sync.sh --mode git [--branch BRANCH]
#   bash remote-sync.sh --mode rsync --dry-run
#   bash remote-sync.sh --mode rsync --yes [--delete]
set -euo pipefail

MODE="git"
HOST="${REMOTE_HOST:-prod-srs}"
DIR="${REMOTE_DIR:-/home/streamgate}"
BRANCH="${REMOTE_BRANCH:-$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo HEAD)}"
APPLY=0
DELETE=0

usage() {
  echo "usage: $0 --mode git|rsync [--branch B] [--dry-run|--yes] [--delete]" >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --mode) MODE="$2"; shift 2 ;;
    --branch) BRANCH="$2"; shift 2 ;;
    --host) HOST="$2"; shift 2 ;;
    --dir) DIR="$2"; shift 2 ;;
    --dry-run) APPLY=0; shift ;;
    --yes) APPLY=1; shift ;;
    --delete) DELETE=1; shift ;;
    *) usage ;;
  esac
done

case "$MODE" in
  git)
    if [ "$BRANCH" = "HEAD" ] || ! git symbolic-ref -q HEAD >/dev/null 2>&1; then
      echo "ERROR: detached HEAD; pass --branch <branch> to sync a real branch." >&2
      exit 1
    fi
    if [ -n "$(git status --porcelain)" ]; then
      echo "ERROR: local worktree is dirty; commit/stash first, or use --mode rsync for WIP." >&2
      git status --short | head -20 >&2
      exit 1
    fi
    echo "git sync: $(git rev-parse --short HEAD) -> $HOST:$DIR ($BRANCH)"
    if [ "$APPLY" -eq 0 ]; then
      echo "DRY-RUN: git push origin HEAD:refs/heads/$BRANCH"
      echo "DRY-RUN: ssh $HOST 'cd $DIR && git fetch origin && git checkout $BRANCH && git pull --ff-only'"
      exit 0
    fi
    git push origin "HEAD:refs/heads/$BRANCH"
    ssh -o BatchMode=yes -o ConnectTimeout=8 "$HOST" \
      "cd '$DIR' && git fetch origin && git checkout '$BRANCH' && git pull --ff-only"
    ;;
  rsync)
    echo "WARNING: rsync writes the remote working tree; the git clone will show modified files." >&2
    echo "         Prefer REMOTE_DIR=/home/streamgate-wip for WIP syncs." >&2
    EXCLUDES=(--exclude '.git' --exclude '.claude' --exclude 'skills-lock.json' \
      --exclude 'streamgate' --exclude 'build' --exclude 'node_modules' \
      --exclude 'frontend/node_modules' --exclude '.env*' --exclude '*.log' --exclude '.verify-artifacts' --exclude 'contracts/lib' --exclude 'h5-demo/node_modules' --exclude 'scripts/h5-demo-acceptance/node_modules' --exclude '.workbuddy' \
      --exclude '.DS_Store' --exclude '.codegraph' --exclude '.omo' --exclude '.codebuddy')
    ARGS=(-az "${EXCLUDES[@]}")
    [ "$DELETE" -eq 1 ] && ARGS+=(--delete)
    if [ "$APPLY" -eq 0 ]; then
      echo "DRY-RUN rsync to $HOST:$DIR"
      rsync "${ARGS[@]}" --dry-run --itemize-changes ./ "$HOST:$DIR/"
      echo "Re-run with --yes to apply."
      exit 0
    fi
    if [ "$DELETE" -eq 1 ] && [ "${CONFIRM_DELETE:-}" != "yes" ]; then
      echo "ERROR: --delete requires CONFIRM_DELETE=yes (destructive)." >&2
      exit 1
    fi
    ssh -o BatchMode=yes -o ConnectTimeout=8 "$HOST" "mkdir -p '$DIR'"
    rsync "${ARGS[@]}" ./ "$HOST:$DIR/"
    ;;
  *) usage ;;
esac
