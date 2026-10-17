#!/usr/bin/env bash
# Every stage runs inside the pinned verification controller. The host only
# invokes Shell, Git and Docker; project services never publish host ports.
set -euo pipefail

ROOT_DIR="${VERIFY_SOURCE_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
cd "$ROOT_DIR"
if [ "${VERIFY_IN_CONTROLLER:-}" != 1 ]; then
  exec bash "$ROOT_DIR/scripts/verify-controller.sh" "$@"
fi
git config --global --add safe.directory "$ROOT_DIR"

VERIFY_STAGE="${VERIFY_STAGE:-${1:-all}}"
if [ "$VERIFY_STAGE" = all ]; then
  source_status="$(GIT_OPTIONAL_LOCKS=0 git status --porcelain --untracked-files=normal)" || exit 1
  if [ -n "$source_status" ]; then
    echo 'complete acceptance requires a frozen clean candidate' >&2
    exit 1
  fi
fi
IMAGE_TAG="${VERIFY_TOOL_IMAGE_ID:-${VERIFY_IMAGE:-streamgate-verify:local}}"
PROJECT_NAME="${VERIFY_PROJECT:-streamgate-verify-$$}"
ARTIFACT_DIR="${VERIFY_ARTIFACTS:-${ROOT_DIR}/.verify-artifacts}"
LOG_DIR="${ARTIFACT_DIR}/logs"
COMPOSE_FILE="deploy/docker/compose.acceptance.yml"
VERIFY_COMPOSE_FILE="deploy/docker/compose.verify.yml"
# Space-separated list of compose projects this run created; the cleanup trap
# removes exactly these and nothing else.
CREATED_PROJECTS=""
PROXY_PIDS=""
BUILT_BINARIES=()
REFERENCE_WORKTREE=""
CANDIDATE_PREFIX="${VERIFY_CANDIDATE_PREFIX:-${PROJECT_NAME}-candidate}"
CANDIDATE_BUILT=0
CANDIDATE_MANIFEST="${VERIFY_CANDIDATE_MANIFEST:-${ARTIFACT_DIR}/candidate.json}"
if [ -n "${VERIFY_CANDIDATE_MANIFEST:-}" ]; then CANDIDATE_BUILT=1; fi

ALL_STAGES=(unit chain durability ops media perf release)

log()  { printf '\033[1;34m[verify]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[verify:FAIL]\033[0m %s\n' "$*" >&2; exit 1; }

require_controller_tool() {
  command -v "$1" >/dev/null 2>&1 || fail "controller tool '$1' is required but not found"
}

# platform_variant_available <image> — is the requested platform variant usable
# from the local store? `docker image inspect --platform` is not a reliable
# answer on every engine: on this host (Docker 29 desktop-linux, classic image
# store) it exits 0 and prints an empty Os/Arch for an index-only reference that
# is stored as a foreign architecture, so trusting it returns early and the
# later `docker run --platform` fails against the native image — observed as
# "image with reference nginx:alpine was found but does not provide the
# specified platform (linux/amd64)" at the H5 proxy step. `docker create`
# resolves the variant exactly the way `docker run` will, without starting it
# and without contacting a registry.
platform_variant_available() {
  local id
  id="$(timeout 60 docker create --pull never --platform "$VERIFY_PLATFORM" "$1" 2>/dev/null)" || return 1
  docker rm -f "$id" >/dev/null 2>&1 || :
  return 0
}

ensure_runtime_image() {
  local image="$1" attempt
  [[ "$image" =~ @sha256:[0-9a-f]{64}$ ]] || fail "runtime image must use an immutable SHA256 reference: ${image}"
  if platform_variant_available "$image"; then return; fi
  for attempt in 1 2 3; do
    if timeout 300 docker pull --platform "$VERIFY_PLATFORM" "$image" \
      && platform_variant_available "$image"; then
      return
    fi
    log "pinned runtime image not usable for ${VERIFY_PLATFORM} yet (attempt ${attempt}/3): ${image}"
  done
  fail "could not prepare pinned runtime image for ${VERIFY_PLATFORM}: ${image}"
}

