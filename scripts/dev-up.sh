#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

RUN_DIR="${RUN_DIR:-$ROOT_DIR/.dev}"
LOG_DIR="$RUN_DIR/logs"
USE_HOSTNET="${USE_HOSTNET:-auto}" # auto | 0 | 1
START_SEED="${START_SEED:-1}"      # 0 | 1
FRONTEND_PORT="${FRONTEND_PORT:-3000}"
NEXT_PUBLIC_API_BASE_URL="${NEXT_PUBLIC_API_BASE_URL:-http://localhost:8080}"

mkdir -p "$LOG_DIR"

BACKEND_ENV_FILE=""
if [[ -f "$ROOT_DIR/backend/.env" ]]; then
  BACKEND_ENV_FILE="$ROOT_DIR/backend/.env"
elif [[ -f "$ROOT_DIR/.env.example" ]]; then
  BACKEND_ENV_FILE="$ROOT_DIR/.env.example"
fi

log() {
  printf '[dev-up] %s\n' "$*"
}

fail() {
  printf '[dev-up] FAIL: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  local cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || fail "required command not found: $cmd"
}

set_compose_mode() {
  local mode="$1"
  case "$mode" in
    base)
      COMPOSE_MODE="base"
      COMPOSE_FILES=(-f docker-compose.yml)
      ;;
    hostnet)
      COMPOSE_MODE="hostnet"
      COMPOSE_FILES=(-f docker-compose.yml -f docker-compose.hostnet.yml)
      ;;
    *)
      fail "unknown compose mode: $mode"
      ;;
  esac
}

compose() {
  docker compose "${COMPOSE_FILES[@]}" "$@"
}

check_pid_file() {
  local name="$1"
  local pid_file="$RUN_DIR/$name.pid"
  if [[ ! -f "$pid_file" ]]; then
    return 0
  fi

  local pid
  pid="$(cat "$pid_file")"
  if [[ -n "$pid" ]] && kill -0 "$pid" >/dev/null 2>&1; then
    fail "$name already running with PID $pid. Run scripts/dev-down.sh first."
  fi
  rm -f "$pid_file"
}

wait_for_http() {
  local name="$1"
  local url="$2"
  local max_attempts="${3:-60}"
  local attempt=1

  while [[ "$attempt" -le "$max_attempts" ]]; do
    if curl -fsS "$url" >/dev/null 2>&1; then
      log "$name is ready: $url"
      return 0
    fi
    sleep 1
    attempt=$((attempt + 1))
  done

  fail "$name did not become ready: $url"
}

start_process() {
  local name="$1"
  local command="$2"
  local log_file="$LOG_DIR/$name.log"
  local pid_file="$RUN_DIR/$name.pid"

  bash -lc "$command" >"$log_file" 2>&1 &
  local pid=$!
  echo "$pid" >"$pid_file"

  sleep 1
  if ! kill -0 "$pid" >/dev/null 2>&1; then
    fail "$name exited early. Check log: $log_file"
  fi

  log "$name started (pid=$pid, log=$log_file)"
}

start_postgres() {
  local compose_log="$RUN_DIR/compose-postgres.log"
  if compose up -d postgres >"$compose_log" 2>&1; then
    return 0
  fi

  if [[ "$USE_HOSTNET" == "auto" ]] && [[ "$COMPOSE_MODE" == "base" ]] && [[ -f "$ROOT_DIR/docker-compose.hostnet.yml" ]]; then
    if grep -Eqi 'operation not supported|veth' "$compose_log"; then
      log "bridge networking unavailable, retrying with host networking"
      set_compose_mode hostnet
      compose up -d postgres
      return 0
    fi
  fi

  cat "$compose_log" >&2
  fail "failed to start postgres via docker compose"
}

require_cmd docker
require_cmd go
require_cmd npm
require_cmd curl

case "$USE_HOSTNET" in
  1|true|TRUE)
    set_compose_mode hostnet
    ;;
  0|false|FALSE)
    set_compose_mode base
    ;;
  auto)
    set_compose_mode base
    ;;
  *)
    fail "USE_HOSTNET must be auto, 0, or 1"
    ;;
esac

check_pid_file backend
check_pid_file worker
check_pid_file frontend

# Stop dockerized app services so local processes can bind to ports.
docker compose -f docker-compose.yml stop backend worker frontend >/dev/null 2>&1 || true
if [[ -f "$ROOT_DIR/docker-compose.hostnet.yml" ]]; then
  docker compose -f docker-compose.yml -f docker-compose.hostnet.yml stop backend worker frontend >/dev/null 2>&1 || true
fi

log "starting postgres with compose mode: $COMPOSE_MODE"
start_postgres
echo "$COMPOSE_MODE" >"$RUN_DIR/compose_mode"

if [[ "$START_SEED" == "1" ]]; then
  log "running seed"
  compose --profile tools run --rm seed
fi

if [[ ! -d "$ROOT_DIR/frontend/node_modules" ]]; then
  log "installing frontend dependencies"
  (cd frontend && npm install)
fi

start_process \
  backend \
  "cd '$ROOT_DIR/backend' && if [[ -n '$BACKEND_ENV_FILE' ]]; then set -a; source '$BACKEND_ENV_FILE'; set +a; fi && exec go run ./cmd/api"

start_process \
  worker \
  "cd '$ROOT_DIR/backend' && if [[ -n '$BACKEND_ENV_FILE' ]]; then set -a; source '$BACKEND_ENV_FILE'; set +a; fi && exec go run ./cmd/worker"

start_process \
  frontend \
  "cd '$ROOT_DIR/frontend' && export NEXT_PUBLIC_API_BASE_URL='$NEXT_PUBLIC_API_BASE_URL' && exec npm run dev -- --port '$FRONTEND_PORT'"

wait_for_http backend "http://localhost:8080/healthz" 60
wait_for_http frontend "http://localhost:${FRONTEND_PORT}" 90

log "all services are up"
log "backend:  http://localhost:8080"
log "frontend: http://localhost:${FRONTEND_PORT}"
log "stop with: ./scripts/dev-down.sh"
