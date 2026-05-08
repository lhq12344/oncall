#!/usr/bin/env bash
# 本地开发脚本，适用于对话 / 知识库 / 搜索应用。
#
# 用法：
#   ./scripts/dev.sh start
#   ./scripts/dev.sh stop
#   ./scripts/dev.sh restart
#   ./scripts/dev.sh status
#   ./scripts/dev.sh logs backend
#   ./scripts/dev.sh logs frontend
#   ./scripts/dev.sh logs middleware
#   ./scripts/dev.sh clean-volumes
#
# 说明：
#   - start 会先通过 Docker Compose 启动 Redis、Milvus、Attu 等中间件，再启动后端(:6872)和前端(:3100)。
#   - stop 会关闭本地应用进程和中间件容器，默认保留 Docker volume 数据。
#   - restart 只重启后端和前端，不重启中间件容器。
#   - clean-volumes 会在明确确认后删除 Redis / Milvus 的持久化数据。
#   - 如果 Docker 需要 sudo，请先执行 `sudo -v`，再运行 start / status / logs / stop。
#
set -euo pipefail

if [ "${EUID:-$(id -u)}" -eq 0 ] && [ -n "${SUDO_USER:-}" ]; then
  echo "[dev] ERROR: do not run this script with sudo."
  echo "[dev] Run 'sudo -v' first if Docker needs sudo, then run './scripts/dev.sh ${1:-start}' as your normal user."
  exit 1
fi

BACKEND_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKSPACE_ROOT="$(cd "$BACKEND_ROOT/.." && pwd)"
FRONTEND_ROOT="$WORKSPACE_ROOT/Front_page"
cd "$BACKEND_ROOT"

COMPOSE_FILE="deploy/docker-compose.middleware.yml"
RUN_DIR=".run"
BACKEND_PID_FILE="$RUN_DIR/backend.pid"
FRONTEND_PID_FILE="$RUN_DIR/frontend.pid"
BACKEND_LOG="$RUN_DIR/backend.log"
FRONTEND_LOG="$RUN_DIR/frontend.log"
BACKEND_BIN="$RUN_DIR/backend-bin"
BACKEND_PORT="${BACKEND_PORT:-6872}"
FRONTEND_PORT="${FRONTEND_PORT:-3100}"
ATTU_PORT="${ATTU_PORT:-8000}"
DOCKER_CMD=(docker)
export ATTU_PORT

mkdir -p "$RUN_DIR"

require_command() {
  local command_name="$1"
  local install_hint="$2"

  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "[dev] ERROR: $command_name command not found. $install_hint"
    exit 1
  fi
}

load_env_file() {
  if [ ! -f ".env" ]; then
    return 0
  fi

  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
}