cleanup() {
  local rc=$? cleanup_rc=0 p pid binary
  trap - EXIT INT TERM
  for pid in $PROXY_PIDS; do kill "$pid" >/dev/null 2>&1 || :; done
  for binary in ${BUILT_BINARIES[@]+"${BUILT_BINARIES[@]}"}; do rm -f "$binary" || cleanup_rc=1; done
  if [ -n "${REFERENCE_WORKTREE:-}" ]; then
    git worktree remove --force "$REFERENCE_WORKTREE" >/dev/null 2>&1 || cleanup_rc=1
  fi
  docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" down -v --remove-orphans >/dev/null 2>&1 || cleanup_rc=1
  for p in $CREATED_PROJECTS; do
    docker network disconnect "${p}_default" "$VERIFY_CONTROLLER" >/dev/null 2>&1 || :
    docker compose -p "$p" -f "$VERIFY_COMPOSE_FILE" down -v --remove-orphans >/dev/null 2>&1 || cleanup_rc=1
    local ids
    ids="$(docker ps -aq --filter "label=com.docker.compose.project=${p}")" || { ids=""; cleanup_rc=1; }
    if [ -n "$ids" ]; then docker rm -f $ids >/dev/null 2>&1 || cleanup_rc=1; fi
  done
  if [ "$cleanup_rc" != 0 ]; then
    printf '%s\n' "verify cleanup failed; original exit=${rc}" >&2
    if [ "$rc" = 0 ]; then rc=1; fi
  fi
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

ensure_submodules() {
  if [ -f .gitmodules ]; then
    log "ensuring git submodules are checked out"
    git submodule update --init --recursive
  fi
}

validate_compose() {
  log "validating ${COMPOSE_FILE} and ${VERIFY_COMPOSE_FILE}"
  docker compose -f "$COMPOSE_FILE" config -q \
    || fail "acceptance compose file is invalid"
  docker compose -f "$VERIFY_COMPOSE_FILE" config -q \
    || fail "verify compose file is invalid"
}

build_candidate() {
  log "building candidate image ${IMAGE_TAG}"
  # VERIFY_GOPROXY overrides the image's default module source (direct VCS).
  # Restricted networks that block both proxy.golang.org and raw git clones
  # can pass a reachable proxy, e.g. VERIFY_GOPROXY=https://goproxy.cn,direct.
  local proxy_args=()
  if [ -n "${VERIFY_GOPROXY:-}" ]; then
    log "using GOPROXY=${VERIFY_GOPROXY}"
    proxy_args=(--build-arg "GOPROXY=${VERIFY_GOPROXY}")
  fi
  # VERIFY_PLATFORM forces the toolchain image onto a specific platform (e.g.
  # linux/amd64 on an arm64 host) so the Dockerfile.verify solc prefetch takes
  # the deterministic linux-amd64 path that CI uses.
  local platform_args=()
  if [ -n "${VERIFY_PLATFORM:-}" ]; then
    log "building toolchain image for platform ${VERIFY_PLATFORM}"
    platform_args=(--platform "${VERIFY_PLATFORM}")
  fi
  # ${arr[@]+...} keeps an empty array safe under `set -u` on bash 3.2.
  docker build -f deploy/docker/Dockerfile.verify \
    ${platform_args[@]+"${platform_args[@]}"} \
    ${proxy_args[@]+"${proxy_args[@]}"} -t "$IMAGE_TAG" . \
    || fail "candidate image build failed"
}

# run_in_image <stage> <shell script>
# scripts/ and test/ are excluded from the image by the repo-root .dockerignore
# (it exists for the runtime images), so both are bind-mounted read-only from
# the same checkout the image was built from — same commit, no second copy.
# When the acceptance compose project's network exists (durability brings it
# up first), the container joins it so a STREAMGATE_TEST_DB_DSN whose host is
# "postgres" resolves to the project's Postgres by service name.
run_in_image() {
  local stage="$1"; shift
  local log_file="${LOG_DIR}/${stage}.log"
  local net_args=()
  local platform_args=()
  mkdir -p "$LOG_DIR"
  if docker network inspect "${PROJECT_NAME}_default" >/dev/null 2>&1; then
    net_args=(--network "${PROJECT_NAME}_default")
  fi
  if [ -n "${VERIFY_PLATFORM:-}" ]; then
    platform_args=(--platform "${VERIFY_PLATFORM}")
  fi
  log "stage=${stage} → log ${log_file}"
  set +e
  docker run --rm --pull never \
        --name "${PROJECT_NAME}-${stage}" \
        --label "com.docker.compose.project=${PROJECT_NAME}" \
        --label "com.streamgate.verify.run=${PROJECT_NAME}" \
        ${platform_args[@]+"${platform_args[@]}"} \
        ${net_args[@]+"${net_args[@]}"} \
        -v "${ARTIFACT_DIR}:/artifacts" \
        -v "${ROOT_DIR}/scripts:/src/scripts:ro" \
        -v "${ROOT_DIR}/test:/src/test:ro" \
        -w /src \
        "${IMAGE_TAG}" bash -lc "$1" 2>&1 | tee "$log_file"
  local statuses=("${PIPESTATUS[@]}")
  set -e
  local rc="${statuses[0]}"
  if [ "$rc" = 0 ]; then rc="${statuses[1]}"; fi
  if [ "$rc" != 0 ]; then
    log "stage '${stage}' failed with exit ${rc} (log: ${log_file})"
    exit "$rc"
  fi
  log "stage=${stage} PASS"
}

# run_logged <stage> <command...> — runs the command in the CURRENT shell
# (functions below read globals like VERIFY_H5_PORT, which a fresh `bash -c`
# subshell would not inherit), teeing the output to the stage's log file and
# failing loudly on a non-zero exit.
run_logged() {
  local stage="$1"; shift
  local log_file="${LOG_DIR}/${stage}.log"
  mkdir -p "$LOG_DIR"
  : > "$log_file"   # a re-run must not append to the previous run's log
  log "stage=${stage} (controller) → log ${log_file}"
  # A function in `if ! function | tee` loses errexit throughout its body.
  # Use an independent shell context and preserve the original exit status.
  set +e
  ( set -e; "$@" ) 2>&1 | tee -a "$log_file"
  local statuses=("${PIPESTATUS[@]}")
  set -e
  local rc="${statuses[0]}"
  if [ "$rc" = 0 ]; then rc="${statuses[1]}"; fi
  if [ "$rc" != 0 ]; then
    log "stage '${stage}' failed with exit ${rc} (log: ${log_file})"
    exit "$rc"
  fi
  log "stage=${stage} PASS"
}

stage_unit() {
  run_in_image unit '
    set -euo pipefail
    python3 scripts/cd/test_assert_required_tests.py
    python3 scripts/cd/test_verify_proxy.py
    python3 scripts/cd/test_candidate_manifest.py
    python3 scripts/cd/test_image_identity.py
    python3 scripts/cd/test_verify_shell.py
    python3 scripts/cd/test_assert_candidate_evidence.py
    node scripts/cd/test_h5_api_base.cjs
    echo "== forge build (ABI-sync guard needs the artifacts) =="
    ( cd contracts && forge build )
    echo "== gofmt =="
    unformatted="$(gofmt -l cmd pkg test)"
    if [ -n "$unformatted" ]; then echo "unformatted files:"; echo "$unformatted"; exit 1; fi
    echo "== go vet =="
    go vet ./...
    echo "== go build (default tags) =="
    go build ./...
    echo "== go build (-tags nats_integration) =="
    go build -tags nats_integration ./...
    echo "== tag-gated compile precheck (anvil / load / testnet) =="
    # Build-tagged test files are invisible to the checks above, so a call-site
    # drift inside one (an API that gained a parameter) survives every default
    # build and only explodes in the chain stage ten minutes later. Vet each tag
    # here, where the feedback is immediate and the cause is obvious.
    go vet -tags anvil ./test/... ./pkg/...
    go vet -tags load ./test/load/
    go vet -tags testnet ./test/...
    go vet -tags e2e ./test/e2e/
    go vet -tags fork ./test/... ./pkg/...
    go vet -tags fullchain ./test/... ./pkg/...
    # k8s is deliberately absent: its tag pulls k8s.io/kube-openapi, which does
    # not compile against the yaml dependency pinned by this module (upstream
    # drift, unrelated to this repository code).
    echo "== go mod tidy is clean =="
    cp go.mod /tmp/go.mod.before; cp go.sum /tmp/go.sum.before
    go mod tidy
    diff -u /tmp/go.mod.before go.mod
    diff -u /tmp/go.sum.before go.sum
    echo "== golangci-lint =="
    golangci-lint run ./...
    echo "== race tests + coverage (same gate as make test-ci and CI) =="
    go test -race -covermode=atomic -coverprofile=/artifacts/coverage.out $(./scripts/ci-packages.sh)
    ./scripts/check-coverage.sh /artifacts/coverage.out "${COVERAGE_MIN:-60}"
    echo "== coverage gate self-test (the gate must be able to fail) =="
    bash scripts/check-coverage_test.sh
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

# ---------------------------------------------------------------------------
# durability (SG-12 / SG-13): the real task / outbox / tx / event-checkpoint
# restart matrix — DB-backed Go suites that SKIP without STREAMGATE_TEST_DB_DSN.
# A scratch Postgres comes up on the acceptance compose project, the tests run
# INSIDE the toolchain image on that project's network (container aliased
# postgres), and the required-test assertion turns "skipped" into a failure.
# ---------------------------------------------------------------------------
stage_durability() {
  require_controller_tool docker
  # Postgres only — the same scratch service the CI durability job provides.
  # The DB-backed suites that need Redis/MinIO skip on their own and are not
  # part of the required list.
  log "bringing up scratch Postgres on project ${PROJECT_NAME}"
  local pg_image
  pg_image="$(docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" config --format json \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["services"]["postgres"]["image"])')" || fail "cannot resolve scratch Postgres image"
  ensure_runtime_image "$pg_image"
  docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" up -d --pull never --wait --wait-timeout 120 postgres \
    || fail "could not start the scratch Postgres"
  # The toolchain image ships no psql, so the scratch database is provisioned
  # from the host through the project's postgres container; the tests then
  # reach it by service name over the project network.
  log "provisioning scratch database streamgate_durability"
  # DROP/CREATE as separate -c calls: psql wraps multiple -c statements in one
  # transaction, and DROP DATABASE cannot run inside a transaction block.
  docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" exec -T postgres \
    psql -U postgres -d postgres -c "DROP DATABASE IF EXISTS streamgate_durability" \
    || fail "could not drop the scratch database"
  docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" exec -T postgres \
    psql -U postgres -d postgres -c "CREATE DATABASE streamgate_durability" \
    || fail "could not create the scratch database"
  run_in_image durability '
    set -euo pipefail
    export STREAMGATE_TEST_DB_DSN="postgres://postgres:postgres@postgres:5432/streamgate_durability?sslmode=disable"
    echo "== DB-backed durability suites (transcoding/registry/event/integration) =="
    go_test_rc=0
    go test -race -count=1 -json \
      ./pkg/service/transcoding ./pkg/service/registry ./pkg/web3/event ./pkg/service ./test/integration/... \
      > /artifacts/durability-tests.json || go_test_rc=$?
    python3 scripts/cd/assert_required_tests.py \
      --suite durability-tests --exit-code "$go_test_rc" \
      /artifacts/durability-tests.json
    echo "== anvil-tagged DB-backed content lifecycle suite (self-contained chain) =="
    # The anvil suite loads contracts/out/*.json, so forge artifacts must exist
    # before it runs (the same prerequisite CI pins).
    ( cd contracts && forge build )
    go_test_rc=0
    go test -tags=anvil -race -count=1 -json ./test/integration \
      > /artifacts/durability-anvil.json || go_test_rc=$?
    python3 scripts/cd/assert_required_tests.py \
      --suite durability-anvil --exit-code "$go_test_rc" \
      /artifacts/durability-anvil.json
  '
}

# ---------------------------------------------------------------------------
# ops (SG-10 / SG-14): production profile / secret / deadline / SIGTERM /
# backup-recovery semantics plus a short fuzz pass over the seed corpus. All
# in-image, no stack needed.
# ---------------------------------------------------------------------------
stage_ops() {
  run_in_image ops '
    set -euo pipefail
    echo "== ops/profile/drain gates (same filters as the CI ops job) =="
    go_test_rc=0
    go test -race -count=1 -json \
      -run "TestBuildProviders_Production|TestBuildProviders_DevStillFallsBackToMemory|TestSetupRouter_Production|TestSetDraining|TestIsDraining|TestDrainMiddleware" \
      ./pkg/gateway ./pkg/core \
      > /artifacts/ops-gateway-core.json || go_test_rc=$?
    python3 scripts/cd/assert_required_tests.py \
      --suite ops-gateway-core --exit-code "$go_test_rc" \
      /artifacts/ops-gateway-core.json

    go_test_rc=0
    go test -race -count=1 -json \
      -run "TestValidateProduction|TestIsProduction|TestCapabilityPolicy|TestDeriveProfile" \
      ./pkg/core/config \
      > /artifacts/ops-config.json || go_test_rc=$?
    python3 scripts/cd/assert_required_tests.py \
      --suite ops-config --exit-code "$go_test_rc" \
      /artifacts/ops-config.json

    echo "== SG-14 ops acceptance suites (protected endpoints, rate limit, drain/health, metrics) =="
    go_test_rc=0
    go test -race -count=1 -json \
      -run "TestAcceptance_ProtectedOpsEndpoints|TestAcceptance_MediaRateLimitAndNAT|TestAcceptance_StreamingDrain503Health200" \
      ./pkg/gateway \
      > /artifacts/ops-gateway-sg14.json || go_test_rc=$?
    python3 scripts/cd/assert_required_tests.py \
      --suite ops-gateway-sg14 --exit-code "$go_test_rc" \
      /artifacts/ops-gateway-sg14.json

    go_test_rc=0
    go test -race -count=1 -json \
      -run "TestAcceptance_GateDecisionMetricRecorded" \
      ./pkg/middleware \
      > /artifacts/ops-middleware.json || go_test_rc=$?
    python3 scripts/cd/assert_required_tests.py \
      --suite ops-middleware --exit-code "$go_test_rc" \
      /artifacts/ops-middleware.json

    go_test_rc=0
    go test -race -count=1 -json \
      -run "TestMonitoring_NoHighCardinalityLabels" \
      ./pkg/monitoring \
      > /artifacts/ops-monitoring.json || go_test_rc=$?
    python3 scripts/cd/assert_required_tests.py \
      --suite ops-monitoring --exit-code "$go_test_rc" \
      /artifacts/ops-monitoring.json

    echo "== SG-14 key rotation / audience separation suite =="
    go_test_rc=0
    go test -race -count=1 -json \
      -run "TestAcceptance_KeyRotationAndAudience" \
      ./pkg/service \
      > /artifacts/ops-keyrotation.json || go_test_rc=$?
    python3 scripts/cd/assert_required_tests.py \
      --suite ops-keyrotation --exit-code "$go_test_rc" \
      /artifacts/ops-keyrotation.json

    echo "== short fuzz pass over every Fuzz target (seed corpus + 30s mutation) =="
    fuzz_files="$(grep -rl "^func Fuzz" --include="*_test.go" pkg test 2>/dev/null || true)"
    if [ -z "$fuzz_files" ]; then
      echo "GATE FAILURE: the fuzz contract names Fuzz targets but none exist under pkg/ or test/"
      exit 1
    fi
    for f in $fuzz_files; do
      d="$(dirname "$f")"
      for t in $(grep -h "^func Fuzz" "$f" | sed -E "s/^func (Fuzz[A-Za-z0-9_]+).*/\1/"); do
        echo "-- fuzz $d/$t (30s) --"
        go test -run "^$" -fuzz "^${t}\$" -fuzztime 30s "./$d"
      done
    done
  '
}

# ---------------------------------------------------------------------------
# Shared controller plumbing for the fullstack stages (media / perf / release):
# a project-scoped stack from deploy/docker/compose.verify.yml.
# ---------------------------------------------------------------------------

# wait_health <url> <deadline-seconds>
wait_health() {
  local url="$1" deadline=$((SECONDS + $2))
  while :; do
    [ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "$url" 2>/dev/null || echo 000)" = "200" ] && return 0
    [ "$SECONDS" -ge "$deadline" ] && return 1
    sleep 3
  done
}

# Build the candidate once. References have a separate manifest and can never
# replace the candidate's identity or become its rollback baseline implicitly.
prebuild_verify_stack() {
  local project="$1" src="$ROOT_DIR" prefix="$CANDIDATE_PREFIX" manifest="$CANDIDATE_MANIFEST"
  local revision service binary archive
  if [ -n "${VERIFY_BUILD_REF:-}" ]; then
    src="$(prepare_reference_worktree "$VERIFY_BUILD_REF")"
    REFERENCE_WORKTREE="$src"
    prefix="${project}-reference"
    manifest="${ARTIFACT_DIR}/reference.json"
  elif [ "$CANDIDATE_BUILT" = 1 ]; then
    python3 scripts/cd/candidate_manifest.py "$manifest" --revision "$(git rev-parse HEAD)" --platform "$VERIFY_PLATFORM" || fail "candidate identity changed"
    VERIFY_IMAGE_PREFIX="$prefix"
    export VERIFY_IMAGE_PREFIX
    return
  fi
  revision="$(git -C "$src" rev-parse HEAD)"
  log "building shared images for ${revision} inside verification controller"
  for service in monolith api-gateway transcoder; do
    case "$service" in
      monolith) binary=./cmd/monolith/streamgate ;;
      *) binary="./cmd/microservices/${service}" ;;
    esac
    [ ! -e "$src/deploy/docker/${service}-linux" ] || fail "generated binary already exists: ${service}-linux"
    BUILT_BINARIES=("$src/deploy/docker/${service}-linux")
    ( cd "$src" && CGO_ENABLED=0 GOOS=linux \
      go build -tags nats_integration -trimpath -o "deploy/docker/${service}-linux" "$binary" ) \
      || fail "${service} compile failed"
    DOCKER_BUILDKIT=1 docker build --platform "$VERIFY_PLATFORM" \
      --label "org.opencontainers.image.revision=${revision}" \
      --label "com.streamgate.build.tags=nats_integration" \
      --build-arg "BINARY_NAME=${service}" \
      -f "$ROOT_DIR/deploy/docker/Dockerfile.verify-runtime" \
      -t "${prefix}-${service}:latest" "$src" || fail "${service} image build failed"
    rm -f "$src/deploy/docker/${service}-linux"
    BUILT_BINARIES=()
  done
  archive="${ARTIFACT_DIR}/$(basename "$manifest" .json)-images.tar"
  docker save -o "$archive" "${prefix}-monolith:latest" "${prefix}-api-gateway:latest" "${prefix}-transcoder:latest" \
    || fail "candidate image archive failed"
  python3 scripts/cd/candidate_manifest.py "$manifest" --prefix "$prefix" \
    --archive "$archive" --revision "$revision" --platform "$VERIFY_PLATFORM" || fail "candidate manifest failed"
  if [ -n "${VERIFY_BUILD_REF:-}" ]; then
    git worktree remove --force "$src" || fail "reference worktree cleanup failed"
    REFERENCE_WORKTREE=""
  else
    CANDIDATE_BUILT=1
  fi
  VERIFY_IMAGE_PREFIX="$prefix"
  export VERIFY_IMAGE_PREFIX
}

