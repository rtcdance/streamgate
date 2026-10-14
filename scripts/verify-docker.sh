#!/usr/bin/env bash
# ============================================================
# StreamGate — unified Docker verification entry
# ============================================================
# Builds the verification toolchain image once (Go + Foundry + FFmpeg + Node +
# golangci-lint) and runs the requested stage, so a clean checkout needs no
# host toolchain beyond Docker itself. Every stage runs against the same
# candidate source state.
#
#   scripts/verify-docker.sh                 # VERIFY_STAGE=all
#   VERIFY_STAGE=unit scripts/verify-docker.sh
#   make verify-docker VERIFY_STAGE=chain
#
# Stage contract lives in TRANSITION_PLAN.md §5 SG-00. Go-only stages (unit,
# chain, durability, ops) run INSIDE the toolchain image. The stages that need
# docker CLI + host tools (media, perf, release) run on the host against a
# project-scoped compose stack (deploy/docker/compose.verify.yml): unique
# project name, ephemeral host ports, no container_name — so parallel runs and
# the sg-fc-* fullchain stack never collide, and every cleanup below removes
# ONLY the containers this run created.
#
# VERIFY_GOPROXY (e.g. https://goproxy.cn,direct) overrides the toolchain
# image's module source. The verify stack's transcoder image needs no override:
# its Dockerfile already defaults to goproxy.cn.
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
VERIFY_COMPOSE_FILE="deploy/docker/compose.verify.yml"
# Space-separated list of compose projects this run created; the cleanup trap
# removes exactly these and nothing else.
CREATED_PROJECTS=""

ALL_STAGES=(unit chain durability ops media perf release)

log()  { printf '\033[1;34m[verify]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[verify:FAIL]\033[0m %s\n' "$*" >&2; exit 1; }

require_host_tool() {
  command -v "$1" >/dev/null 2>&1 || fail "host tool '$1' is required but not found"
}

cleanup() {
  # Only ever tear down the projects this run created (the acceptance infra
  # project plus every fullstack project). The sg-fc-* stack is never touched:
  # it addresses its containers by container_name, which no project-scoped
  # compose command here can ever name.
  docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" down -v --remove-orphans >/dev/null 2>&1 || true
  local p
  for p in $CREATED_PROJECTS; do
    docker compose -p "$p" -f "$VERIFY_COMPOSE_FILE" down -v --remove-orphans >/dev/null 2>&1 || true
    # The release drill and the h5 proxy run as plain containers (no project
    # label), so compose's --remove-orphans cannot see them; remove by name.
    docker rm -f "${p}-drill-monolith" "${p}-h5" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT

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
  # ${arr[@]+...} keeps an empty array safe under `set -u` on bash 3.2.
  docker build -f deploy/docker/Dockerfile.verify ${proxy_args[@]+"${proxy_args[@]}"} -t "$IMAGE_TAG" . \
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
  mkdir -p "$LOG_DIR"
  if docker network inspect "${PROJECT_NAME}_default" >/dev/null 2>&1; then
    net_args=(--network "${PROJECT_NAME}_default")
  fi
  log "stage=${stage} → log ${log_file}"
  if ! docker run --rm \
        --name "${PROJECT_NAME}-${stage}" \
        ${net_args[@]+"${net_args[@]}"} \
        -v "${ARTIFACT_DIR}:/artifacts" \
        -v "${ROOT_DIR}/scripts:/src/scripts:ro" \
        -v "${ROOT_DIR}/test:/src/test:ro" \
        -w /src \
        "${IMAGE_TAG}" bash -lc "$1" 2>&1 | tee "$log_file"; then
    fail "stage '${stage}' failed (log: ${log_file})"
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
  log "stage=${stage} (host) → log ${log_file}"
  if ! "$@" 2>&1 | tee -a "$log_file"; then
    fail "stage '${stage}' failed (log: ${log_file})"
  fi
  log "stage=${stage} PASS"
}

