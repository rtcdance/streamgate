#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
export VERIFY_SOURCE_ROOT="$ROOT_DIR"
export VERIFY_STAGE="${VERIFY_STAGE:-${1:-all}}"
if [ "$VERIFY_STAGE" = all ]; then
  source_status="$(GIT_OPTIONAL_LOCKS=0 git status --porcelain --untracked-files=normal)" || {
    echo 'cannot verify candidate checkout state' >&2; exit 1;
  }
  if [ -n "$source_status" ]; then
    echo 'complete acceptance requires a frozen clean candidate; use individual stages during repairs' >&2
    exit 1
  fi
fi
export VERIFY_PROJECT="${VERIFY_PROJECT:-streamgate-verify-$$-${RANDOM}}"
export VERIFY_PLATFORM="${VERIFY_PLATFORM:-linux/amd64}"
export VERIFY_ARTIFACTS="${VERIFY_ARTIFACTS:-${ROOT_DIR}/.verify-artifacts/${VERIFY_PROJECT}}"
# EFF-03 (optional): the shared Go build cache is resolved on the HOST and
# forwarded, because stage containers bind it through the daemon and the driver
# cannot invent a host path from inside the controller. Only created when the
# operator opted in; the default run stays a cold build.
if [ "${VERIFY_SHARED_GOCACHE:-0}" = 1 ]; then
  export VERIFY_GO_CACHE="${VERIFY_GO_CACHE:-${HOME}/.cache/streamgate-go-build/${VERIFY_PLATFORM//\//-}}"
  mkdir -p "$VERIFY_GO_CACHE" || { echo "cannot create Go build cache: $VERIFY_GO_CACHE" >&2; exit 1; }
fi
export VERIFY_IMAGE="${VERIFY_IMAGE:-${VERIFY_PROJECT}-tools:local}"
export VERIFY_CONTROLLER="${VERIFY_PROJECT}-controller"
if [ -n "$(docker ps -aq --filter "label=com.streamgate.verify.run=${VERIFY_PROJECT}")" ] \
  || [ -n "$(docker volume ls -q --filter "label=com.streamgate.verify.run=${VERIFY_PROJECT}")" ] \
  || [ -n "$(docker network ls -q --filter "label=com.docker.compose.project=${VERIFY_PROJECT}")" ]; then
  echo "verification project already has resources: ${VERIFY_PROJECT}; choose a fresh VERIFY_PROJECT" >&2
  exit 1
fi
# A foreign container may already occupy the deterministic controller name. Refuse
# before building or running anything so cleanup never targets an unrelated container.
if [ -n "$(docker ps -aq --filter "name=^/${VERIFY_CONTROLLER}$")" ]; then
  echo "controller container name already in use: ${VERIFY_CONTROLLER}; choose a fresh VERIFY_PROJECT" >&2
  exit 1