# prepare_reference_worktree <ref> — a detached worktree at the ref, OUTSIDE
# the working tree so nothing here is touched.
#
# Submodules are REUSED from the main tree, not re-cloned: `git submodule
# update` fetches from github.com, which is slow or unreachable on some
# networks and hung this step indefinitely. The shortcut is only valid when the
# gitlinks match, so that is asserted first — otherwise the comparison would
# silently build the wrong library version.
prepare_reference_worktree() {
  local ref="$1" wt lib
  git rev-parse --verify "${ref}^{commit}" >/dev/null 2>&1 \
    || fail "PERF_REFERENCE_REF '${ref}' is not a commit in this repository"
  wt="$(mktemp -d "${TMPDIR:-/tmp}/sg-ref-build.XXXXXX")"
  rmdir "$wt"
  git worktree add --detach "$wt" "$ref" >/dev/null 2>&1 \
    || fail "could not create a worktree at ${ref}"
  if [ -f .gitmodules ]; then
    if ! git diff --quiet "$ref" HEAD -- contracts/lib; then
      git worktree remove --force "$wt" >/dev/null 2>&1 || true
      fail "reference ref ${ref} points at different submodule commits than HEAD — cannot reuse the checked-out libraries; check them out in ${ref} and retry"
    fi
    for lib in contracts/lib/*/; do
      [ -d "$lib" ] || continue
      rm -rf "${wt:?}/${lib}"
      cp -R "$lib" "${wt:?}/${lib}"
    done
  fi
  printf '%s\n' "$wt"
}

# deploy_chain_contracts <anvil-host-port> — the fullchain stack's anvil
# entrypoint deploys DemoNFT (0x5FbDB…) and ContentRegistry (0xe7f17…) at the
# deterministic first-deployer-nonce addresses; a bare verify anvil deploys
# nothing, so the orchestrator deploys them with the same key and verifies
# both have code before the app needs them.
deploy_chain_contracts() {
  local rpc="$1" code
  command -v cast >/dev/null 2>&1 || fail "controller cast (Foundry) is required to deploy the verify chain contracts"
  command -v forge >/dev/null 2>&1 || fail "controller forge (Foundry) is required to deploy the verify chain contracts"
  local key="0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
  local demo="0x5FbDB2315678afecb367f032d93F642f64180aa3"
  local registry="0xe7f1725E7734CE288F8367e1Bb143E90bb3F0512"
  if [ "$(cast code --rpc-url "$rpc" "$demo" 2>/dev/null)" = "0x" ]; then
    log "deploying DemoNFT to ${rpc}"
    ( cd contracts && forge create --rpc-url "$rpc" --private-key "$key" \
        src/DemoNFT.sol:DemoNFT --legacy --broadcast ) \
      || fail "DemoNFT deployment failed"
  fi
  code="$(cast code --rpc-url "$rpc" "$demo" 2>/dev/null || true)"
  [ -n "$code" ] && [ "$code" != "0x" ] || fail "DemoNFT has no code at $demo after deployment"
  if [ "$(cast code --rpc-url "$rpc" "$registry" 2>/dev/null)" = "0x" ]; then
    log "deploying ContentRegistry to ${rpc}"
    ( cd contracts && forge create --rpc-url "$rpc" --private-key "$key" \
        src/ContentRegistry.sol:ContentRegistry --legacy --broadcast ) \
      || fail "ContentRegistry deployment failed"
  fi
  code="$(cast code --rpc-url "$rpc" "$registry" 2>/dev/null || true)"
  [ -n "$code" ] && [ "$code" != "0x" ] || fail "ContentRegistry has no code at $registry after deployment"
  log "DemoNFT + ContentRegistry deployed and verified at deterministic addresses"
}

# ensure_playwright — npm ci + bundled Chromium once per host; skipped once
# installed.
ensure_playwright() {
  log "installing locked Playwright harness deps (npm ci)"
  ( cd scripts/h5-demo-acceptance && npm ci --no-audit --no-fund ) \
    || fail "npm ci for the h5-demo acceptance harness failed"
  log "ensuring Playwright chromium is installed"
  # Playwright's default 30s download timeout is shorter than a cold
  # chrome-for-testing fetch over a slow link, so the whole media stage died on
  # "Request to https://storage.googleapis.com/... timed out after 30000ms"
  # even though nothing was wrong with the candidate. Retry with a bounded,
  # longer timeout; the browser build itself stays pinned by package-lock.json,
  # so this changes only how long we wait for those bytes.
  local attempt
  for attempt in 1 2 3; do
    if ( cd scripts/h5-demo-acceptance \
      && PLAYWRIGHT_DOWNLOAD_CONNECTION_TIMEOUT=180000 \
         npx playwright install --with-deps chromium ); then
      return
    fi
    log "playwright chromium install failed (attempt ${attempt}/3)"
  done
  fail "playwright chromium install failed"
}

# H5 and the browser communicate through loopback in the controller network
# namespace. Nginx reaches the selected app by project service DNS.
VERIFY_H5_PORT=""
start_h5_nginx() {
  local project="$1" port="$2" upstream=monolith
  [ "$port" != "$VERIFY_GW_PORT" ] || upstream=api-gateway
  local conf="${ARTIFACT_DIR}/nginx-h5-${project}.conf"
  local nginx_image=nginx:alpine@sha256:892bb2152464f0c5d8972593a5f5afaf8054552580b699d95f8ff4af08c2d33d
  ensure_runtime_image "$nginx_image"
  cat > "$conf" <<NGINX
server {
    listen 8080;
    server_name localhost;
    root /usr/share/nginx/html;
    index index.html;
    location ^~ /demo/ {
        alias /usr/share/nginx/html/;
        add_header Cache-Control "no-store, no-cache, must-revalidate" always;
        add_header Pragma "no-cache" always;
        add_header Expires "0" always;
    }
    location /health { proxy_pass http://${upstream}:8080/health; }
    location /metrics { proxy_pass http://${upstream}:8080/metrics; }
    location /api/ { proxy_pass http://${upstream}:8080; proxy_set_header Host \$host; client_max_body_size 500M; }
    location / { try_files \$uri \$uri/ /index.html; }
}
NGINX
  # Serve the frontend bytes from this candidate's image, rather than a live
  # checkout that could change independently while its media is being tested.
  local static_dir extract_id
  static_dir="$(mktemp -d "${ARTIFACT_DIR}/h5-${project}.XXXXXX")" || fail "H5 artifact directory failed"
  extract_id="$(docker create \
    --platform "$VERIFY_PLATFORM" \
    --label "com.docker.compose.project=${project}" \
    --label "com.streamgate.verify.run=${PROJECT_NAME}" \
    "${VERIFY_IMAGE_PREFIX}-monolith:latest")" || fail "H5 image extraction container failed"
  docker cp "${extract_id}:/app/h5-demo/." "$static_dir/" || fail "H5 candidate extraction failed"
  docker rm "$extract_id" >/dev/null || fail "H5 extraction container cleanup failed"
  docker rm -f "${project}-h5" >/dev/null 2>&1 || :
  docker run -d --pull never --platform "$VERIFY_PLATFORM" --name "${project}-h5" --network "${project}_default" --network-alias verify-h5 \
    --label "com.docker.compose.project=${project}" \
    --label "com.docker.compose.service=h5" \
    --label "com.streamgate.verify.run=${PROJECT_NAME}" \
    -v "${static_dir}:/usr/share/nginx/html:ro" \
    -v "${conf}:/etc/nginx/conf.d/default.conf:ro" \
    "$nginx_image" >/dev/null \
    || fail "could not start H5 proxy"
  wait_health "http://localhost:${VERIFY_H5_PORT}/demo/" 60 || fail "H5 proxy did not become ready"
}

start_project_front() {
  local project="$1" config="${ARTIFACT_DIR}/${1}-front.json" ready="${ARTIFACT_DIR}/${1}-front.ready"
  python3 - "$config" <<'PYCONFIG'
import json, sys
services = [("monolith", 8080), ("api-gateway", 8080), ("minio", 9000), ("anvil", 8545), ("postgres", 5432), ("verify-h5", 8080)]
with open(sys.argv[1], "w") as output:
    json.dump([dict(listen=0, host=host, port=port) for host, port in services], output)
PYCONFIG
  rm -f "$ready"
  python3 scripts/cd/verify_proxy.py "$config" "$ready" > "${ARTIFACT_DIR}/${project}-front.log" 2>&1 &
  PROJECT_PROXY_PID=$!
  PROXY_PIDS="${PROXY_PIDS} ${PROJECT_PROXY_PID}"
  local deadline=$((SECONDS + 10))
  while [ ! -f "$ready" ]; do
    kill -0 "$PROJECT_PROXY_PID" 2>/dev/null || fail "project front exited before readiness"
    [ "$SECONDS" -lt "$deadline" ] || fail "project front readiness timeout"
    sleep 0.1
  done
  local ports
  ports="$(python3 - "$ready" <<'PYPORTS'
import json, sys
ports = json.load(open(sys.argv[1]))
print(*(ports[host] for host in ("monolith", "api-gateway", "minio", "anvil", "postgres", "verify-h5")))
PYPORTS
)" || fail "project front did not report bound ports"
  read -r VERIFY_MONO_PORT VERIFY_GW_PORT VERIFY_MINIO_PORT VERIFY_ANVIL_PORT VERIFY_PG_PORT VERIFY_H5_PORT <<< "$ports"
}

# run_fullchain <project> <monolith-port> <gateway-port> <anvil-port> <minio-port> —
# the 66-check acceptance against BOTH deployment modes of the verify stack,
# with project-scoped containers and controller-local front ports. When
# gw_port == mono_port only the monolith mode runs (perf/release do not start
# a gateway). The H5 page comes from the per-run nginx proxy above, whose /api
# upstream is switched to the mode under test.
run_fullchain() {
  local project="$1" mono_port="$2" gw_port="$3" anvil_port="$4" minio_port="$5"
  require_controller_tool curl
  require_controller_tool python3
  require_controller_tool cast
  require_controller_tool ffmpeg
  ensure_playwright
  local common_env=(
    PG_CONTAINER="${project}-postgres-1"
    REDIS_CONTAINER="${project}-redis-1"
    TC="${project}-transcoder-1"
    ANVIL_URL="http://localhost:${anvil_port}"
    ANVIL_BASE="http://localhost:${anvil_port}"
    MINIO_LIVENESS_URL="http://localhost:${minio_port}/minio/health/live"
    PW_CHANNEL=""
    H5_BASE="http://localhost:${VERIFY_H5_PORT}/demo"
    MONOLITH_BASE="http://localhost:${mono_port}"
  )
  log "running fullchain acceptance against the monolith mode (localhost:${mono_port})"
  start_h5_nginx "$project" "$mono_port"
  if ! env "${common_env[@]}" \
    H5_DEMO_URL="http://localhost:${VERIFY_H5_PORT}/demo/" \
    API_BASE="http://localhost:${mono_port}" \
    bash scripts/fullchain-acceptance.sh "http://localhost:${mono_port}"; then
    diag_stack_logs "$project"
    return 1
  fi
  if [ "$gw_port" != "$mono_port" ]; then
    log "running fullchain acceptance against the api-gateway mode (localhost:${gw_port})"
    # The same page, same origin — only the /api upstream moves to the
    # gateway, so the two modes cannot drift on anything but the wiring.
    start_h5_nginx "$project" "$gw_port"
    if ! env "${common_env[@]}" \
      H5_DEMO_URL="http://localhost:${VERIFY_H5_PORT}/demo/" \
      API_BASE="http://localhost:${gw_port}" \
      bash scripts/fullchain-acceptance.sh "http://localhost:${gw_port}"; then
      diag_stack_logs "$project"
      return 1
    fi
  fi
}

# diag_stack_logs <project> — the acceptance's curl -sf discards error bodies,
# so a failed run gets the app containers' errors appended to the stage output.
diag_stack_logs() {
  local project="$1" svc
  for svc in monolith api-gateway transcoder; do
    echo "===== ${project}-${svc}-1: ERROR/WARN/panic lines ====="
    docker logs "${project}-${svc}-1" 2>&1 | grep -E 'ERROR|WARN|panic|error' | tail -40 || true
    echo "===== ${project}-${svc}-1 (tail 60) ====="
    docker logs --tail 60 "${project}-${svc}-1" 2>&1 || true
  done
}

# migrate_verify_stack <project> — applies the full migration set (the app
# never auto-migrates; the fullchain stack's schema comes from `make
# migrate-up`). Runs BEFORE the app services come up, so they never boot
# against an unmigrated database.
migrate_verify_stack() {
  local project="$1"
  log "applying database migrations through project network"
  DATABASE_HOST=localhost DATABASE_PORT="$VERIFY_PG_PORT" \
  DATABASE_USER=postgres DATABASE_PASSWORD=postgres \
  DATABASE_NAME=streamgate DATABASE_SSLMODE=disable \
    go run ./cmd/migrate up || fail "database migrations failed (project ${project})"
}

# bring_up_fullstack <project> [service...] — build once, allocate loopback
# ports (so the CORS list can be computed BEFORE `up`), start the INFRA,
# migrate, then start the app services and wait for their health endpoints.
# Registers the project for cleanup and exports VERIFY_{MONO,GW,MINIO,ANVIL}_PORT
# for the caller (the DAG is sequential, so one set of names is enough).
bring_up_fullstack() {
  local project="$1"; shift
  local services=("$@") want=() svc
  if [ "${#services[@]}" -eq 0 ]; then
    services=(postgres redis minio nats anvil monolith transcoder api-gateway)
  fi
  CREATED_PROJECTS="${CREATED_PROJECTS} ${project}"
  # Reserve ephemeral loopback listeners atomically before building or making
  # network requests. A bind(0)/close probe would race later outgoing sockets.
  start_project_front "$project"
  # The H5 browser origin is a dynamic localhost secure context.
  VERIFY_CORS_ORIGINS="http://localhost:${VERIFY_MONO_PORT},http://localhost:${VERIFY_H5_PORT}"
  export VERIFY_MONO_PORT VERIFY_GW_PORT VERIFY_MINIO_PORT VERIFY_ANVIL_PORT \
    VERIFY_IMAGE_PREFIX VERIFY_CORS_ORIGINS
  log "project ${project} ports: monolith=${VERIFY_MONO_PORT} gateway=${VERIFY_GW_PORT} minio=${VERIFY_MINIO_PORT} anvil=${VERIFY_ANVIL_PORT}"
  prebuild_verify_stack "$project"
  # Split the requested services: infra first (migrations need Postgres), app
  # second (they must never boot against an unmigrated database).
  local infra=() app=()
  for svc in "${services[@]}"; do
    case "$svc" in
      monolith|api-gateway|transcoder) app+=("$svc") ;;
      *) infra+=("$svc") ;;
    esac
  done
  if [[ " ${app[*]} " = *' api-gateway '* && " ${infra[*]} " != *' consul '* ]]; then
    infra+=(consul)
  fi
  if [ "${#infra[@]}" -gt 0 ]; then
    local image images
    images="$(docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" config --format json \
      | python3 -c 'import json,sys; c=json.load(sys.stdin); print(*(c["services"][s]["image"] for s in sys.argv[1:]), sep="\n")' "${infra[@]}")" \
      || fail "could not resolve pinned infrastructure images"
    while IFS= read -r image; do ensure_runtime_image "$image"; done <<< "$images"
    log "starting verify infra (project ${project}): ${infra[*]}"
    docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" up -d --no-build --pull never --wait --wait-timeout 180 "${infra[@]}" \
      || fail "verify infra failed to start (project ${project})"
    docker network connect "${project}_default" "$VERIFY_CONTROLLER" || fail "controller network attach failed"
    migrate_verify_stack "$project"
    deploy_chain_contracts "http://localhost:${VERIFY_ANVIL_PORT}"
  fi
  if [ "${#app[@]}" -gt 0 ]; then
    log "starting verify app services (project ${project}): ${app[*]}"
    docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" up -d --no-build --no-deps --pull never --wait --wait-timeout 300 "${app[@]}" \
      || fail "verify app services failed to start (project ${project})"
  fi
  for svc in "${services[@]}"; do
    case "$svc" in
      monolith)     want+=("monolith|${VERIFY_MONO_PORT}") ;;
      api-gateway)  want+=("api-gateway|${VERIFY_GW_PORT}") ;;
    esac
  done
  if [ "${#want[@]}" -gt 0 ]; then
    local entry name port
    for entry in "${want[@]}"; do
      name="${entry%%|*}"; port="${entry##*|}"
      log "waiting for ${name} health on localhost:${port}"
      wait_health "http://localhost:${port}/health" 300 \
        || fail "${name} not health-ready within 300s (project ${project})"
    done
    log "stack healthy after ${SECONDS}s"
  fi
  local manifest="$CANDIDATE_MANIFEST"
  if [ -n "${VERIFY_BUILD_REF:-}" ]; then manifest="${ARTIFACT_DIR}/reference.json"; fi
  python3 scripts/cd/candidate_manifest.py "$manifest" --project "$project" --platform "$VERIFY_PLATFORM" || fail "stage candidate identity mismatch"
}

# Release only the current project's services and controller forwarding process.
teardown_fullstack() {
  local project="$1"
  if [ -n "${PROJECT_PROXY_PID:-}" ]; then
    kill "$PROJECT_PROXY_PID" 2>/dev/null || :
    wait "$PROJECT_PROXY_PID" 2>/dev/null || :
    PROXY_PIDS="${PROXY_PIDS/ $PROJECT_PROXY_PID/}"
    PROJECT_PROXY_PID=""
  fi
  docker rm -f "${project}-h5" >/dev/null 2>&1 || :
  docker network disconnect "${project}_default" "$VERIFY_CONTROLLER" || fail "controller network disconnect failed"
  docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" down -v --remove-orphans || fail "project cleanup failed"
}

# ---------------------------------------------------------------------------
# media (SG-05 / SG-06): real wallet login, real media decode, real browser
# playback against BOTH deployment modes, with all tools in the controller.
# ---------------------------------------------------------------------------
stage_media() {
  require_controller_tool docker
  local project="${PROJECT_NAME}-media"
  bring_up_fullstack "$project"
  run_logged media run_fullchain "$project" "$VERIFY_MONO_PORT" "$VERIFY_GW_PORT" "$VERIFY_ANVIL_PORT" "$VERIFY_MINIO_PORT"
  teardown_fullstack "$project"
}

# ---------------------------------------------------------------------------
# perf (SG-07): fixed-resource, real-request load matrix. Seeds the stack
# through the full acceptance (its side effects ARE the seed: a gated content
# with real media), then runs the warm_cache matrix at 1/10/50 viewers with
# the acceptance's own wallet. Host-side: perf-acceptance.sh needs go+cast on
# the host.
# ---------------------------------------------------------------------------
# wait_for_anvil_proxy <project> — the socat forwarder carries no healthcheck
# (the image has no probe utility), so readiness is asserted through the chain
# itself: cast, run from the anvil container, must reach the chain BY PROXY
# NAME. A proxy that is not forwarding would leave the monolith's rpc_urls
# pointing at a dead primary, and the fallback scenario would prove nothing.
wait_for_anvil_proxy() {
  local project="$1" deadline=$((SECONDS + 60))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" exec -T anvil \
      cast chain-id --rpc-url http://anvil-proxy:8545 2>/dev/null | grep -q '31337'; then
      log "anvil-proxy forwards to the anvil chain"
      return 0
    fi
    sleep 2
  done
  fail "anvil-proxy never forwarded to the chain (project ${project})"
}

# assert_two_chain_endpoints <project> — the monolith must actually be holding
# TWO same-chain endpoints, or the fault scenarios below degrade into a single
# dead endpoint. Read it back from the app rather than trusting the compose
# interpolation.
assert_two_chain_endpoints() {
  local port="$1"
  python3 - "$port" <<'PY' || fail "the monolith does not expose two chain-31337 RPC endpoints"
import json, sys, urllib.request
port = sys.argv[1]
with urllib.request.urlopen(f"http://localhost:{port}/api/v1/web3/rpc-status", timeout=10) as r:
    data = json.load(r)
for chain in data.get("chains", []):
    if chain.get("chain_id") == 31337:
        urls = [e.get("url", "") for e in chain.get("rpcs", [])]
        print(f"chain 31337 endpoints: {urls}")
        if len(urls) != 2:
            sys.exit(f"expected 2 endpoints, got {len(urls)}: {urls}")
        sys.exit(0)
sys.exit("chain 31337 is not reported at all")
PY
}

# perf_scenario <scenario> <viewers> <result-dir> <content-id> <fixture-hash>
#   [skip-warmup] — one labelled load pass against the per-run stack. The
# wallet pool already exists (the warm pass ran PERF_SETUP), so later passes
# REUSE it: regenerating the pool and re-minting 50 NFTs would cost ~6 minutes
# per scenario and cover nothing new.
perf_scenario() {
  local scenario="$1" viewers="$2" result_dir="$3" content_id="$4" fixture_hash="$5" skip_warmup="${6:-0}"
  PERF_CONTENT_ID="$content_id" \
  PERF_FIXTURE_HASH="$fixture_hash" \
  PERF_WALLET_KEYS_FILE="$result_dir/wallets.keys" \
  PERF_SKIP_WARMUP="$skip_warmup" \
  SCENARIO="$scenario" \
  VIEWERS_LIST="$viewers" \
  RESULT_DIR="$result_dir" \
  BASE_URL="http://localhost:${VERIFY_MONO_PORT}" \
    bash scripts/perf-acceptance.sh \
    || fail "perf-acceptance.sh failed (scenario=${scenario})"
}

# perf_fault_matrix <project> <result-dir> <content-id> <fixture-hash> — SG-07
# step 3's fault scenarios, each with a real injection and the bar the harness
# applies to that label (test/load/live_test.go):
#   cold_cache       restart the monolith (its L1 is in-process) and drop the
#                    L2 keys — sessions live in the same Redis, so ONLY
#                    nft_cache:* is deleted; ≥99% still required.
#   rpc_primary_down stop the proxy, keep the direct anvil: the fallback must
#                    carry 50 concurrent viewers at ≥99%.
#   rpc_all_down     stop both endpoints: the only acceptable result is a total
#                    denial — 0 served, 0 media bytes.
perf_fault_matrix() {
  local project="$1" result_dir="$2" content_id="$3" fixture_hash="$4"

  log "fault matrix: cold_cache (restart monolith, drop L2 gate keys)"
  docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" restart monolith >/dev/null \
    || fail "could not restart the monolith for the cold_cache scenario"
  wait_health "http://localhost:${VERIFY_MONO_PORT}/health" 300 \
    || fail "monolith did not come back for the cold_cache scenario"
  # Targeted, not FLUSHALL: sessions, blacklist and rate-limit buckets share
  # this Redis and must survive, or the run would measure a logged-out stack.
  docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" exec -T redis \
    sh -c "redis-cli --scan --pattern 'nft_cache:*' | while read -r k; do [ -n \"\$k\" ] && redis-cli DEL \"\$k\"; done" >/dev/null 2>&1 || true
  # skip-warmup=1: the built-in sanity run would populate the very cache this
  # scenario exists to measure cold (and leave a stray cold_cache-viewers-1.json
  # that looks like a real 1-viewer result).
  perf_scenario cold_cache 10 "$result_dir" "$content_id" "$fixture_hash" 1

  log "fault matrix: rpc_primary_down (stop the primary proxy; fallback must serve)"
  docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" stop anvil-proxy >/dev/null \
    || fail "could not stop the anvil-proxy for the rpc_primary_down scenario"
  perf_scenario rpc_primary_down 50 "$result_dir" "$content_id" "$fixture_hash" 1

  log "fault matrix: rpc_all_down (stop both endpoints)"
  docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" stop anvil >/dev/null \
    || fail "could not stop the anvil for the rpc_all_down scenario"
  perf_scenario rpc_all_down 10 "$result_dir" "$content_id" "$fixture_hash" 1

  log "fault matrix: restoring the chain endpoints"
  docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" start anvil-proxy >/dev/null 2>&1 || true
  docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" start anvil >/dev/null 2>&1 || true
}

# ---------------------------------------------------------------------------
# Reference comparison (SG-07 step 5): the candidate's p95 must not exceed
# 1.20x the FIXED reference's, measured on THIS runner, sequentially, with the
# same fixture, load and resource limits, medians over independent rounds. Off
# unless PERF_REFERENCE_REF names a git ref: it builds and runs a second full
# stack, so it is a calibration step (run when the reference changes), not a
# cost every `all` pass pays.
# ---------------------------------------------------------------------------
PERF_COMPARE_VIEWERS="${PERF_COMPARE_VIEWERS:-50}"
PERF_COMPARE_ROUNDS="${PERF_COMPARE_ROUNDS:-3}"
PERF_COMPARE_LIMIT="${PERF_COMPARE_LIMIT:-1.20}"

# perf_rounds <result-dir> <wallets-keys> <content-id> <fixture-hash> <commit-label>
#   — the measured rounds of the comparison scenario, each in its own
# sub-directory so the median is taken over INDEPENDENT runs rather than one run
# repeated. commit-label is stamped into the JSON: the harness otherwise records
# THIS checkout's HEAD, which would label a reference measurement as if it were
# the candidate's.
perf_rounds() {
  local dir="$1" keys="$2" content_id="$3" fixture_hash="$4" commit_label="$5" i round
  for i in $(seq 1 "$PERF_COMPARE_ROUNDS"); do
    round="${dir}/round-${i}"
    mkdir -p "$round"
    log "comparison round ${i}/${PERF_COMPARE_ROUNDS} at ${PERF_COMPARE_VIEWERS} viewers → ${round} (${commit_label})"
    PERF_CONTENT_ID="$content_id" \
    PERF_FIXTURE_HASH="$fixture_hash" \
    PERF_WALLET_KEYS_FILE="$keys" \
    PERF_SKIP_WARMUP=1 \
    PERF_COMMIT="$commit_label" \
    SCENARIO=warm_cache \
    VIEWERS_LIST="$PERF_COMPARE_VIEWERS" \
    RESULT_DIR="$round" \
    BASE_URL="http://localhost:${VERIFY_MONO_PORT}" \
      bash scripts/perf-acceptance.sh \
      || fail "comparison round ${i} failed (${dir})"
  done
}

# acceptance_failed_only_in_browser <log-file> — true when the acceptance's own
# summary says every check passed except the browser one. Used to keep the perf
# stage's SEED (which needs a seeded content + transcode, not a Chromium decode)
# from being sunk by a page-load timeout, without blanket-ignoring real
# failures: any other failure, or more than the one browser check, still fails.
acceptance_failed_only_in_browser() {
  local log_file="$1" summary
  summary="$(grep -aE '^RESULT: [0-9]+ PASSED, [0-9]+ FAILED' "$log_file" | tail -1)"
  [ -n "$summary" ] || return 1
  [ "$(printf '%s' "$summary" | sed -E 's/^RESULT: ([0-9]+) PASSED.*/\1/')" = "65" ] || return 1
  [ "$(printf '%s' "$summary" | sed -E 's/.* ([0-9]+) FAILED.*/\1/')" = "1" ] || return 1
  grep -aq '^FAIL: Browser playback failed' "$log_file"
}