read_yaml_block_value() {
  local block="$1"
  local key="$2"
  local file="$3"

  if [ ! -f "$file" ]; then
    return 0
  fi

  awk -v block="$block" -v key="$key" '
    $0 ~ "^[[:space:]]*" block ":[[:space:]]*$" {
      in_block = 1
      next
    }
    in_block && $0 ~ "^[^[:space:]#][^:]*:" {
      in_block = 0
    }
    in_block && $0 ~ "^[[:space:]]*" key ":[[:space:]]*" {
      value = $0
      sub("^[[:space:]]*" key ":[[:space:]]*", "", value)
      sub(/[[:space:]]+#.*$/, "", value)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      gsub(/^["'\''"]|["'\''"]$/, "", value)
      print value
      exit
    }
  ' "$file"
}

has_backend_model_env_config() {
  [ -n "${DS_QUICK_CHAT_MODEL_API_KEY:-}" ] &&
    [ -n "${DS_QUICK_CHAT_MODEL_BASE_URL:-}" ] &&
    [ -n "${DS_QUICK_CHAT_MODEL_MODEL:-}" ]
}

has_backend_model_yaml_config() {
  local config_file="manifest/config/config.yaml"
  local api_key base_url model

  api_key="$(read_yaml_block_value "ds_quick_chat_model" "api_key" "$config_file")"
  base_url="$(read_yaml_block_value "ds_quick_chat_model" "base_url" "$config_file")"
  model="$(read_yaml_block_value "ds_quick_chat_model" "model" "$config_file")"

  [ -n "$api_key" ] && [ -n "$base_url" ] && [ -n "$model" ]
}

require_backend_model_config() {
  load_env_file

  if has_backend_model_env_config || has_backend_model_yaml_config; then
    return 0
  fi

  echo "[dev] ERROR: chat model config is incomplete."
  echo "[dev] Set ds_quick_chat_model.{api_key,base_url,model} in manifest/config/config.yaml"
  echo "[dev] or set DS_QUICK_CHAT_MODEL_* in .env / shell before starting."
  exit 1
}

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
    if port_is_listening "$port"; then
      echo "[dev] WARNING: could not stop orphaned $name listener on port $port."
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
  local skip_middleware="${1:-}"
  local attu_ready=0

  require_command go "Install Go or add it to PATH before starting the backend."
  require_command npm "Install Node.js/npm before starting the frontend."
  require_backend_model_config

  if [ "$skip_middleware" = "--skip-middleware" ]; then
    echo "[dev] Skipping middleware startup; using already-running middleware."
  else
    if configure_docker; then
      echo "[dev] Starting middleware..."
      "${DOCKER_CMD[@]}" compose -f "$COMPOSE_FILE" up -d
    else
      echo "[dev] WARNING: Docker is unavailable; using already-running middleware if reachable."
    fi
  fi

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

  echo "[dev] Waiting for Attu on $ATTU_PORT (up to 60s)..."
  if wait_for_tcp 127.0.0.1 "$ATTU_PORT" 60; then
    echo "[dev] Attu ready."
    attu_ready=1
  else
    echo "[dev] WARNING: Attu is not ready after 60s."
    echo "[dev] Run './scripts/dev.sh start' after Docker access is available, or check middleware logs."
  fi

  if [ ! -d "$FRONTEND_ROOT" ]; then
    echo "[dev] ERROR: frontend directory not found: $FRONTEND_ROOT"
    exit 1
  fi

  if [ ! -d "$FRONTEND_ROOT/node_modules" ]; then
    echo "[dev] node_modules not found, running npm install..."
    if ! (cd "$FRONTEND_ROOT" && npm install); then
      echo "[dev] ERROR: npm install failed. Aborting."
      exit 1
    fi
  fi

  if is_running "$BACKEND_PID_FILE"; then
    echo "[dev] Backend already running (PID $(cat "$BACKEND_PID_FILE"))."
  else
    rm -f "$BACKEND_PID_FILE"
    if port_is_listening "$BACKEND_PORT"; then
      stop_orphan_listener "$BACKEND_PORT" "backend"
    fi
    if port_is_listening "$BACKEND_PORT"; then
      echo "[dev] ERROR: backend port $BACKEND_PORT is already in use, but $BACKEND_PID_FILE is not running."
      echo "[dev] Stop the existing process, free the port, or start with BACKEND_PORT=<free-port>."
      exit 1
    fi
    echo "[dev] Building backend..."
    go build -o "$BACKEND_BIN" main.go
    echo "[dev] Starting backend..."
    nohup setsid env BACKEND_PORT="$BACKEND_PORT" "$BACKEND_BIN" >"$BACKEND_LOG" 2>&1 &
    echo $! >"$BACKEND_PID_FILE"
    echo "[dev] Backend PID: $(cat "$BACKEND_PID_FILE")"
    echo "[dev] Waiting for backend on $BACKEND_PORT..."
    if ! wait_for_tcp 127.0.0.1 "$BACKEND_PORT" 90; then
      echo "[dev] ERROR: backend did not become ready after 90s."
      tail -n 40 "$BACKEND_LOG" || true
      stop_process_group "$BACKEND_PID_FILE" "backend"
      exit 1
    fi
  fi

  if is_running "$FRONTEND_PID_FILE"; then
    echo "[dev] Frontend already running (PID $(cat "$FRONTEND_PID_FILE"))."
  else
    rm -f "$FRONTEND_PID_FILE"
    if port_is_listening "$FRONTEND_PORT"; then
      stop_orphan_listener "$FRONTEND_PORT" "frontend"
    fi
    if port_is_listening "$FRONTEND_PORT"; then
      echo "[dev] ERROR: frontend port $FRONTEND_PORT is already in use, but $FRONTEND_PID_FILE is not running."
      echo "[dev] Stop the existing process or free the port before starting."
      exit 1
    fi
    echo "[dev] Starting frontend..."
    nohup setsid env VITE_API_BASE_URL="${VITE_API_BASE_URL:-}" VITE_BACKEND_PORT="$BACKEND_PORT" bash -c 'cd "$1" && exec ./node_modules/.bin/vite --port="$2" --host=0.0.0.0' bash "$FRONTEND_ROOT" "$FRONTEND_PORT" >"$FRONTEND_LOG" 2>&1 &
    echo $! >"$FRONTEND_PID_FILE"
    echo "[dev] Frontend PID: $(cat "$FRONTEND_PID_FILE")"
    echo "[dev] Waiting for frontend on $FRONTEND_PORT..."
    if ! wait_for_tcp 127.0.0.1 "$FRONTEND_PORT" 30; then
      echo "[dev] ERROR: frontend did not become ready after 30s."
      tail -n 40 "$FRONTEND_LOG" || true
      stop_process_group "$FRONTEND_PID_FILE" "frontend"
      exit 1
    fi
  fi

  echo ""
  echo "[dev] All services started."
  echo "  Backend:  http://localhost:$BACKEND_PORT"
  echo "  Frontend: http://localhost:$FRONTEND_PORT"
  if [ "$attu_ready" -eq 1 ]; then
    echo "  Attu:     http://localhost:$ATTU_PORT"
  else
    echo "  Attu:     not ready on localhost:$ATTU_PORT"
  fi
  echo "  Logs:     $BACKEND_LOG / $FRONTEND_LOG"
}