fi
# EFF-04: one acceptance at a time on this daemon. The lock is a Docker
# network with a fixed name: creating it is atomic (a second create fails),
# it needs no image, and it lives on the daemon rather than on one host, so a
# second checkout cannot slip past a host-local lock file. The cleanup trap
# releases it; a SIGKILLed shell leaves it behind on purpose, and the failure
# message names the owner and the exact removal command instead of silently
# taking the lock over.
MUTEX_NAME="streamgate-verify-mutex"
MUTEX_OWNER=0
acquire_mutex() {
  if docker network create \
      --label "com.streamgate.verify.mutex.owner=${VERIFY_PROJECT}" \
      --label "com.streamgate.verify.mutex.at=$(date +%s)" \
      "$MUTEX_NAME" >/dev/null 2>&1; then
    MUTEX_OWNER=1
    return 0
  fi
  local owner
  owner="$(docker network inspect "$MUTEX_NAME" \
    --format '{{index .Labels "com.streamgate.verify.mutex.owner"}}' 2>/dev/null || true)"
  if [ "${VERIFY_MUTEX_TAKEOVER:-0}" = 1 ] \
     && [ -z "$(docker ps -q --filter "label=com.streamgate.verify.run=${owner}" 2>/dev/null)" ]; then
    echo "taking over the acceptance lock from inactive run '${owner:-unknown}'" >&2
    docker network rm "$MUTEX_NAME" >/dev/null 2>&1 || true
    if docker network create \
        --label "com.streamgate.verify.mutex.owner=${VERIFY_PROJECT}" \
        --label "com.streamgate.verify.mutex.at=$(date +%s)" \
        "$MUTEX_NAME" >/dev/null 2>&1; then
      MUTEX_OWNER=1
      return 0
    fi
  fi
  echo "another acceptance holds the daemon lock (owner: ${owner:-unknown})." >&2
  echo "If that run is gone, clear it with: docker network rm ${MUTEX_NAME}" >&2
  echo "or re-run with VERIFY_MUTEX_TAKEOVER=1 once it has no running containers." >&2
  return 1
}
# The full cleanup trap is installed only after the tool image is built, so an
# early failure (environment gate, frozen clone, build) would otherwise leak the
# lock. Release it from a minimal trap until the real cleanup replaces it.
release_mutex() {
  if [ "${MUTEX_OWNER:-0}" = 1 ]; then
    docker network rm "$MUTEX_NAME" >/dev/null 2>&1 || true
  fi
}
acquire_mutex || exit 1
trap release_mutex EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# EFF-00: refuse an environment that cannot carry the formal run BEFORE the
# tool image is built. A 16 GiB Docker VM OOM-killed the DAG twice and an
# arm64 daemon measures through emulation, so the final all / full perf /
# release path requires a dedicated native amd64 runner. Targeted stages are
# the repair loop and stay available on smaller machines.
bash scripts/cd/verify_environment.sh "$VERIFY_STAGE" "$VERIFY_PROJECT" || exit 1
# EFF-04 A: run the acceptance from a frozen clone, not from the directory a
# human or another agent may still be editing. The clone is pinned to the exact
# HEAD the clean-tree check just approved, its submodules are cloned from the
# local checkouts (offline, no GitHub round trip), and the container mounts the
# clone, so a mid-run edit or commit in the source tree cannot change what is
# being verified. Targeted stages keep running from the working tree: they are
# the repair loop, and paying for a clone on every fix cycle is not the point.
mkdir -p "$VERIFY_ARTIFACTS"
VERIFY_ARTIFACTS="$(cd "$VERIFY_ARTIFACTS" && pwd)"
export VERIFY_ARTIFACTS

SOURCE_REPO="$ROOT_DIR"
FROZEN_SOURCE=""
frozen_requested=0
case "${VERIFY_FROZEN_SOURCE:-auto}" in
  1) frozen_requested=1 ;;
  0) frozen_requested=0 ;;
  *) [ "$VERIFY_STAGE" = all ] && frozen_requested=1 ;;
esac
if [ "$frozen_requested" = 1 ]; then
  FROZEN_SOURCE="$VERIFY_ARTIFACTS/source"
  if [ -e "$FROZEN_SOURCE" ]; then
    echo "frozen source already exists: $FROZEN_SOURCE; choose a fresh VERIFY_ARTIFACTS" >&2
    exit 1
  fi
  frozen_revision="$(git -C "$SOURCE_REPO" rev-parse HEAD)" || exit 1
  git clone --quiet --no-checkout "$SOURCE_REPO" "$FROZEN_SOURCE" \
    || { echo "could not clone the frozen source" >&2; exit 1; }
  git -C "$FROZEN_SOURCE" checkout --quiet --detach "$frozen_revision" \
    || { echo "could not check out $frozen_revision in the frozen source" >&2; exit 1; }
  if [ -f "$SOURCE_REPO/.gitmodules" ]; then
    git -C "$FROZEN_SOURCE" submodule init >/dev/null 2>&1 || true
    while read -r _ sub_path; do
      [ -n "$sub_path" ] || continue
      [ -d "$SOURCE_REPO/$sub_path" ] || continue
      git -C "$FROZEN_SOURCE" config "submodule.$sub_path.url" "$SOURCE_REPO/$sub_path" || exit 1
    done < <(git -C "$SOURCE_REPO" config --file .gitmodules --get-regexp '^submodule\..*\.path$')
    # Git refuses the "file" transport for submodules by default (CVE-2022-39253).
    # The URL was set on the line above to a checkout we control, so allowing it
    # here keeps the population offline instead of fetching from GitHub.
    git -C "$FROZEN_SOURCE" -c protocol.file.allow=always submodule update --init --recursive --quiet \
      || { echo "could not populate the frozen source submodules" >&2; exit 1; }
  fi
  ROOT_DIR="$FROZEN_SOURCE"
  # The driver resolves its source root from VERIFY_SOURCE_ROOT, which was
  # exported above from the editing tree; without this the container would
  # mount the clone and then operate on a path it cannot see.
  export VERIFY_SOURCE_ROOT="$ROOT_DIR"
  cd "$ROOT_DIR"
  echo "[env] verifying the frozen clone at $ROOT_DIR (revision $frozen_revision)" >&2
fi
if [ -f .gitmodules ]; then git submodule update --init --recursive; fi