# perf_seed_and_pool <project> <dir> [tolerant] — seed through the acceptance
# (its side effects ARE the seed) and build the wallet pool the measured rounds
# reuse. Sets COMPARE_CONTENT_ID / COMPARE_FIXTURE_HASH for the caller.
#
# tolerant=1 is for the REFERENCE stack only: its job is to be a perf baseline,
# and the acceptance's browser-decode check needs a Chromium navigation that is
# orthogonal to the measured TTFB of gated media — a single idle-page timeout
# there must not sink a p95 comparison. The seed is still validated where it
# matters: the content and its transcode must exist, which the assertions below
# enforce. The candidate keeps the full 66-check gate in the media stage.
perf_seed_and_pool() {
  local project="$1" dir="$2" tolerant="${3:-0}" log_file="${LOG_DIR}/compare-seed.log"
  mkdir -p "$LOG_DIR"; : > "$log_file"
  log "stage=compare-seed (host) → log ${log_file}"
  if ! run_fullchain "$project" "$VERIFY_MONO_PORT" "$VERIFY_MONO_PORT" "$VERIFY_ANVIL_PORT" "$VERIFY_MINIO_PORT" 2>&1 | tee -a "$log_file"; then
    if [ "$tolerant" = "1" ] && acceptance_failed_only_in_browser "$log_file"; then
      log "WARNING: the seed acceptance failed ONLY the browser check (log: ${log_file}) — tolerated for the perf stage, whose seed needs a seeded content and transcode; the media stage keeps the strict 66-check gate"
    else
      fail "seed acceptance failed for ${project} (log: ${log_file})"
    fi
  fi
  COMPARE_CONTENT_ID="$(docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" exec -T postgres \
    psql -U postgres -d streamgate -tAc "SELECT id FROM contents ORDER BY created_at DESC LIMIT 1" 2>/dev/null \
    | tr -d '[:space:]' || true)"
  [ -n "$COMPARE_CONTENT_ID" ] || fail "no seeded content in ${project}"
  COMPARE_FIXTURE_HASH="$(docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" exec -T postgres \
    psql -U postgres -d streamgate -tAc "SELECT file_hash FROM contents WHERE id = '$COMPARE_CONTENT_ID'" 2>/dev/null \
    | tr -d '[:space:]' || true)"
  log "comparison seed: content=${COMPARE_CONTENT_ID} fixture_hash=${COMPARE_FIXTURE_HASH:-<none>}"
  # PERF_WALLETS is pinned so the pool covers the measured viewer count even
  # though this setup pass only plays 1 viewer.
  PERF_CONTENT_ID="$COMPARE_CONTENT_ID" \
  PERF_SETUP=1 \
  PERF_WALLETS="$PERF_COMPARE_VIEWERS" \
  PERF_WALLET_KEYS_FILE="${dir}/wallets.keys" \
  SCENARIO=warm_cache \
  VIEWERS_LIST=1 \
  RESULT_DIR="$dir" \
  BASE_URL="http://localhost:${VERIFY_MONO_PORT}" \
    bash scripts/perf-acceptance.sh \
    || fail "wallet pool setup failed for ${project}"
}