stop_app_processes() {
  echo "[dev] Stopping backend and frontend..."
  stop_process_group "$BACKEND_PID_FILE" "backend"
  stop_process_group "$FRONTEND_PID_FILE" "frontend"
  stop_orphan_listener "$BACKEND_PORT" "backend"
  stop_orphan_listener "$FRONTEND_PORT" "frontend"
}

cmd_stop() {
  stop_app_processes

  echo "[dev] Stopping middleware containers..."
  if ! configure_docker; then
    echo "[dev] WARNING: Docker is unavailable, so middleware containers were not stopped."
    echo "[dev] Local backend/frontend processes have been stopped."
    return 0
  fi
  "${DOCKER_CMD[@]}" compose -f "$COMPOSE_FILE" down
  echo "[dev] Done. Data volumes preserved."
}

cmd_restart() {
  stop_app_processes
  sleep 2
  cmd_start --skip-middleware
}

cmd_status() {
  echo "=== Backend ==="
  if is_running "$BACKEND_PID_FILE"; then
    echo "  RUNNING (PID $(cat "$BACKEND_PID_FILE"))"
  elif port_is_listening "$BACKEND_PORT"; then
    echo "  UNKNOWN (port $BACKEND_PORT is listening, but $BACKEND_PID_FILE is stale or missing)"
  else
    rm -f "$BACKEND_PID_FILE"
    echo "  STOPPED"
  fi

  echo "=== Frontend ==="
  if is_running "$FRONTEND_PID_FILE"; then
    echo "  RUNNING (PID $(cat "$FRONTEND_PID_FILE"))"
  elif port_is_listening "$FRONTEND_PORT"; then
    echo "  UNKNOWN (port $FRONTEND_PORT is listening, but $FRONTEND_PID_FILE is stale or missing)"
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

  echo "=== Attu ==="
  if port_is_listening "$ATTU_PORT"; then
    echo "  RUNNING (http://localhost:$ATTU_PORT)"
  else
    echo "  STOPPED (port $ATTU_PORT is not listening)"
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