proxy_args=()
if [ -n "${VERIFY_GOPROXY:-}" ]; then proxy_args=(--build-arg "GOPROXY=${VERIFY_GOPROXY}"); fi
docker build --platform "$VERIFY_PLATFORM" -f deploy/docker/Dockerfile.verify \
  ${proxy_args[@]+"${proxy_args[@]}"} -t "$VERIFY_IMAGE" .
export VERIFY_TOOL_IMAGE_ID="$(docker image inspect --format '{{.Id}}' "$VERIFY_IMAGE")"

endpoint="${DOCKER_HOST:-$(docker context inspect --format '{{.Endpoints.docker.Host}}')}"
case "$endpoint" in
  unix://*) socket="${endpoint#unix://}" ;;
  *) echo 'verify-controller requires a local Docker Unix socket' >&2; exit 1 ;;
esac

# Bash may read the rest of a long script after a stage returns. Execute a
# private snapshot so edits to the checkout cannot change its parser offset.
mkdir "$VERIFY_ARTIFACTS/execution" || {
  echo 'execution evidence already exists; choose a fresh VERIFY_ARTIFACTS directory' >&2; exit 1;
}
cp scripts/verify-docker.sh "$VERIFY_ARTIFACTS/execution/verify-driver.sh"

forward_env=()
while IFS= read -r name; do
  case "$name" in
    # GOMAXPROCS/GOFLAGS let a memory-constrained Docker VM (e.g. 8 GiB
    # Docker Desktop) cap compile parallelism instead of being OOM-killed.
    VERIFY_*|PERF_*|COVERAGE_MIN|GOMAXPROCS|GOFLAGS) forward_env+=(-e "$name") ;;
  esac
done < <(compgen -e)

cleanup() {
  local rc=$? cleanup_rc=0
  trap - EXIT INT TERM
  docker rm -f "$VERIFY_CONTROLLER" >/dev/null 2>&1 || :
  # If the controller was killed before its EXIT trap, remove only this run's
  # labelled resources. Other projects and their persistent volumes are untouched.
  local ids
  ids="$(docker ps -aq --filter "label=com.streamgate.verify.run=${VERIFY_PROJECT}" --filter label=com.docker.compose.project)" || { ids=""; cleanup_rc=1; }
  if [ -n "$ids" ]; then docker rm -f $ids >/dev/null 2>&1 || cleanup_rc=1; fi
  ids="$(docker volume ls -q --filter "label=com.streamgate.verify.run=${VERIFY_PROJECT}" --filter label=com.docker.compose.project)" || { ids=""; cleanup_rc=1; }
  if [ -n "$ids" ]; then docker volume rm $ids >/dev/null 2>&1 || cleanup_rc=1; fi
  ids="$(docker network ls -q --filter "label=com.streamgate.verify.run=${VERIFY_PROJECT}" --filter label=com.docker.compose.project)" || { ids=""; cleanup_rc=1; }
  if [ -n "$ids" ]; then docker network rm $ids >/dev/null 2>&1 || cleanup_rc=1; fi
  # Release the daemon lock only if this run created it; a takeover or a failed
  # acquire must not remove someone else's lock. The exit code is preserved
  # above, so a cleanup failure can never turn a cancelled run into a pass.
  if [ "${MUTEX_OWNER:-0}" = 1 ]; then
    docker network rm "$MUTEX_NAME" >/dev/null 2>&1 || cleanup_rc=1
  fi
  # The frozen clone is reproducible from the recorded revision; keeping ~900MB
  # of it per run would only make the artifacts directory unmanageable.
  if [ -n "${FROZEN_SOURCE:-}" ] && [ -d "$FROZEN_SOURCE" ]; then
    rm -rf -- "$FROZEN_SOURCE" || cleanup_rc=1
  fi
  # A force-removed controller cannot execute its child traps. The private
  # root is run-scoped and also holds the ledger written BEFORE creating a
  # build output, so cancellation cannot leave wallet/session backups behind.
  if [ -n "${VERIFY_ARTIFACTS:-}" ]; then
    local owned_binary worktree
    if [ -f "$VERIFY_ARTIFACTS/.private/owned-binaries" ]; then
      while IFS= read -r owned_binary; do
        case "$owned_binary" in
          "$ROOT_DIR/deploy/docker/monolith-linux"|"$ROOT_DIR/deploy/docker/api-gateway-linux"|"$ROOT_DIR/deploy/docker/transcoder-linux")
            rm -f -- "$owned_binary" || cleanup_rc=1 ;;
          *) echo 'unexpected owned binary path in cleanup ledger' >&2; cleanup_rc=1 ;;
        esac
      done < "$VERIFY_ARTIFACTS/.private/owned-binaries"
    fi
    for worktree in "$VERIFY_ARTIFACTS"/.private/reference-build.*; do
      [ -d "$worktree" ] || continue
      git worktree remove --force "$worktree" >/dev/null 2>&1 || cleanup_rc=1
    done
    rm -f -- "$VERIFY_ARTIFACTS/perf-loader.test" || cleanup_rc=1
    rm -rf -- "$VERIFY_ARTIFACTS/.private" || cleanup_rc=1
  else
    cleanup_rc=1
  fi
  if [ "$cleanup_rc" != 0 ]; then
    printf '%s\n' "verify-controller cleanup failed; original exit=${rc}" >&2
    if [ "$rc" = 0 ]; then rc=1; fi
  fi
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Start the controller container DETACHED and wait for it from this shell.
# Blocking on a foreground child swallows INT/TERM for the entire run: bash
# defers a trap until the current foreground command finishes, and the
# controller lives for the whole acceptance stage — measured here, a foreground
# `sleep 30` ran its TERM trap only after 29.6s. A cancelled run would then
# keep its containers, volumes and networks alive, which is exactly what the
# plan forbids. `--rm` is dropped for the same reason: the exit code must still
# be readable from the stopped container, and cleanup removes it by name.
docker run -d --init --platform "$VERIFY_PLATFORM" --name "$VERIFY_CONTROLLER" \
  --label "com.streamgate.verify.run=${VERIFY_PROJECT}" \
  --label "com.docker.compose.project=${VERIFY_PROJECT}" \
  -v "${socket}:/var/run/docker.sock" \
  -v "${ROOT_DIR}:${ROOT_DIR}" -v "${VERIFY_ARTIFACTS}:${VERIFY_ARTIFACTS}" \
  -w "$ROOT_DIR" "${forward_env[@]}" \
  -e VERIFY_IN_CONTROLLER=1 -e VERIFY_TOOLS_READY=1 \
  "$VERIFY_TOOL_IMAGE_ID" bash "$VERIFY_ARTIFACTS/execution/verify-driver.sh" "$@" \
  >/dev/null 2>"${VERIFY_ARTIFACTS}/controller-start.err" &