# reconfigure_monolith_rpcs <project> <rpc-urls> — recreate the monolith with a
# given STREAMGATE_CHAIN_RPC_URLS value and wait for health. The chain client
# reads its endpoint list at boot, so changing the wiring means recreating the
# container, not editing it in place. The reference pass cannot use this (its
# binary predates the env), which is exactly why the CANDIDATE's measured
# rounds are reconfigured to match the reference's single-endpoint wiring.
reconfigure_monolith_rpcs() {
  local project="$1" rpcs="$2"
  VERIFY_CHAIN_RPCS="$rpcs" \
    docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" up -d --no-build --no-deps --pull never monolith >/dev/null \
    || fail "could not recreate the monolith with STREAMGATE_CHAIN_RPC_URLS='${rpcs}'"
  wait_health "http://localhost:${VERIFY_MONO_PORT}/health" 300 \
    || fail "monolith did not come back after the RPC reconfiguration"
}

# perf_reference_pass <ref> — build the reference image from the ref, bring up
# the reference stack, seed it and run the measured rounds; then tear it down
# so the candidate stack never shares the runner with it.
perf_reference_pass() {
  local ref="$1" project="${PROJECT_NAME}-ref" dir="${ARTIFACT_DIR}/perf-reference"
  rm -rf "$dir"; mkdir -p "$dir"
  log "reference comparison: building + running the reference stack from ${ref}"
  # The reference predates the chain-rpc override and the fault matrix, so it
  # runs with its own single endpoint: drop the perf-stage override for its
  # build/up, then restore it for the candidate stack.
  local saved_rpcs="${VERIFY_CHAIN_RPCS-}"
  unset VERIFY_CHAIN_RPCS
  VERIFY_BUILD_REF="$ref" bring_up_fullstack "$project" postgres redis minio nats anvil monolith transcoder
  unset VERIFY_BUILD_REF
  if [ -n "$saved_rpcs" ]; then export VERIFY_CHAIN_RPCS="$saved_rpcs"; fi
  perf_seed_and_pool "$project" "$dir" 1
  perf_rounds "$dir" "${dir}/wallets.keys" "$COMPARE_CONTENT_ID" "$COMPARE_FIXTURE_HASH" "reference:${ref}"
  teardown_fullstack "$project"
}