stage_unit() {
  run_in_image unit '
    set -euo pipefail
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
  require_host_tool docker
  # Postgres only — the same scratch service the CI durability job provides.
  # The DB-backed suites that need Redis/MinIO skip on their own and are not
  # part of the required list.
  log "bringing up scratch Postgres on project ${PROJECT_NAME}"
  docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" up -d --wait postgres \
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
    go test -race -count=1 -json \
      ./pkg/service/transcoding ./pkg/service/registry ./pkg/web3/event ./pkg/service ./test/integration/... \
      > /artifacts/durability-tests.json || true
    python3 scripts/cd/assert_required_tests.py \
      --require "TestIntegration_TranscodingLeaseAgainstPostgres" \
      --require "TestAcceptance_Event" \
      --require "TestAcceptance_RegistryIdempotency|TestAcceptance_NonceAcrossInstances|TestAcceptance_BroadcastUnknownRecovery|TestAcceptance_TxReceiptReorg" \
      --require "TestAcceptance_ChainNonceFloor|TestAcceptance_SharedNonceAuthority" \
      /artifacts/durability-tests.json
    echo "== anvil-tagged DB-backed content lifecycle suite (self-contained chain) =="
    # The anvil suite loads contracts/out/*.json, so forge artifacts must exist
    # before it runs (the same prerequisite CI pins).
    ( cd contracts && forge build )
    go test -tags=anvil -race -count=1 -json ./test/integration \
      > /artifacts/durability-anvil.json || true
    python3 scripts/cd/assert_required_tests.py \
      --require "TestAnvil_ContentRegistry_RegisterAndReadBack" \
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
    go test -race -count=1 -json \
      -run "TestBuildProviders_Production|TestBuildProviders_DevStillFallsBackToMemory|TestSetupRouter_Production|TestSetDraining|TestIsDraining|TestDrainMiddleware" \
      ./pkg/gateway ./pkg/core \
      > /artifacts/ops-gateway-core.json || true
    python3 scripts/cd/assert_required_tests.py \
      --require "TestBuildProviders_Production|TestBuildProviders_DevStillFallsBackToMemory|TestSetupRouter_Production" \
      --require "TestSetDraining|TestIsDraining|TestDrainMiddleware" \
      /artifacts/ops-gateway-core.json

    go test -race -count=1 -json \
      -run "TestValidateProduction|TestIsProduction|TestCapabilityPolicy|TestDeriveProfile" \
      ./pkg/core/config \
      > /artifacts/ops-config.json || true
    python3 scripts/cd/assert_required_tests.py \
      --require "TestValidateProduction|TestIsProduction" \
      --require "TestCapabilityPolicy" \
      --require "TestDeriveProfile" \
      /artifacts/ops-config.json

    echo "== SG-14 ops acceptance suites (protected endpoints, rate limit, drain/health, metrics) =="
    go test -race -count=1 -json \
      -run "TestAcceptance_ProtectedOpsEndpoints|TestAcceptance_MediaRateLimitAndNAT|TestAcceptance_StreamingDrain503Health200" \
      ./pkg/gateway \
      > /artifacts/ops-gateway-sg14.json || true
    python3 scripts/cd/assert_required_tests.py \
      --require "TestAcceptance_ProtectedOpsEndpoints" \
      --require "TestAcceptance_MediaRateLimitAndNAT" \
      --require "TestAcceptance_StreamingDrain503Health200" \
      /artifacts/ops-gateway-sg14.json

    go test -race -count=1 -json \
      -run "TestAcceptance_GateDecisionMetricRecorded" \
      ./pkg/middleware \
      > /artifacts/ops-middleware.json || true
    python3 scripts/cd/assert_required_tests.py \
      --require "TestAcceptance_GateDecisionMetricRecorded" \
      /artifacts/ops-middleware.json

    go test -race -count=1 -json \
      -run "TestMonitoring_NoHighCardinalityLabels" \
      ./pkg/monitoring \
      > /artifacts/ops-monitoring.json || true
    python3 scripts/cd/assert_required_tests.py \
      --require "TestMonitoring_NoHighCardinalityLabels" \
      /artifacts/ops-monitoring.json

    echo "== SG-14 key rotation / audience separation suite =="
    go test -race -count=1 -json \
      -run "TestAcceptance_KeyRotationAndAudience" \
      ./pkg/service \
      > /artifacts/ops-keyrotation.json || true
    python3 scripts/cd/assert_required_tests.py \
      --require "TestAcceptance_KeyRotationAndAudience" \
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
# Shared host-side plumbing for the fullstack stages (media / perf / release):
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

# prebuild_verify_stack <project> — cross-compiles STATIC linux binaries (the
# quick Dockerfiles COPY them in; they must not be run on the host, and the
# host artifacts are removed right after the image builds) and tags the images
# where compose expects them. The transcoder image needs no proxy override:
# Dockerfile.transcoder defaults to goproxy.cn internally.
prebuild_verify_stack() {
  local project="$1" goos
  command -v go >/dev/null 2>&1 || fail "host Go toolchain is required to build the verify stack images"
  goos="$(go env GOOS)"; [ "$goos" = "darwin" ] && goos=linux
  log "cross-building linux binaries for the verify stack (GOARCH=$(go env GOARCH))"
  ( CGO_ENABLED=0 GOOS="$goos" GOARCH="$(go env GOARCH)" go build -o deploy/docker/streamgate-linux ./cmd/monolith/streamgate \
    && CGO_ENABLED=0 GOOS="$goos" GOARCH="$(go env GOARCH)" go build -o deploy/docker/api-gateway-linux ./cmd/microservices/api-gateway ) \
    || fail "cross-build of the verify stack binaries failed"
  docker build -f deploy/docker/Dockerfile.monolith.quick -t "${project}-monolith" . \
    || fail "monolith image build failed"
  docker build -f deploy/docker/Dockerfile.api-gateway.quick -t "${project}-api-gateway" . \
    || fail "api-gateway image build failed"
  docker build -f deploy/docker/Dockerfile.transcoder -t "${project}-transcoder" . \
    || fail "transcoder image build failed"
  rm -f deploy/docker/streamgate-linux deploy/docker/api-gateway-linux
}

# deploy_chain_contracts <anvil-host-port> — the fullchain stack's anvil
# entrypoint deploys DemoNFT (0x5FbDB…) and ContentRegistry (0xe7f17…) at the
# deterministic first-deployer-nonce addresses; a bare verify anvil deploys
# nothing, so the orchestrator deploys them with the same key and verifies
# both have code before the app needs them.
deploy_chain_contracts() {
  local rpc="$1" code
  command -v cast >/dev/null 2>&1 || fail "host cast (Foundry) is required to deploy the verify chain contracts"
  command -v forge >/dev/null 2>&1 || fail "host forge (Foundry) is required to deploy the verify chain contracts"
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
  if [ ! -d scripts/h5-demo-acceptance/node_modules/playwright ]; then
    log "installing Playwright harness deps (npm ci)"
    ( cd scripts/h5-demo-acceptance && npm ci --no-audit --no-fund ) \
      || fail "npm ci for the h5-demo acceptance harness failed"
  fi
  log "ensuring Playwright chromium is installed"
  ( cd scripts/h5-demo-acceptance && npx playwright install chromium ) \
    || fail "playwright chromium install failed"
}

# The browser spec needs the H5 page to be served exactly the way the sg-fc
# stack serves it: through nginx whose /demo/ location carries its own
# add_header (which in nginx cancels the server-level CSP inheritance, so the
# page loads WITHOUT a Content-Security-Policy header), on a host port the
# page's own API-base inference accepts (h5-demo/js/api.js hardcodes an
# accepted set containing 29091, which the sg-fc stack never binds).
VERIFY_H5_PORT=29091

# start_h5_nginx <project> <upstream-port> — (re)starts the per-run h5 proxy
# in front of the CURRENT mode's API. One container, re-run between the two
# acceptance modes so /api always points at the mode under test.
start_h5_nginx() {
  local project="$1" port="$2"
  local conf="${ARTIFACT_DIR}/nginx-h5-${project}.conf"
  cat > "$conf" <<NGINX
server {
    listen 8080;
    server_name localhost;
    root /usr/share/nginx/html;
    index index.html;

    # Mirror deploy/nginx/default.conf: this location's own add_header cancels
    # the server-level header inheritance, so /demo/ is served without a CSP
    # header — the same surface the sg-fc stack exposes to the browser spec.
    location ^~ /demo/ {
        alias /usr/share/nginx/html/;
        add_header Cache-Control "no-store, no-cache, must-revalidate" always;
        add_header Pragma "no-cache" always;
        add_header Expires "0" always;
    }

    location /health { proxy_pass http://host.docker.internal:${port}/health; proxy_set_header Host \$host; proxy_set_header X-Real-IP \$remote_addr; }
    location /metrics { proxy_pass http://host.docker.internal:${port}/metrics; proxy_set_header Host \$host; proxy_set_header X-Real-IP \$remote_addr; }
    location /api/ { proxy_pass http://host.docker.internal:${port}; proxy_set_header Host \$host; proxy_set_header X-Real-IP \$remote_addr; proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for; client_max_body_size 500M; }
    location / { try_files \$uri \$uri/ /index.html; }
}
NGINX
  docker rm -f "${project}-h5" >/dev/null 2>&1 || true
  docker run -d --name "${project}-h5" -p "${VERIFY_H5_PORT}:8080" \
    -v "${ROOT_DIR}/h5-demo:/usr/share/nginx/html:ro" \
    -v "${conf}:/etc/nginx/conf.d/default.conf:ro" \
    nginx:alpine >/dev/null \
    || fail "could not start the h5 proxy container"
  wait_health "http://localhost:${VERIFY_H5_PORT}/demo/" 60 \
    || fail "h5 proxy not serving /demo/ on port ${VERIFY_H5_PORT}"
  log "h5 proxy on :${VERIFY_H5_PORT} → upstream localhost:${port}"
}

# run_fullchain <project> <monolith-port> <gateway-port> <anvil-port> <minio-port> —
# the 66-check acceptance against BOTH deployment modes of the verify stack,
# with the stack's project-prefixed container names and published ports. When
# gw_port == mono_port only the monolith mode runs (perf/release do not start
# a gateway). The H5 page comes from the per-run nginx proxy above, whose /api
# upstream is switched to the mode under test.
run_fullchain() {
  local project="$1" mono_port="$2" gw_port="$3" anvil_port="$4" minio_port="$5"
  require_host_tool curl
  require_host_tool python3
  require_host_tool cast
  require_host_tool ffmpeg
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
  local project="$1" pg_port
  pg_port="$(docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" port postgres 5432 2>/dev/null \
    | sed -n 's/.*:\([0-9][0-9]*\)$/\1/p' | head -1)"
  [ -n "$pg_port" ] || fail "no published port for postgres (project ${project})"
  log "applying database migrations on localhost:${pg_port}"
  DATABASE_HOST=localhost DATABASE_PORT="$pg_port" \
  DATABASE_USER=postgres DATABASE_PASSWORD=postgres \
  DATABASE_NAME=streamgate DATABASE_SSLMODE=disable \
    go run ./cmd/migrate up || fail "database migrations failed (project ${project})"
}

# bring_up_fullstack <project> [service...] — prebuild, allocate free host
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
  # One free host port per published service, picked now so VERIFY_CORS_ORIGINS
  # (interpolated by compose at up time) can name the monolith's real origin.
  VERIFY_MONO_PORT="$(free_port)"
  VERIFY_GW_PORT="$(free_port)"
  VERIFY_MINIO_PORT="$(free_port)"
  VERIFY_ANVIL_PORT="$(free_port)"
  VERIFY_IMAGE_PREFIX="$project"
  # CORS: the h5 demo page runs on the verify stack's nginx origin (:29091),
  # and the browser spec's hls.js XHRs the API directly from that page — so
  # the API's allow-list must carry BOTH the monolith's own origin (gateway
  # mode) and the page origin, mirroring how docker-compose.fullchain.yml
  # lists http://localhost:18000 (its nginx) in the monolith's CORS list.
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
  if [ "${#infra[@]}" -gt 0 ]; then
    log "starting verify infra (project ${project}): ${infra[*]}"
    docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" up -d --wait "${infra[@]}" \
      || fail "verify infra failed to start (project ${project})"
    migrate_verify_stack "$project"
    deploy_chain_contracts "http://localhost:${VERIFY_ANVIL_PORT}"
  fi
  if [ "${#app[@]}" -gt 0 ]; then
    log "starting verify app services (project ${project}): ${app[*]}"
    docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" up -d --no-build --wait "${app[@]}" \
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
}

# teardown_fullstack <project> — remove a fullstack stage's stack AND its h5
# proxy the moment that stage finishes, instead of leaving both for the exit
# trap. In `all` the fullstack stages (media → perf → release) run back to
# back, and they must not overlap: the h5 proxy binds the FIXED host port
# 29091 (the page's API-base inference accepts no other — see start_h5_nginx),
# so a stack left up by the previous stage still holds it and the next stage
# dies on "port is already allocated"; and a co-resident full stack would
# compete with perf's measured run. On a stage failure `fail` exits and the
# exit trap still cleans everything up, so this only handles the happy path.
teardown_fullstack() {
  local project="$1"
  docker rm -f "${project}-h5" >/dev/null 2>&1 || true
  docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" down -v --remove-orphans >/dev/null 2>&1 || true
}

# free_port — an unused TCP port on 127.0.0.1, found via Python (the host
# toolchain requirement already includes python3 for the acceptance script).
free_port() {
  python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()'
}

# ---------------------------------------------------------------------------
# media (SG-05 / SG-06): real wallet login, real media decode, real browser
# playback — the full 66-check acceptance against BOTH deployment modes of a
# fullchain-style verify stack. Host-side: needs docker CLI, cast, ffmpeg and
# Playwright.
# ---------------------------------------------------------------------------
stage_media() {
  require_host_tool docker
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
stage_perf() {
  require_host_tool docker
  require_host_tool go
  require_host_tool cast
  local project="${PROJECT_NAME}-perf" result_dir content_id
  # perf targets the monolith only; the gateway is not needed for load.
  bring_up_fullstack "$project" postgres redis minio nats anvil monolith transcoder
  run_logged perf run_fullchain "$project" "$VERIFY_MONO_PORT" "$VERIFY_MONO_PORT" "$VERIFY_ANVIL_PORT" "$VERIFY_MINIO_PORT"
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
  # PERF_SETUP=1 makes perf-acceptance.sh generate its own per-viewer wallet
  # pool, log each wallet in for real and dev-mint the gating DemoNFT to each
  # — no single caller key required. A caller-provided PERF_PRIVATE_KEY /
  # PERF_WALLET_KEYS_FILE still wins (both envs are passed through untouched).
  PERF_CONTENT_ID="$content_id" \
  PERF_SETUP="${PERF_SETUP:-1}" \
  PERF_FIXTURE_HASH="${PERF_FIXTURE_HASH:-$fixture_hash}" \
  SCENARIO="${SCENARIO:-warm_cache}" \
  VIEWERS_LIST="${VIEWERS_LIST:-1 10 50}" \
  RESULT_DIR="$result_dir" \
  BASE_URL="http://localhost:${VERIFY_MONO_PORT}" \
    bash scripts/perf-acceptance.sh \
    || fail "perf-acceptance.sh failed"
  log "perf results in ${result_dir}"
  ls -la "$result_dir"
  # SG-07: a result that lost its provenance (commit / scenario) is not an
  # acceptance artefact.
  python3 - "$result_dir" <<'PY' || fail "perf results failed the provenance check"
import glob, json, sys
files = sorted(glob.glob(sys.argv[1] + "/warm_cache-viewers-*.json"))
if not files:
    sys.exit("no warm_cache result files found")
for f in files:
    r = json.load(open(f))
    for key in ("commit", "scenario", "content_id", "viewers", "success_count"):
        if r.get(key) in (None, ""):
            sys.exit(f"{f}: missing provenance field {key!r}")
    print(f"provenance ok: {f.split('/')[-1]} commit={r['commit'][:12]} fixture_hash={r.get('fixture_hash','')[:16]}")
PY
  teardown_fullstack "$project"
}

# ---------------------------------------------------------------------------
# release (SG-08): a temporary deployment from EMPTY volumes of the images
# just built (same digest), a smoke pass (full acceptance, monolith mode),
# then a failure drill: a deliberately broken variant must fail its health
# gate AND the healthy image must recover — before final teardown.
# ---------------------------------------------------------------------------
stage_release() {
  require_host_tool docker
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
  docker compose -p "$project" -f "$VERIFY_COMPOSE_FILE" up -d --no-build monolith \
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
