#!/usr/bin/env bash
# ============================================================
# StreamGate — unified Docker verification entry
# ============================================================
# Builds the verification toolchain image once (Go + Foundry + FFmpeg + Node +
# golangci-lint) and runs the requested stage inside it, so a clean checkout
# needs no host toolchain. Every stage runs against the same candidate image.
#
#   scripts/verify-docker.sh                 # VERIFY_STAGE=all
#   VERIFY_STAGE=unit scripts/verify-docker.sh
#   make verify-docker VERIFY_STAGE=chain
#
# Stage contract lives in TRANSITION_PLAN.md §5 SG-00. Stages whose backing work
# is not implemented yet fail loudly on purpose — an unimplemented gate must
# never report green.
# ============================================================
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

VERIFY_STAGE="${VERIFY_STAGE:-${1:-all}}"
IMAGE_TAG="${VERIFY_IMAGE:-streamgate-verify:local}"
PROJECT_NAME="${VERIFY_PROJECT:-streamgate-verify-$$}"
ARTIFACT_DIR="${VERIFY_ARTIFACTS:-${ROOT_DIR}/.verify-artifacts}"
LOG_DIR="${ARTIFACT_DIR}/logs"
COMPOSE_FILE="deploy/docker/compose.acceptance.yml"

ALL_STAGES=(unit chain durability ops media perf release)

log()  { printf '\033[1;34m[verify]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[verify:FAIL]\033[0m %s\n' "$*" >&2; exit 1; }

cleanup() {
  # Only ever tear down the project this run created.
  docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

ensure_submodules() {
  if [ -f .gitmodules ]; then
    log "ensuring git submodules are checked out"
    git submodule update --init --recursive
  fi
}

validate_compose() {
  log "validating ${COMPOSE_FILE}"
  docker compose -f "$COMPOSE_FILE" config -q \
    || fail "acceptance compose file is invalid"
}

build_candidate() {
  log "building candidate image ${IMAGE_TAG}"
  docker build -f deploy/docker/Dockerfile.verify -t "$IMAGE_TAG" . \
    || fail "candidate image build failed"
}

# run_in_image <stage> <shell script>
run_in_image() {
  local stage="$1"; shift
  local log_file="${LOG_DIR}/${stage}.log"
  mkdir -p "$LOG_DIR"
  log "stage=${stage} → log ${log_file}"
  if ! docker run --rm \
        --name "${PROJECT_NAME}-${stage}" \
        -v "${ARTIFACT_DIR}:/artifacts" \
        -w /src \
        "${IMAGE_TAG}" bash -lc "$*" 2>&1 | tee "$log_file"; then
    fail "stage '${stage}' failed (log: ${log_file})"
  fi
  log "stage=${stage} PASS"
}

stage_unit() {
  run_in_image unit '
    set -euo pipefail
    echo "== gofmt =="
    unformatted="$(gofmt -l cmd pkg test)"
    if [ -n "$unformatted" ]; then echo "unformatted files:"; echo "$unformatted"; exit 1; fi
    echo "== go vet =="
    go vet ./...
    echo "== go build (default tags) =="
    go build ./...
    echo "== go build (-tags nats_integration) =="
    go build -tags nats_integration ./...
    echo "== go mod tidy is clean =="
    cp go.mod /tmp/go.mod.before; cp go.sum /tmp/go.sum.before
    go mod tidy
    diff -u /tmp/go.mod.before go.mod
    diff -u /tmp/go.sum.before go.sum
    echo "== golangci-lint =="
    golangci-lint run ./...
    echo "== race tests + coverage =="
    go test -race -covermode=atomic -coverprofile=/artifacts/coverage.out ./...
    go tool cover -func=/artifacts/coverage.out | tail -1
  '
}

stage_chain() {
  run_in_image chain '
    set -euo pipefail
    echo "== forge build =="
    ( cd contracts && forge build )
    echo "== forge test =="
    ( cd contracts && forge test )
    echo "== anvil integration + web3 service tests =="
    go test -tags=anvil -race -count=1 ./test/integration/web3/... ./pkg/service/...
  '
}

# Stages whose implementation belongs to later tasks. They fail on purpose so an
# incomplete pipeline can never be mistaken for a green one.
stage_unimplemented() {
  local stage="$1"
  case "$stage" in
    durability) fail "stage 'durability' not implemented yet (SG-12 / SG-13: media task + tx/event restart matrix)" ;;
    ops)        fail "stage 'ops' not implemented yet (SG-10 / SG-14: profile / secret / deadline / SIGTERM / backup-restore)" ;;
    media)      fail "stage 'media' not implemented yet (SG-05 / SG-06: real wallet -> HLS decode + revocation)" ;;
    perf)       fail "stage 'perf' not implemented yet (SG-07: fixed-resource load matrix)" ;;
    release)    fail "stage 'release' not implemented yet (SG-08: same-digest temp deploy / smoke / rollback drill)" ;;
    *)          fail "unknown stage '${stage}'" ;;
  esac
}

run_stage() {
  case "$1" in
    unit) stage_unit ;;
    chain) stage_chain ;;
    durability|ops|media|perf|release) stage_unimplemented "$1" ;;
    *) fail "unknown VERIFY_STAGE '$1' (expected: unit|chain|durability|ops|media|perf|release|all)" ;;
  esac
}

mkdir -p "$ARTIFACT_DIR"
ensure_submodules
validate_compose
build_candidate

if [ "$VERIFY_STAGE" = "all" ]; then
  log "running full DAG: ${ALL_STAGES[*]}"
  for stage in "${ALL_STAGES[@]}"; do
    run_stage "$stage"
  done
else
  run_stage "$VERIFY_STAGE"
fi

log "verification complete (VERIFY_STAGE=${VERIFY_STAGE}); artifacts in ${ARTIFACT_DIR}"