# perf_compare_p95 <ref-dir> <cand-dir> — median ttfb p95 per side, then the
# 1.20x limit. Both sides must carry real rounds and a real commit.
perf_compare_p95() {
  python3 - "$1" "$2" "$PERF_COMPARE_VIEWERS" "$PERF_COMPARE_LIMIT" <<'PY' || fail "the candidate's p95 regressed past the reference limit"
import glob, json, os, statistics, sys

ref_dir, cand_dir, viewers, limit = sys.argv[1], sys.argv[2], int(sys.argv[3]), float(sys.argv[4])

def rounds(d):
    out = []
    for f in sorted(glob.glob(os.path.join(d, "round-*", f"warm_cache-viewers-{viewers}.json"))):
        r = json.load(open(f))
        p95 = (r.get("ttfb_ms") or {}).get("p95")
        if not p95:
            sys.exit(f"{f}: no ttfb p95 recorded — not a comparable round")
        out.append((float(p95), r.get("commit", "")))
    return out

ref, cand = rounds(ref_dir), rounds(cand_dir)
if not ref:
    sys.exit(f"no reference rounds under {ref_dir}")
if not cand:
    sys.exit(f"no candidate rounds under {cand_dir}")

ref_p95 = statistics.median([p for p, _ in ref])
cand_p95 = statistics.median([p for p, _ in cand])
ratio = cand_p95 / ref_p95 if ref_p95 else float("inf")

def show(label, rows):
    print(f"  {label:10s} rounds(ms)={[round(p, 1) for p, _ in rows]} "
          f"median={statistics.median([p for p, _ in rows]):.2f} "
          f"commit(s)={sorted({c[:12] for _, c in rows})}")

print(f"reference comparison at {viewers} viewers ({len(ref)} vs {len(cand)} rounds):")
show("reference", ref)
show("candidate", cand)
print(f"  ratio={ratio:.3f} limit={limit} → {'PASS' if ratio <= limit else 'REGRESSION'}")
if ratio > limit:
    sys.exit(f"candidate p95 {cand_p95:.2f}ms exceeds {limit}x the reference {ref_p95:.2f}ms")
PY
}