state="" tries=0
# A failed `docker run` must surface as an error rather than as an endless poll,
# so its stderr is kept instead of discarded.
while [ "$tries" -lt 60 ]; do
  state="$(docker inspect --format '{{.State.Status}}' "$VERIFY_CONTROLLER" 2>/dev/null)" || state=""
  [ -n "$state" ] && break
  # A real one-second tick. `read -t` on /dev/null returns at EOF immediately,
  # so it does NOT wait: the loop burned all its retries before the container was
  # even created and reported "did not start" while the controller was in fact
  # running. `sleep` is an external command, so a pending INT/TERM runs its trap
  # when the tick finishes — at most a second later, which keeps a cancelled run
  # responsive.
  sleep 1
  tries=$((tries + 1))
done
if [ -z "$state" ]; then
  echo "the verification controller did not start: ${VERIFY_CONTROLLER}" >&2
  if [ -s "${VERIFY_ARTIFACTS}/controller-start.err" ]; then
    cat "${VERIFY_ARTIFACTS}/controller-start.err" >&2
  fi
  exit 1
fi
# A detached container's output never reaches this shell, and cleanup removes
# the container, so without this the whole controller log (including every
# stage result) would be lost. Follow it only once the container exists — the
# backgrounded `docker run` and `docker logs -f` otherwise race, and the
# follower dies with "No such container" before any stage output is streamed.
# A background follower cannot block the INT/TERM traps the way a foreground
# `docker run` did. The copy in the run's artifacts also survives cleanup.
: > "${VERIFY_ARTIFACTS}/controller.log"
docker logs -f "$VERIFY_CONTROLLER" 2>&1 | tee -a "${VERIFY_ARTIFACTS}/controller.log" &
LOG_FOLLOWER=$!
while :; do
  case "$state" in exited|dead) break ;; esac
  sleep 1
  state="$(docker inspect --format '{{.State.Status}}' "$VERIFY_CONTROLLER" 2>/dev/null)" || state=""
  [ -n "$state" ] || break
done
# `docker logs -f` ends by itself once the container stops; wait only for it so
# the tail of the log is not truncated by this shell exiting first.
wait "$LOG_FOLLOWER" 2>/dev/null || :
rc="$(docker inspect --format '{{.State.ExitCode}}' "$VERIFY_CONTROLLER" 2>/dev/null)" || rc=1
exit "$rc"
