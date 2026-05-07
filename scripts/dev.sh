#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

COMPOSE_FILE="deploy/docker-compose.middleware.yml"
RUN_DIR=".run"
BACKEND_PID_FILE="$RUN_DIR/backend.pid"
FRONTEND_PID_FILE="$RUN_DIR/frontend.pid"
BACKEND_LOG="$RUN_DIR/backend.log"
FRONTEND_LOG="$RUN_DIR/frontend.log"
BACKEND_BIN="$RUN_DIR/backend-bin"
DOCKER_CMD=(docker)

mkdir -p "$RUN_DIR"

configure_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    echo "[dev] ERROR: docker command not found. Install Docker with Compose support before running this command."
    return 1
  fi
  if ! docker compose version >/dev/null 2>&1; then
    echo "[dev] ERROR: docker compose is not available. Install the Docker Compose v2 plugin."
    return 1
  fi

  DOCKER_CMD=(docker)
  if ! docker info >/dev/null 2>&1; then
    if command -v sudo >/dev/null 2>&1; then
      echo "[dev] Docker socket is not accessible by the current user; using sudo docker."
      if ! sudo -n docker info >/dev/null 2>&1; then
        echo "[dev] ERROR: sudo docker is not available without an interactive password prompt."
        echo "[dev] Run sudo -v first, add the user to the docker group, or run the script as a Docker-enabled user."
        return 1
      fi
      DOCKER_CMD=(sudo docker)
    else
      echo "[dev] ERROR: docker is installed, but the current user cannot access the Docker daemon socket."
      echo "[dev] Add the user to the docker group or run this script with a user that can access Docker."
      return 1
    fi
  fi
  return 0
}

require_docker() {
  configure_docker || exit 1
}

is_running() {
  local pid_file="$1"
  [ -f "$pid_file" ] && kill -0 "$(cat "$pid_file")" 2>/dev/null
}

port_is_listening() {
  local port="$1"
  wait_for_tcp 127.0.0.1 "$port" 1
}

stop_process_group() {
  local pid_file="$1"
  local name="$2"

  if [ ! -f "$pid_file" ]; then
    return 0
  fi

  local pid
  pid="$(cat "$pid_file")"
  if [ -z "$pid" ]; then
    rm -f "$pid_file"
    return 0
  fi

  if kill -0 "$pid" 2>/dev/null; then
    echo "[dev] Stopping $name process group (PID $pid)..."
    kill -- "-$pid" 2>/dev/null || kill "$pid" 2>/dev/null || true
    for _ in $(seq 1 10); do
      kill -0 "$pid" 2>/dev/null || break
      sleep 1
    done
    if kill -0 "$pid" 2>/dev/null; then
      echo "[dev] Force stopping $name process group (PID $pid)..."
      kill -9 -- "-$pid" 2>/dev/null || kill -9 "$pid" 2>/dev/null || true
    fi
  fi
  rm -f "$pid_file"
}

stop_orphan_listener() {
  local port="$1"
  local name="$2"

  if ! port_is_listening "$port"; then
    return 0
  fi

  echo "[dev] Stopping orphaned $name listener on port $port..."
  if command -v fuser >/dev/null 2>&1; then
    fuser -k -TERM "${port}/tcp" >/dev/null 2>&1 || true
    for _ in $(seq 1 10); do
      port_is_listening "$port" || return 0
      sleep 1
    done
    if port_is_listening "$port"; then
      fuser -k -KILL "${port}/tcp" >/dev/null 2>&1 || true
    fi
  else
    echo "[dev] WARNING: fuser is not available; cannot clean orphaned $name listener on port $port."
  fi
}

wait_for_tcp() {
  local host="$1"
  local port="$2"
  local max_attempts="$3"

  for i in $(seq 1 "$max_attempts"); do
    if command -v nc >/dev/null 2>&1; then
      nc -z "$host" "$port" 2>/dev/null && return 0
    else
      (echo >"/dev/tcp/$host/$port") >/dev/null 2>&1 && return 0
    fi
    sleep 1
  done
  return 1
}