stage_perf() {
  require_controller_tool docker
  require_controller_tool go
  require_controller_tool cast
  local project="${PROJECT_NAME}-perf" result_dir content_id
  # Reference comparison first, while NO other fullstack stack is up: it needs
  # the same fixed h5 port and the same resources, sequentially — not alongside
  # the candidate stack.
  if [ -n "${PERF_REFERENCE_REF:-}" ]; then
    perf_reference_pass "$PERF_REFERENCE_REF"
  fi
  # Two SAME-CHAIN endpoints for this stage only, the stoppable proxy FIRST:
  # perf is where the fallback and all-down scenarios are injected, so the
  # monolith is given a primary it can lose without losing the chain. The
  # other fullstack stages leave this unset and keep their single endpoint.
  # It stays exported for the whole stage because `compose restart`/`start`
  # re-interpolate the file; it is cleared before the stage returns.
  export VERIFY_CHAIN_RPCS="http://anvil-proxy:8545,http://anvil:8545"
  # perf targets the monolith only; the gateway is not needed for load.
  bring_up_fullstack "$project" postgres redis minio nats anvil anvil-proxy monolith transcoder
  wait_for_anvil_proxy "$project"
  assert_two_chain_endpoints "$VERIFY_MONO_PORT"
  # The seed's acceptance is tolerated ONLY for the browser check — the perf
  # stage needs a seeded content and its transcode, and a Chromium page-load
  # timeout is orthogonal to that. Any other check failing still fails here, and
  # the media stage keeps the strict 66-check gate.
  : > "${LOG_DIR}/perf.log"
  log "stage=perf (host) → log ${LOG_DIR}/perf.log"
  if ! run_fullchain "$project" "$VERIFY_MONO_PORT" "$VERIFY_MONO_PORT" "$VERIFY_ANVIL_PORT" "$VERIFY_MINIO_PORT" 2>&1 | tee -a "${LOG_DIR}/perf.log"; then
    if acceptance_failed_only_in_browser "${LOG_DIR}/perf.log"; then
      log "WARNING: the perf seed failed ONLY the browser check — tolerated here; the media stage keeps the strict 66-check gate"
    else
      fail "perf seed acceptance failed (log: ${LOG_DIR}/perf.log)"
    fi
  fi
  if [ -n "${PERF_CONTENT_ID:-}" ]; then
    content_id="$PERF_CONTENT_ID"
    log "using caller-provided PERF_CONTENT_ID=${content_id}"
  else
    content_id="$(docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" exec -T postgres \
      psql -U postgres -d streamgate -tAc "SELECT id FROM contents ORDER BY created_at DESC LIMIT 1" 2>/dev/null \
      | tr -d '[:space:]' || true)"
    [ -n "$content_id" ] || fail "could not extract the seeded content id from the verify stack's Postgres"
    log "seeded content id from Postgres: ${content_id}"
  fi
  # SG-07's result contract: the JSON carries commit + fixture_hash. The
  # fixture hash is contents.file_hash — the SHA-256 of the original media the
  # seed upload sealed — so the recorded provenance is the REAL fixture.
  local fixture_hash
  fixture_hash="$(docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" exec -T postgres \
    psql -U postgres -d streamgate -tAc "SELECT file_hash FROM contents WHERE id = '$content_id'" 2>/dev/null \
    | tr -d '[:space:]' || true)"
  log "fixture hash for ${content_id}: ${fixture_hash:-<none recorded>}"
  result_dir="${ARTIFACT_DIR}/perf"
  # Clear the previous run's results BEFORE measuring. Without this the
  # provenance check below can be satisfied by a stale file: it carries the
  # required fields, and one written by an earlier run of the SAME commit is
  # indistinguishable from this run's output — so a scenario that never actually
  # ran would be counted as measured. The wallet pool is regenerated by SETUP
  # (PERF_SETUP defaults to 1), so nothing this run needs lives here yet.
  rm -rf "${result_dir:?the perf result directory must be set}"
  mkdir -p "$result_dir"
  # When a reference comparison is requested, EVERY healthy measurement (this
  # matrix and the comparison rounds) runs on the reference's wiring: one direct
  # endpoint. The reference binary predates STREAMGATE_CHAIN_RPC_URLS, so it can
  # only run single-endpoint; charging the candidate a socat hop the reference
  # never pays would report an acceptance artifact as a product regression. The
  # two-endpoint fault-matrix wiring is restored before the fault matrix below.
  if [ -n "${PERF_REFERENCE_REF:-}" ]; then
    log "comparison: wiring the candidate like the reference (single direct endpoint) for all healthy measurements"
    reconfigure_monolith_rpcs "$project" ""
  fi
  # SG-07 step 2 asks for the matrix at each count "repeated 3 times", and for a
  # 100-viewer capacity exploration. PERF_MATRIX_REPEATS and PERF_CAPACITY turn
  # those on; each repeat gets its own directory so the runs are independent
  # samples rather than one run counted repeatedly. The wallet pool is sized to
  # cover the largest count once, and reused across repeats.
  local matrix_repeats="${PERF_MATRIX_REPEATS:-1}" capacity="${PERF_CAPACITY:-0}" rep matrix_dir
  # SG-07 step 2 measures the matrix at 1/10/50/100 viewers and repeats it; the
  # single-pass run that every `all` pays for covers the M4 levels 1/10/50 and
  # leaves the 100 tier to the explicit calibration switch. The provenance gate
  # enforces exactly this split, so the stage's levels and the gate's
  # expectations cannot drift apart.
  local matrix_viewers="${VIEWERS_LIST:-}"
  [ -n "$matrix_viewers" ] || { if [ "$matrix_repeats" -gt 1 ]; then matrix_viewers="1 10 50 100"; else matrix_viewers="1 10 50"; fi; }
  for rep in $(seq 1 "$matrix_repeats"); do
    matrix_dir="$result_dir"
    [ "$matrix_repeats" -gt 1 ] && matrix_dir="${result_dir}/round-${rep}"
    mkdir -p "$matrix_dir"
    # PERF_SETUP=1 makes perf-acceptance.sh generate its own per-viewer wallet
    # pool, log each wallet in for real and dev-mint the gating DemoNFT to each
    # — no single caller key required. A caller-provided PERF_PRIVATE_KEY /
    # PERF_WALLET_KEYS_FILE still wins (both envs are passed through untouched).
    # SETUP runs on the FIRST repeat only: regenerating the pool and re-minting
    # every round would cost minutes per round and cover nothing new.
    local setup_flag=0
    [ "$rep" = "1" ] && setup_flag="${PERF_SETUP:-1}"
    log "perf matrix repeat ${rep}/${matrix_repeats} → ${matrix_dir} (capacity=${capacity})"
    # PERF_WALLET_KEYS_FILE is pinned so the pool lands in ONE known path even
    # when the repeat writes into a round sub-directory; every later repeat and
    # the comparison rounds reuse exactly this file.
    if [ "$rep" = "1" ]; then
      PERF_CONTENT_ID="$content_id" \
      PERF_SETUP="$setup_flag" \
      PERF_WALLET_KEYS_FILE="$result_dir/wallets.keys" \
      PERF_FIXTURE_HASH="${PERF_FIXTURE_HASH:-$fixture_hash}" \
      CAPACITY="$capacity" \
      SCENARIO="${SCENARIO:-warm_cache}" \
      VIEWERS_LIST="$matrix_viewers" \
      RESULT_DIR="$matrix_dir" \
      BASE_URL="http://localhost:${VERIFY_MONO_PORT}" \
        bash scripts/perf-acceptance.sh \
        || fail "perf-acceptance.sh failed (repeat ${rep})"
    else
      # Later repeats reuse the pool SETUP built and measure the SAME levels as
      # the first (matrix_viewers), including 100 when the matrix is repeated:
      # step 2 repeats each of 1/10/50/100, and the wallet pool always covers the
      # largest count because SETUP sizes it from this same list.
      PERF_CONTENT_ID="$content_id" \
      PERF_WALLET_KEYS_FILE="$result_dir/wallets.keys" \
      PERF_SKIP_WARMUP=1 \
      PERF_FIXTURE_HASH="${PERF_FIXTURE_HASH:-$fixture_hash}" \
      CAPACITY=0 \
      SCENARIO="${SCENARIO:-warm_cache}" \
      VIEWERS_LIST="$matrix_viewers" \
      RESULT_DIR="$matrix_dir" \
      BASE_URL="http://localhost:${VERIFY_MONO_PORT}" \
        bash scripts/perf-acceptance.sh \
        || fail "perf-acceptance.sh failed (repeat ${rep})"
    fi
  done
  # SG-07 step 5: the candidate's side of the reference comparison, measured
  # BEFORE the fault matrix so it never shares the stack with a stopped
  # endpoint. The reference side already ran and is torn down.
  if [ -n "${PERF_REFERENCE_REF:-}" ]; then
    local compare_dir="${ARTIFACT_DIR}/perf-compare"
    rm -rf "$compare_dir"; mkdir -p "$compare_dir"
    # The candidate is already on the reference's single-endpoint wiring: it was
    # reconfigured before the warm matrix, above.
    perf_rounds "$compare_dir" "$result_dir/wallets.keys" "$content_id" "${PERF_FIXTURE_HASH:-$fixture_hash}" ""
    # Restore the two-endpoint wiring the fault matrix below needs, and prove it
    # is actually back (the fault scenarios are meaningless otherwise).
    log "comparison: restoring the two-endpoint fault-matrix wiring"
    reconfigure_monolith_rpcs "$project" "$VERIFY_CHAIN_RPCS"
    assert_two_chain_endpoints "$VERIFY_MONO_PORT"
  fi
  # SG-07 step 3: the fault matrix, injected for real against this same stack.
  perf_fault_matrix "$project" "$result_dir" "$content_id" "${PERF_FIXTURE_HASH:-$fixture_hash}"
  log "perf results in ${result_dir}"
  ls -la "$result_dir"
  # SG-07 step 2 records the environment with the numbers (platform/arch, every
  # resource limit, tool and image versions) — a throughput figure whose budget
  # cannot be looked up is not the artefact the step asks for. Limits are read
  # back from the containers, so an unpinned service shows its real values.
  bash scripts/perf-run-manifest.sh "$result_dir" "$project" "$VERIFY_COMPOSE_FILE" \
    || fail "could not write the perf run manifest"
  # SG-07: a result that lost its provenance is not an acceptance artefact, and
  # every result must be bound to THIS candidate rather than an earlier run's
  # numbers. The gate lives in its own script so it can be exercised against the
  # layouts it has to reject (stale commit, mixed commits, a repeat that never
  # happened) without paying for a whole perf stage.
  bash scripts/perf-provenance-check.sh "$result_dir" "$matrix_repeats" \
    || fail "perf results failed the provenance check"
  # SG-07 step 5: the regression limit itself, same runner, both sides real.
  if [ -n "${PERF_REFERENCE_REF:-}" ]; then
    perf_compare_p95 "${ARTIFACT_DIR}/perf-reference" "${ARTIFACT_DIR}/perf-compare"
  fi
  teardown_fullstack "$project"
  unset VERIFY_CHAIN_RPCS
}

