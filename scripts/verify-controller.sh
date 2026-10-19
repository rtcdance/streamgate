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
mkdir -p "$VERIFY_ARTIFACTS"
VERIFY_ARTIFACTS="$(cd "$VERIFY_ARTIFACTS" && pwd)"
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
    VERIFY_*|PERF_*|COVERAGE_MIN) forward_env+=(-e "$name") ;;
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