wait_for_redis() {
  for _ in $(seq 1 60); do
    if command -v redis-cli >/dev/null 2>&1; then
      redis-cli -p 31029 ping 2>/dev/null | grep -q PONG && return 0
    elif wait_for_tcp 127.0.0.1 31029 1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

cmd_start() {
  require_docker

  echo "[dev] Starting middleware..."
  "${DOCKER_CMD[@]}" compose -f "$COMPOSE_FILE" up -d

  echo "[dev] Waiting for Redis on 31029..."
  if ! wait_for_redis; then
    echo "[dev] ERROR: Redis is not ready after 60s."
    exit 1
  fi
  echo "[dev] Redis ready."

  echo "[dev] Waiting for Milvus on 31953 (up to 60s)..."
  if wait_for_tcp 127.0.0.1 31953 60; then
    echo "[dev] Milvus ready."
  else
    if [ "${ALLOW_MILVUS_DEGRADED:-}" = "1" ]; then
      echo "[dev] WARNING: Milvus not ready after 60s, continuing because ALLOW_MILVUS_DEGRADED=1."
    else
      echo "[dev] ERROR: Milvus is not ready after 60s. Knowledge upload/retrieval requires Milvus."
      echo "[dev] Set ALLOW_MILVUS_DEGRADED=1 to start without Milvus for chat-only debugging."
      exit 1
    fi
  fi

  if [ ! -d "Front_page/node_modules" ]; then
    echo "[dev] node_modules not found, running npm install..."
    if ! (cd Front_page && npm install); then
      echo "[dev] ERROR: npm install failed. Aborting."
      exit 1
    fi
  fi

  if is_running "$BACKEND_PID_FILE"; then
    echo "[dev] Backend already running (PID $(cat "$BACKEND_PID_FILE"))."
  else
    rm -f "$BACKEND_PID_FILE"
    if port_is_listening 6872; then
      echo "[dev] ERROR: backend port 6872 is already in use, but $BACKEND_PID_FILE is not running."
      echo "[dev] Stop the existing process or free the port before starting."
      exit 1
    fi
    echo "[dev] Building backend..."
    go build -o "$BACKEND_BIN" main.go
    echo "[dev] Starting backend..."
    nohup setsid "$BACKEND_BIN" >"$BACKEND_LOG" 2>&1 &
    echo $! >"$BACKEND_PID_FILE"
    echo "[dev] Backend PID: $(cat "$BACKEND_PID_FILE")"
    echo "[dev] Waiting for backend on 6872..."
    if ! wait_for_tcp 127.0.0.1 6872 30; then
      echo "[dev] ERROR: backend did not become ready after 30s."
      tail -n 40 "$BACKEND_LOG" || true
      stop_process_group "$BACKEND_PID_FILE" "backend"
      exit 1
    fi
  fi

  if is_running "$FRONTEND_PID_FILE"; then
    echo "[dev] Frontend already running (PID $(cat "$FRONTEND_PID_FILE"))."
  else
    rm -f "$FRONTEND_PID_FILE"
    if port_is_listening 3000; then
      echo "[dev] ERROR: frontend port 3000 is already in use, but $FRONTEND_PID_FILE is not running."
      echo "[dev] Stop the existing process or free the port before starting."
      exit 1
    fi
    echo "[dev] Starting frontend..."
    nohup setsid bash -c "cd Front_page && exec npm run dev" >"$FRONTEND_LOG" 2>&1 &
    echo $! >"$FRONTEND_PID_FILE"
    echo "[dev] Frontend PID: $(cat "$FRONTEND_PID_FILE")"
    echo "[dev] Waiting for frontend on 3000..."
    if ! wait_for_tcp 127.0.0.1 3000 30; then
      echo "[dev] ERROR: frontend did not become ready after 30s."
      tail -n 40 "$FRONTEND_LOG" || true
      stop_process_group "$FRONTEND_PID_FILE" "frontend"
      exit 1
    fi
  fi

  echo ""
  echo "[dev] All services started."
  echo "  Backend:  http://localhost:6872"
  echo "  Frontend: http://localhost:3000"
  echo "  Logs:     $BACKEND_LOG / $FRONTEND_LOG"
}

cmd_stop() {
  echo "[dev] Stopping backend and frontend..."
  stop_process_group "$BACKEND_PID_FILE" "backend"
  stop_process_group "$FRONTEND_PID_FILE" "frontend"

  echo "[dev] Stopping middleware containers..."
  if ! configure_docker; then
    echo "[dev] WARNING: Docker is unavailable, so middleware containers were not stopped."
    echo "[dev] Local backend/frontend processes have been stopped."
    stop_orphan_listener 6872 "backend"
    stop_orphan_listener 3000 "frontend"
    return 0
  fi
  "${DOCKER_CMD[@]}" compose -f "$COMPOSE_FILE" down
  stop_orphan_listener 6872 "backend"
  stop_orphan_listener 3000 "frontend"
  echo "[dev] Done. Data volumes preserved."
}

cmd_restart() {
  cmd_stop
  sleep 2
  cmd_start
}

cmd_status() {
  echo "=== Backend ==="
  if is_running "$BACKEND_PID_FILE"; then
    echo "  RUNNING (PID $(cat "$BACKEND_PID_FILE"))"
  elif port_is_listening 6872; then
    echo "  UNKNOWN (port 6872 is listening, but $BACKEND_PID_FILE is stale or missing)"
  else
    rm -f "$BACKEND_PID_FILE"
    echo "  STOPPED"
  fi

  echo "=== Frontend ==="
  if is_running "$FRONTEND_PID_FILE"; then
    echo "  RUNNING (PID $(cat "$FRONTEND_PID_FILE"))"
  elif port_is_listening 3000; then
    echo "  UNKNOWN (port 3000 is listening, but $FRONTEND_PID_FILE is stale or missing)"
  else
    rm -f "$FRONTEND_PID_FILE"
    echo "  STOPPED"
  fi

  echo "=== Middleware ==="
  if configure_docker; then
    "${DOCKER_CMD[@]}" compose -f "$COMPOSE_FILE" ps
  else
    echo "  UNKNOWN (Docker unavailable)"
  fi
}

cmd_logs() {
  target="${1:-backend}"
  case "$target" in
    backend) tail -f "$BACKEND_LOG" ;;
    frontend) tail -f "$FRONTEND_LOG" ;;
    middleware)
      require_docker
      "${DOCKER_CMD[@]}" compose -f "$COMPOSE_FILE" logs -f
      ;;
    *)
      echo "Usage: $0 logs [backend|frontend|middleware]"
      exit 1
      ;;
  esac
}

cmd_clean_volumes() {
  require_docker

  echo "WARNING: This will DELETE all middleware data volumes (Redis, Milvus, etc.)."
  read -rp "Are you sure? Type 'yes' to confirm: " confirm
  if [ "$confirm" != "yes" ]; then
    echo "Aborted."
    exit 0
  fi
  "${DOCKER_CMD[@]}" compose -f "$COMPOSE_FILE" down -v
  echo "[dev] Volumes deleted."
}

case "${1:-}" in
  start) cmd_start ;;
  stop) cmd_stop ;;
  restart) cmd_restart ;;
  status) cmd_status ;;
  logs) cmd_logs "${2:-backend}" ;;
  clean-volumes) cmd_clean_volumes ;;
  *)
    echo "Usage: $0 {start|stop|restart|status|logs [backend|frontend|middleware]|clean-volumes}"
    exit 1
    ;;
esac