# ---------------------------------------------------------------------------
# release (SG-08): a temporary deployment from EMPTY volumes of the images
# just built (same digest), a smoke pass (full acceptance, monolith mode),
# then a failure drill: a deliberately broken variant must fail its health
# gate AND the healthy image must recover — before final teardown.
# ---------------------------------------------------------------------------
stage_release() {
  require_controller_tool docker
  local project="${PROJECT_NAME}-release"
  # Empty volumes by construction: a fresh project name per run; the cleanup
  # trap's `down -v` removes them again.
  bring_up_fullstack "$project"
  run_logged release-smoke run_fullchain "$project" "$VERIFY_MONO_PORT" "$VERIFY_MONO_PORT" "$VERIFY_ANVIL_PORT" "$VERIFY_MINIO_PORT"

  log "failure drill: broken CMD variant must fail its health gate"
  local drill="${project}-drill"
  # The drill reuses the SAME compose project (and image) as the deployment
  # above — it is that deployment with a broken command, per SG-08's
  # same-digest drill, not a second stack. Compose's restart policy would
  # mask a broken image, so the drill runs a plain container that must fail:
  # either the runtime refuses to create it (missing binary → non-zero
  # `docker run`) or it starts and exits. Both are the required failure.
  if docker run -d --name "${drill}-monolith" \
    --label "com.docker.compose.project=${project}" \
    --label "com.streamgate.verify.run=${PROJECT_NAME}" \
    -e STREAMGATE_DB_HOST=postgres -e STREAMGATE_DB_PORT=5432 \
    -e STREAMGATE_DB_USER=postgres -e STREAMGATE_DB_PASSWORD=postgres \
    -e STREAMGATE_DB_NAME=streamgate -e STREAMGATE_REDIS_HOST=redis \
    -e STREAMGATE_REDIS_PORT=6379 \
    "${VERIFY_IMAGE_PREFIX}-monolith:latest" /nonexistent-broken-cmd; then
    local dead="" drill_deadline=$((SECONDS + 30))
    while [ "$SECONDS" -lt "$drill_deadline" ]; do
      if [ "$(docker inspect -f '{{.State.Running}}' "${drill}-monolith" 2>/dev/null || echo false)" = "false" ]; then
        dead=1; break
      fi
      sleep 2
    done
    [ -n "$dead" ] || fail "the broken variant is still running — the drill did not fail as required"
    local restarts
    restarts="$(docker inspect -f '{{.RestartCount}}' "${drill}-monolith" 2>/dev/null || echo unknown)"
    log "broken variant exited (RestartCount=${restarts}) as required"
    docker rm -f "${drill}-monolith" >/dev/null 2>&1 || true
  else
    # The runtime refused the broken variant before it could run — the same
    # required failure, observed at creation time.
    log "broken variant refused at creation as required"
    docker rm -f "${drill}-monolith" >/dev/null 2>&1 || true
  fi

  log "failure drill: healthy image must recover"
  # Stop the healthy deployment first, so the restart below is a genuine
  # recovery drill rather than the container that was never stopped.
  docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" stop monolith >/dev/null 2>&1 \
    || fail "could not stop the monolith for the recovery drill"
  docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" up -d --no-build --no-deps --pull never monolith \
    || fail "healthy image did not come back in the recovery drill"
  wait_health "http://localhost:${VERIFY_MONO_PORT}/health" 300 \
    || fail "healthy image did not recover within the health budget"
  log "healthy image recovered (project ${project})"
  teardown_fullstack "$project"
}

run_stage() {
  case "$1" in
    unit) stage_unit ;;
    chain) stage_chain ;;
    durability) stage_durability ;;
    ops) stage_ops ;;
    media) stage_media ;;
    perf) stage_perf ;;
    release) stage_release ;;
    *) fail "unknown VERIFY_STAGE '$1' (expected: unit|chain|durability|ops|media|perf|release|all)" ;;
  esac
}

assert_final_candidate_evidence() {
  # A partial implementation must never produce an overall green run. These
  # gates become executable evidence as each approved batch is completed.
  python3 scripts/cd/assert_candidate_evidence.py "$ARTIFACT_DIR"
}

mkdir -p "$ARTIFACT_DIR"
ensure_submodules
validate_compose
if [ "${VERIFY_TOOLS_READY:-}" != 1 ]; then
  build_candidate
else
  log "using controller toolchain ${IMAGE_TAG}"
fi

if [ "$VERIFY_STAGE" = "all" ]; then
  log "running full DAG: ${ALL_STAGES[*]}"
  for stage in "${ALL_STAGES[@]}"; do
    run_stage "$stage"
  done
  assert_final_candidate_evidence
else
  run_stage "$VERIFY_STAGE"
fi

log "verification complete (VERIFY_STAGE=${VERIFY_STAGE}); artifacts in ${ARTIFACT_DIR}"
