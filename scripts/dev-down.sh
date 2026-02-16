#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

RUN_DIR="${RUN_DIR:-$ROOT_DIR/.dev}"
USE_HOSTNET="${USE_HOSTNET:-auto}"   # auto | 0 | 1
STOP_POSTGRES="${STOP_POSTGRES:-1}"  # 0 | 1

log() {
  printf '[dev-down] %s\n' "$*"
}

stop_pid() {
  local name="$1"
  local pid_file="$RUN_DIR/$name.pid"
  if [[ ! -f "$pid_file" ]]; then
    return 0
  fi

  local pid
  pid="$(cat "$pid_file")"
  rm -f "$pid_file"

  if [[ -z "$pid" ]] || ! kill -0 "$pid" >/dev/null 2>&1; then
    log "$name is not running"
    return 0
  fi

  kill "$pid" >/dev/null 2>&1 || true
  for _ in $(seq 1 10); do
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      log "$name stopped"
      return 0
    fi
    sleep 1
  done

  kill -9 "$pid" >/dev/null 2>&1 || true
  log "$name killed"
}

stop_postgres() {
  if ! command -v docker >/dev/null 2>&1; then
    log "docker not found, skipping postgres stop"
    return 0
  fi

  local compose_mode=""
  if [[ -f "$RUN_DIR/compose_mode" ]]; then
    compose_mode="$(cat "$RUN_DIR/compose_mode")"
  fi

  stop_base() {
    docker compose -f docker-compose.yml stop postgres >/dev/null 2>&1 || true
  }
  stop_hostnet() {
    docker compose -f docker-compose.yml -f docker-compose.hostnet.yml stop postgres >/dev/null 2>&1 || true
  }

  case "$USE_HOSTNET" in
    1|true|TRUE)
      stop_hostnet
      ;;
    0|false|FALSE)
      stop_base
      ;;
    auto)
      if [[ "$compose_mode" == "hostnet" ]]; then
        stop_hostnet
      elif [[ "$compose_mode" == "base" ]]; then
        stop_base
      else
        stop_base
        if [[ -f "$ROOT_DIR/docker-compose.hostnet.yml" ]]; then
          stop_hostnet
        fi
      fi
      ;;
    *)
      stop_base
      if [[ -f "$ROOT_DIR/docker-compose.hostnet.yml" ]]; then
        stop_hostnet
      fi
      ;;
  esac

  rm -f "$RUN_DIR/compose_mode"
  log "postgres stopped"
}

stop_pid frontend
stop_pid worker
stop_pid backend

if [[ "$STOP_POSTGRES" == "1" ]]; then
  stop_postgres
fi

log "done"
