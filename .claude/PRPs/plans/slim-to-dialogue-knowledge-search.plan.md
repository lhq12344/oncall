# Plan: 精简为对话/知识/搜索应用，并补充一键启动

## Summary
将 OnCall 系统从全量运维平台精简为对话+知识库+网络搜索三合一应用。后端移除 OpsAgent 初始化路径、AIOps HTTP 接口及 DialogueAgent 中的运维工具（K8s/Prometheus/Bash/ops_case），前端移除 OpsPanel 及相关 store 状态，新增 `deploy/docker-compose.middleware.yml` 和 `scripts/dev.sh` 实现本地一键启停。

## User Story
As a 开发者，I want 一条命令就能启动完整的对话+知识库+搜索服务，so that 可以快速进入本地开发和调试，不再需要手动管理多个进程或运维工具配置。

## Problem → Solution
多个运维子系统（OpsAgent/K8s/Prometheus/Bash）混入对话路径，启动链路复杂、依赖多，没有一键启停脚本。→ 精简对话工具集到 4 个，移除 AIOps HTTP 入口，移除前端 OpsPanel，新增幂等的 Docker Compose 中间件配置和 dev.sh 脚本。

## Metadata
- **Complexity**: Large
- **Source PRD**: N/A
- **PRD Phase**: N/A
- **Estimated Files**: 11 files modified/created

---

## UX Design

### Before
```
┌─ Header ──────────────────────────────────────────────┐
│  [Sidebar toggle]  [AI Ops 执行中心 ▼]  [time] [theme]│
└────────────────────────────────────────────────────────┘
        OpsPanel (dropdown, full-width overlay)
        includes: step list, interrupt cards, RunOps btn
```

### After
```
┌─ Header ──────────────────────────────────────────────┐
│  [Sidebar toggle]  [time]  [theme]                     │
└────────────────────────────────────────────────────────┘
        No OpsPanel, no AI Ops button
        Chat/Upload/Knowledge flows unchanged
```

### Interaction Changes
| Touchpoint | Before | After | Notes |
|---|---|---|---|
| Header | AI Ops 按钮 + OpsPanel 下拉 | 移除 | OpsPanel.tsx 文件保留但不渲染 |
| /ai_ops_stream POST | 存在，返回 SSE | 404 | 路由不绑定到 GoFrame |
| /monitoring GET | 存在，返回空数据 | 404 | 同上 |
| DialogueAgent tools | 8 个（含 K8s/Prom/Bash/ops_case） | 4 个 | 见工具列表 |
| `./scripts/dev.sh start` | 不存在 | 一键启动 | 新增 |

---

## Mandatory Reading

| Priority | File | Lines | Why |
|---|---|---|---|
| P0 | `internal/agent/dialogue/agent.go` | 210-235 | `buildDialogueTools` — 要删 4 个工具注册 |
| P0 | `internal/bootstrap/app.go` | 140-215 | 初始化链路 — 要移除 opsIntegration/opsAgent/podLogShipper |
| P0 | `main.go` | 全文 | 传给 bootstrap 的字段、传给 NewV1 的参数 |
| P0 | `internal/controller/chat/chat_v1.go` | 62-135 | NewV1 构造及 opsStreamRunner 初始化 |
| P0 | `internal/controller/chat/chat_v1.go` | 434-600 | AIOpsStream/AIOpsResumeStream/Monitoring — 要删方法 |
| P0 | `api/chat/v1/chat.go` | 全文 | AIOpsStreamReq/Res、MonitoringReq/Res — 要删 struct |
| P1 | `Front_page/src/components/Header.tsx` | 全文 | OpsPanel import + handleOpsClick + AI Ops 按钮 |
| P1 | `Front_page/src/store/useStore.ts` | 全文 | Ops state 字段 + runOps/clearOps/addOpsStep 等 |
| P1 | `Front_page/src/services/api.ts` | 全文 | streamOps/resumeOps 函数 |
| P2 | `Front_page/src/types.ts` | 全文 | OpsStep 类型 |
| P2 | `internal/agent/dialogue/tools/K8sMonitorTool.go` | 全文 | 薄包装，只是转调 opstools |

## External Documentation
| Topic | Source | Key Takeaway |
|---|---|---|
| Milvus Standalone Docker Compose | milvus.io/docs/install_standalone-docker-compose.md | 官方 compose 需要 etcd + minio，Milvus 镜像 `milvusdb/milvus:v2.4.23` |

---

## Patterns to Mirror

### NAMING_CONVENTION
```go
// SOURCE: internal/agent/dialogue/agent.go:212
func buildDialogueTools(ctx context.Context, cfg *Config, ...) []tool.BaseTool {
// 函数以 build 前缀，返回切片；工具文件命名 XxxTool.go，类型 XxxTool struct
```

### ERROR_HANDLING
```go
// SOURCE: internal/bootstrap/app.go:133
if err != nil {
    return nil, fmt.Errorf("failed to create dialogue agent: %w", err)
}
// 可降级的组件（embedder）使用 logger.Warn + fallback nil，不 return error
```

### LOGGING_PATTERN
```go
// SOURCE: internal/bootstrap/app.go:145
logger.Info("initializing dialogue chat agent")
// 启动关键步骤用 logger.Info；可选依赖失败用 logger.Warn；硬错误用 log.Fatalf (main.go)
```

### SSE_PATTERN
```go
// SOURCE: internal/controller/chat/chat_v1.go:811-840
r, err := setupSSE(ctx)
writeSSEData(r, fmt.Sprintf("{\"type\":\"content\",\"content\":%q}", content))
writeSSEData(r, "{\"type\":\"done\"}")
// SSE 事件 JSON 格式固定；type 字段驱动前端分支
```

### FRONTEND_STORE_PATTERN
```typescript
// SOURCE: Front_page/src/store/useStore.ts
interface AppState {
  // 增删字段时同步 partialize 函数（决定 localStorage 持久化哪些字段）
  partialize: (state) => ({ sessions, theme, isSidebarOpen })
// OpsStep/opsSteps/currentOpsTask 从 partialize 中删去
```

### TEST_STRUCTURE
```go
// SOURCE: utility/mem/mem_test.go (pattern)
func TestXxx(t *testing.T) {
    // 标准 go test，表驱动，无外部测试框架
```

---

## Files to Change

| File | Action | Justification |
|---|---|---|
| `internal/agent/dialogue/agent.go` | UPDATE | 从 `buildDialogueTools` 移除 4 个工具；Config 删 PrometheusURL/KubeConfig 字段 |
| `internal/bootstrap/app.go` | UPDATE | 移除 opsIntegration/opsAgent/podLogShipper 初始化；Config 删对应字段；Application 删对应字段 |
| `main.go` | UPDATE | 删除传给 bootstrap 的 prometheus/kubeconfig/logsync 字段；删除传给 NewV1 的 opsAgent 参数 |
| `internal/controller/chat/chat_v1.go` | UPDATE | 删 opsAgent/opsStreamRunner/opsRootAgentName 字段；删 AIOpsStream/AIOpsResumeStream/Monitoring 方法；NewV1 去掉 opsAgent 参数 |
| `api/chat/v1/chat.go` | UPDATE | 删除 AIOpsStreamReq/Res、AIOpsResumeStreamReq/Res、MonitoringReq/Res struct |
| `Front_page/src/components/Header.tsx` | UPDATE | 移除 OpsPanel import、runOps/isOpsPanelOpen/setOpsPanelOpen/isOpsRunning 引用、AI Ops 按钮、`<AnimatePresence><OpsPanel/></AnimatePresence>` |
| `Front_page/src/store/useStore.ts` | UPDATE | AppState 删 isOpsPanelOpen/opsSteps/currentOpsTask/isOpsRunning；删 runOps/clearOps/addOpsStep/updateOpsStep/markOpsInterruptHandled/setOpsRunning/setOpsPanelOpen；partialize 删 opsSteps/currentOpsTask |
| `Front_page/src/services/api.ts` | UPDATE | 删除 streamOps/resumeOps 函数 |
| `Front_page/src/types.ts` | UPDATE | 删除 OpsStep 类型 |
| `deploy/docker-compose.middleware.yml` | CREATE | Redis + Milvus Standalone (etcd + minio) |
| `scripts/dev.sh` | CREATE | start/stop/restart/status/logs/clean-volumes 命令 |

## NOT Building
- 物理删除 `internal/agent/ops/`、`internal/agent/rca/`、`internal/agent/execution/`、`internal/agent/strategy/`（阶段 2）
- `go.mod` 瘦身（阶段 2，在物理删除后 `go mod tidy`）
- 对话工具 `BashApprovalTool` 的完整删除（保留文件，仅从工具列表移除注册）
- Elasticsearch 相关代码删除（仅断开 OpsAgent/PodLogShipper 引用，`utility/elasticsearch/` 保留）
- 前端容器化
- Kubernetes 部署变更

---

## Step-by-Step Tasks

### Task 1: 精简 DialogueAgent 工具集
- **ACTION**: 编辑 `internal/agent/dialogue/agent.go`
- **IMPLEMENT**:
  1. `Config` struct 删除 `PrometheusURL string` 和 `KubeConfig string` 两个字段
  2. `buildDialogueTools` 函数中移除以下 4 行：
     ```go
     tools.NewOpsCaseRetrieveTool(opsCaseRetriever, cfg.Logger),
     tools.NewBashApprovalTool(cfg.Logger),
     // 以及 k8sTool 和 metricsTool 的 if 块（共 10 行）
     ```
  3. `NewDialogueAgent` 中移除 `opsCaseRetriever` 及其 `NewMilvusRetrieverWithCollection` 调用（约 10 行）
  4. 函数签名 `buildDialogueTools` 的参数删去 `opsCaseRetriever einoretriever.Retriever`
  5. 保留：`NewIntentAnalysisTool`, `NewDetailSelectionTool`, `NewKnowledgeRetrieveTool`, `NewWebSearchTool`
- **MIRROR**: NAMING_CONVENTION, ERROR_HANDLING
- **IMPORTS**: 删除 `"go_agent/utility/common"` import（如果只被 `common.MilvusOpsCollection` 使用）；删除 `opstools` 相关的间接 import（通过 K8sMonitorTool/MetricsCollectorTool 引入的 opstools 已不再使用）
- **GOTCHA**: `knowledgeRetriever` 仍保留（用于 `KnowledgeRetrieveTool`）；`cfg.Embedder` 仍保留（用于 `IntentAnalysisTool`）
- **VALIDATE**: `go build ./internal/agent/dialogue/...` 无错误

### Task 2: 精简 bootstrap 初始化链路
- **ACTION**: 编辑 `internal/bootstrap/app.go`
- **IMPLEMENT**:
  1. `Config` struct 删除字段：`PrometheusURL`, `KubeConfig`, `LogSyncEnabled`, `LogSyncNamespaces`, `LogSyncInterval`, `LogSyncTailLines`, `LogSyncIndexPrefix`
  2. `Application` struct 删除字段：`OpsIntegration *ops.IntegratedOpsExecutor`, `OpsAgent adk.Agent`
  3. `NewApplication` 删除以下初始化块（按顺序）：
     - `dialogue.Config` 中的 `KubeConfig` 和 `PrometheusURL` 两行
     - `opsIntegration` 初始化（约 10 行，含 logger.Warn）
     - `opsAgent` 初始化（约 8 行）
     - `podLogShipper` 初始化（约 12 行）
  4. `NewApplication` return 语句删去 `OpsIntegration` 和 `OpsAgent` 字段
  5. `startBackgroundTasks` 删除 `podLogShipper *ops.PodLogShipper` 参数及相关逻辑
  6. 删除 import：`"go_agent/internal/agent/ops"`（如果不再引用）
- **MIRROR**: ERROR_HANDLING, LOGGING_PATTERN
- **IMPORTS**: 若 `ops` 包完全不被引用，从 import block 删去
- **GOTCHA**: `embedder` 仍需保留（传给 `dialogue.Config.Embedder`）；`knowledgeAgent` 仍需初始化
- **VALIDATE**: `go build ./internal/bootstrap/...` 无错误

### Task 3: 精简 main.go
- **ACTION**: 编辑 `main.go`
- **IMPLEMENT**:
  1. 删除从 `g.Cfg()` 读取的以下变量：`prometheusURL`, `kubeConfig`, `logSyncEnabled`, `logSyncNamespaces`, `logSyncInterval`, `logSyncTailLines`, `logSyncIndexPrefix`
  2. `bootstrap.Config{}` 字面量删去对应字段：`PrometheusURL`, `KubeConfig`, `LogSyncEnabled`, `LogSyncNamespaces`, `LogSyncInterval`, `LogSyncTailLines`, `LogSyncIndexPrefix`
  3. `chat.NewV1(...)` 调用删去 `app.OpsAgent` 参数（见 Task 4，NewV1 签名要同步变更）
  4. 删除 `log.Printf("Prometheus URL: %s", prometheusURL.String())` 这行 log
- **MIRROR**: LOGGING_PATTERN
- **IMPORTS**: 若 `es` 包不再需要可保留（Elasticsearch 降级逻辑还在 main.go 中，按计划保留）
- **GOTCHA**: Elasticsearch 初始化代码在 main.go 保留（只断开 OpsAgent 引用，不删 ES 本身）
- **VALIDATE**: `go build .` 无错误

### Task 4: 精简 Controller
- **ACTION**: 编辑 `internal/controller/chat/chat_v1.go`
- **IMPLEMENT**:
  1. `ControllerV1` struct 删除字段：`opsStreamRunner *adk.Runner`, `opsRootAgentName string`, `opsAgent adk.Agent`
  2. `NewV1` 函数签名删去 `opsAgent adk.Agent` 参数
  3. `NewV1` 函数体删去 `ctrl.opsAgent = opsAgent` 和 `ctrl.opsRootAgentName = "ops_agent"` 及 opsStreamRunner 初始化块（约 10 行）
  4. 删除整个 `AIOpsStream` 方法（L434-504，约 70 行）
  5. 删除整个 `AIOpsResumeStream` 方法（L505-575，约 70 行）
  6. 删除整个 `Monitoring` 方法（L576-584，约 10 行）
  7. 删除辅助函数 `formatAIOpsContent`（L1192-1208）和 `isFinalReportContent`（L1208-1219）（这两个只被 AIOps 方法使用）
  8. `extractBashToolResultByMessage`（L732-760）和 `isBashExecuteResult`（L760-811）保留——ChatStream 路径 L664 仍调用它们（因为 BashApprovalTool 的文件保留）；若 Task 1 已从工具列表移除了 BashApprovalTool，则这两个函数可在阶段 2 一起清理；**本次保留，不删**
- **MIRROR**: SSE_PATTERN
- **IMPORTS**: 删除 `v1.AIOpsStreamReq` 等相关引用后，检查 import 是否还需要 `"strings"` 等（搜索其余用途后决定）
- **GOTCHA**: `opsDiagnosticPrompt` 常量（L31 附近）只被 AIOpsStream 使用，随之删去
- **VALIDATE**: `go build ./internal/controller/...` 无错误；`go test ./internal/controller/...` 通过

### Task 5: 精简 API struct
- **ACTION**: 编辑 `api/chat/v1/chat.go`
- **IMPLEMENT**: 删除以下 struct（保留 `ChatStreamReq/Res`, `ChatResumeStreamReq/Res`, `FileUploadReq/Res`, `InterruptContext`）：
  ```go
  // 删除：
  type AIOpsStreamReq struct { ... }
  type AIOpsStreamRes struct{}
  type AIOpsResumeStreamReq struct { ... }
  type AIOpsResumeStreamRes struct{}
  type MonitoringReq struct { ... }
  type MonitoringRes struct { ... }
  type CircuitBreakerStatus struct { ... }
  ```
- **MIRROR**: NAMING_CONVENTION
- **IMPORTS**: 无变化
- **GOTCHA**: GoFrame 使用 `g.Meta` tag 自动注册路由；删除 struct 后对应路由自动消失（无需手动 `v1Group.Bind` 修改）
- **VALIDATE**: `go build ./api/...` 无错误；`go run main.go` 后 `/ai_ops_stream` 返回 404

### Task 6: 精简前端 Header
- **ACTION**: 编辑 `Front_page/src/components/Header.tsx`
- **IMPLEMENT**:
  1. 删除 import：`import { OpsPanel } from './OpsPanel'`；`import { AnimatePresence } from 'motion/react'`（确认其他地方没用）
  2. `useStore()` 解构删去：`runOps`, `isOpsPanelOpen`, `setOpsPanelOpen`, `isOpsRunning`
  3. 删除 `handleOpsClick` 函数（约 10 行）
  4. 删除 Header JSX 中的 AI Ops 按钮（`<button onClick={handleOpsClick}>...</button>`，约 20 行）
  5. 状态指示器文案：`isOpsRunning ? 'Ops Executing...' :` 这段三元判断删去，直接用 `connectionStatus === 'streaming'` 分支
  6. 删除 `<AnimatePresence>{isOpsPanelOpen && <OpsPanel />}</AnimatePresence>` 返回块
- **MIRROR**: FRONTEND_STORE_PATTERN
- **GOTCHA**: `Activity` icon import（来自 lucide-react）随 AI Ops 按钮删去；`ChevronDown` 同理；如果它们不被其他 JSX 用到，从 import 删去
- **VALIDATE**: `cd Front_page && npm run lint` 无 TypeScript 错误

### Task 7: 精简前端 Store
- **ACTION**: 编辑 `Front_page/src/store/useStore.ts`
- **IMPLEMENT**:
  1. `AppState` interface 删除字段：`isOpsPanelOpen: boolean`, `opsSteps: OpsStep[]`, `currentOpsTask: string`, `isOpsRunning: boolean`
  2. `AppState` interface 删除方法签名：`setOpsPanelOpen`, `runOps`, `clearOps`, `addOpsStep`, `updateOpsStep`, `markOpsInterruptHandled`, `setOpsRunning`
  3. `create()(persist(...))` 中删除初始值：`isOpsPanelOpen: false`, `opsSteps: []`, `currentOpsTask: ''`, `isOpsRunning: false`
  4. 删除实现函数体：`setOpsPanelOpen`, `clearOps`, `addOpsStep`, `updateOpsStep`, `markOpsInterruptHandled`, `setOpsRunning`, `runOps`（runOps 约 50 行）
  5. `partialize` 函数删去：`opsSteps: state.opsSteps`, `currentOpsTask: state.currentOpsTask`
  6. 删除 import：`import { ..., AIOpsStep, ..., OpsStep } from '../types'` 中删去 `OpsStep`
  7. 删除辅助函数 `inferOpsStepTitle`（约 10 行）
- **MIRROR**: FRONTEND_STORE_PATTERN
- **GOTCHA**: `AIOpsStep` 仍被 `Message.steps` 使用，**保留**；只删 `OpsStep`；`mergeMessageSteps` 函数用于 `updateLastMessage`，**保留**
- **VALIDATE**: `cd Front_page && npm run lint` 通过

### Task 8: 精简前端 API 服务
- **ACTION**: 编辑 `Front_page/src/services/api.ts`
- **IMPLEMENT**:
  1. 删除 `streamOps` 函数（约 5 行）
  2. 删除 `resumeOps` 函数（约 10 行）
  3. 删除 import 中 `AIOpsStep` 若不再被 `api.ts` 自身使用（检查 `StreamOptions.onStep` 仍需要它）
- **MIRROR**: NAMING_CONVENTION
- **GOTCHA**: `StreamOptions.onStep` 使用 `AIOpsStep` 类型，**保留** import
- **VALIDATE**: `cd Front_page && npm run lint` 通过

### Task 9: 精简 types.ts
- **ACTION**: 编辑 `Front_page/src/types.ts`
- **IMPLEMENT**: 删除 `OpsStep` interface（约 7 行）；保留所有其他类型
- **VALIDATE**: `cd Front_page && npm run lint` 通过

### Task 10: 创建 deploy/docker-compose.middleware.yml
- **ACTION**: CREATE `deploy/docker-compose.middleware.yml`
- **IMPLEMENT**:
```yaml
version: '3.8'

services:
  redis:
    image: redis:7-alpine
    ports:
      - "127.0.0.1:31029:6379"
    volumes:
      - redis_data:/data
    command: redis-server --appendonly yes
    restart: unless-stopped

  etcd:
    image: quay.io/coreos/etcd:v3.5.18
    environment:
      ETCD_AUTO_COMPACTION_MODE: revision
      ETCD_AUTO_COMPACTION_RETENTION: "1000"
      ETCD_QUOTA_BACKEND_BYTES: "4294967296"
      ETCD_SNAPSHOT_COUNT: "50000"
    volumes:
      - etcd_data:/etcd
    command: etcd -advertise-client-urls=http://127.0.0.1:2379 -listen-client-urls http://0.0.0.0:2379 --data-dir /etcd
    restart: unless-stopped

  minio:
    image: minio/minio:RELEASE.2023-03-13T19-46-17Z
    environment:
      MINIO_ACCESS_KEY: minioadmin
      MINIO_SECRET_KEY: minioadmin
    volumes:
      - minio_data:/minio_data
    command: minio server /minio_data --console-address ":9001"
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9000/minio/health/live"]
      interval: 30s
      timeout: 20s
      retries: 3

  milvus:
    image: milvusdb/milvus:v2.4.23
    command: ["milvus", "run", "standalone"]
    security_opt:
      - seccomp:unconfined
    environment:
      ETCD_ENDPOINTS: etcd:2379
      MINIO_ADDRESS: minio:9000
    volumes:
      - milvus_data:/var/lib/milvus
    ports:
      - "127.0.0.1:31953:19530"
    depends_on:
      - etcd
      - minio
    restart: unless-stopped

volumes:
  redis_data:
  etcd_data:
  minio_data:
  milvus_data:
```
- **GOTCHA**: Milvus v2.4 官方 compose 需要 etcd 和 minio；etcd/minio 不暴露宿主机端口（只有 milvus:19530→31953 和 redis:6379→31029 暴露）；端口与 `manifest/config/config.yaml` 中的 `redis.addr: localhost:31029` 和 Milvus `addr: 127.0.0.1:31953` 完全匹配
- **VALIDATE**: `docker compose -f deploy/docker-compose.middleware.yml up -d` 所有容器 healthy

### Task 11: 创建 scripts/dev.sh
- **ACTION**: CREATE `scripts/dev.sh`（`chmod +x`）
- **IMPLEMENT**:
```bash
#!/usr/bin/env bash
set -euo pipefail

# 固定到仓库根目录运行
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

COMPOSE_FILE="deploy/docker-compose.middleware.yml"
RUN_DIR=".run"
BACKEND_PID_FILE="$RUN_DIR/backend.pid"
FRONTEND_PID_FILE="$RUN_DIR/frontend.pid"
BACKEND_LOG="$RUN_DIR/backend.log"
FRONTEND_LOG="$RUN_DIR/frontend.log"

mkdir -p "$RUN_DIR"

cmd_start() {
  echo "[dev] Starting middleware..."
  docker compose -f "$COMPOSE_FILE" up -d

  echo "[dev] Waiting for Redis on 31029..."
  until redis-cli -p 31029 ping 2>/dev/null | grep -q PONG; do sleep 1; done
  echo "[dev] Redis ready."

  echo "[dev] Waiting for Milvus on 31953 (up to 60s)..."
  for i in $(seq 1 60); do
    if nc -z 127.0.0.1 31953 2>/dev/null; then echo "[dev] Milvus ready."; break; fi
    sleep 1
    if [ "$i" -eq 60 ]; then echo "[dev] WARNING: Milvus not ready after 60s, continuing anyway."; fi
  done

  # Frontend dependency check
  if [ ! -d "Front_page/node_modules" ]; then
    echo "[dev] node_modules not found, running npm install..."
    if ! (cd Front_page && npm install); then
      echo "[dev] ERROR: npm install failed. Aborting."; exit 1
    fi
  fi

  echo "[dev] Starting backend..."
  nohup go run main.go > "$BACKEND_LOG" 2>&1 &
  echo $! > "$BACKEND_PID_FILE"
  echo "[dev] Backend PID: $(cat $BACKEND_PID_FILE)"

  echo "[dev] Starting frontend..."
  nohup bash -c "cd Front_page && npm run dev" > "$FRONTEND_LOG" 2>&1 &
  echo $! > "$FRONTEND_PID_FILE"
  echo "[dev] Frontend PID: $(cat $FRONTEND_PID_FILE)"

  echo ""
  echo "[dev] All services started."
  echo "  Backend:  http://localhost:6872"
  echo "  Frontend: http://localhost:3000"
  echo "  Logs:     $BACKEND_LOG / $FRONTEND_LOG"
}

cmd_stop() {
  echo "[dev] Stopping backend and frontend..."
  for pidfile in "$BACKEND_PID_FILE" "$FRONTEND_PID_FILE"; do
    if [ -f "$pidfile" ]; then
      pid=$(cat "$pidfile")
      if kill -0 "$pid" 2>/dev/null; then kill "$pid"; fi
      rm -f "$pidfile"
    fi
  done
  # kill go run child processes
  pkill -f "go-build.*go_agent" 2>/dev/null || true
  pkill -f "vite.*3000" 2>/dev/null || true

  echo "[dev] Stopping middleware containers..."
  docker compose -f "$COMPOSE_FILE" down
  echo "[dev] Done. Data volumes preserved."
}

cmd_restart() { cmd_stop; sleep 2; cmd_start; }

cmd_status() {
  echo "=== Backend ==="
  if [ -f "$BACKEND_PID_FILE" ] && kill -0 "$(cat $BACKEND_PID_FILE)" 2>/dev/null; then
    echo "  RUNNING (PID $(cat $BACKEND_PID_FILE))"
  else echo "  STOPPED"; fi

  echo "=== Frontend ==="
  if [ -f "$FRONTEND_PID_FILE" ] && kill -0 "$(cat $FRONTEND_PID_FILE)" 2>/dev/null; then
    echo "  RUNNING (PID $(cat $FRONTEND_PID_FILE))"
  else echo "  STOPPED"; fi

  echo "=== Middleware ==="
  docker compose -f "$COMPOSE_FILE" ps
}

cmd_logs() {
  target="${1:-backend}"
  case "$target" in
    backend)    tail -f "$BACKEND_LOG" ;;
    frontend)   tail -f "$FRONTEND_LOG" ;;
    middleware) docker compose -f "$COMPOSE_FILE" logs -f ;;
    *) echo "Usage: $0 logs [backend|frontend|middleware]"; exit 1 ;;
  esac
}

cmd_clean_volumes() {
  echo "WARNING: This will DELETE all middleware data volumes (Redis, Milvus, etc.)."
  read -rp "Are you sure? Type 'yes' to confirm: " confirm
  if [ "$confirm" != "yes" ]; then echo "Aborted."; exit 0; fi
  docker compose -f "$COMPOSE_FILE" down -v
  echo "[dev] Volumes deleted."
}

case "${1:-}" in
  start)          cmd_start ;;
  stop)           cmd_stop ;;
  restart)        cmd_restart ;;
  status)         cmd_status ;;
  logs)           cmd_logs "${2:-}" ;;
  clean-volumes)  cmd_clean_volumes ;;
  *) echo "Usage: $0 {start|stop|restart|status|logs [backend|frontend|middleware]|clean-volumes}"; exit 1 ;;
esac
```
- **GOTCHA**: `go run main.go` 会在后台编译后再运行子进程，`nohup go run` 记录的是 `go run` 的 PID，不是最终二进制的 PID；`pkill -f "go-build.*go_agent"` 用于补杀编译出的子进程；WSL2 环境下 `nc` 可能不在 PATH，可替换为 `bash -c "echo > /dev/tcp/127.0.0.1/31953"`
- **VALIDATE**: `./scripts/dev.sh start` 后 4 个端口可达；`./scripts/dev.sh stop` 后进程和容器停止

---

## Testing Strategy

### Validation Commands

#### Build 验证（后端）
```bash
go build ./...
```
EXPECT: 无错误

#### 单元测试
```bash
go test ./...
```
EXPECT: 所有测试通过（当前已有测试位于 `utility/mem/`, `utility/common/`, `internal/context/`）

#### 前端类型检查 + 构建
```bash
cd Front_page && npm install && npm run lint && npm run build
```
EXPECT: TypeScript 无错误，构建成功

#### 路由验证
```bash
go run main.go &
sleep 5
# 应该返回 404 或路由不存在
curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:6872/api/v1/ai_ops_stream
# 应该返回 200
curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:6872/api/v1/chat_stream -H "Content-Type: application/json" -d '{"id":"test","question":"hello"}'
```
EXPECT: ai_ops_stream 返回 404/405，chat_stream 返回 200

#### 脚本验证
```bash
./scripts/dev.sh start
# 等待 ~30 秒
redis-cli -p 31029 ping          # PONG
nc -z 127.0.0.1 31953 && echo OK # OK
curl -s http://localhost:6872/api/v1/chat_stream -X POST ...  # SSE 返回
curl -s http://localhost:3000 | head -5  # HTML 返回

./scripts/dev.sh status  # 所有服务显示 RUNNING
./scripts/dev.sh stop
./scripts/dev.sh status  # 所有服务显示 STOPPED
docker volume ls | grep deploy  # volume 仍存在
```

### Edge Cases Checklist
- [ ] 首次启动（无 node_modules）：`start` 自动执行 `npm install`
- [ ] 重复 `start`：docker compose up -d 是幂等的，不会报错
- [ ] Milvus 启动超时（60s 内未就绪）：只打 WARNING，不终止（应用有降级逻辑）
- [ ] `stop` 时进程已停止：`kill -0` 检查后跳过，无报错
- [ ] `clean-volumes` 未确认：Aborted，不执行

---

## Acceptance Criteria
- [ ] `go build ./...` 无错误
- [ ] `go test ./...` 无失败
- [ ] `cd Front_page && npm run lint` 无 TypeScript 错误
- [ ] 前端页面无 "AI Ops 执行中心" 按钮
- [ ] `POST /api/v1/ai_ops_stream` 返回 404/405
- [ ] `POST /api/v1/chat_stream` 正常返回 SSE
- [ ] `./scripts/dev.sh start` 后 4 个端口（31029/31953/6872/3000）可达
- [ ] `./scripts/dev.sh stop` 后进程和容器停止，volume 保留
- [ ] 上传文件后知识库入库链路仍工作

## Completion Checklist
- [ ] 工具注册函数中无 K8s/Prom/Bash/ops_case 引用
- [ ] bootstrap 无 `ops.NewIntegratedOpsExecutor` / `ops.NewIncidentWorkflowAgent` 调用
- [ ] 前端 store 无 `runOps` / `opsSteps` 状态
- [ ] `.run/` 已加入 `.gitignore`
- [ ] `deploy/` 和 `scripts/` 已提交

## Risks
| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| bootstrap 编译失败（ops import 残留） | Medium | High | Task 2 完成后立即 `go build ./internal/bootstrap/...` 验证 |
| 前端 TypeScript 报错（OpsStep 残留引用） | Medium | Medium | Task 7/8/9 后运行 `npm run lint`；OpsPanel.tsx 文件保留但不导入 |
| dev.sh `go run` PID 管理不稳定 | Medium | Low | `pkill -f` 兜底；阶段 2 可换 `go build` + 直接运行二进制 |
| Milvus v2.4.23 镜像在 WSL2 启动慢 | High | Low | 60s 超时只打 WARNING 不终止，应用有降级模式 |
| `npm run lint` 首次失败（缺 node_modules） | Low | Low | dev.sh start 自动 `npm install` |

## Notes
- 运维内部包（`internal/agent/ops/`, `rca/`, `execution/`, `strategy/`）本次**不做物理删除**，只断开 bootstrap 引用路径。阶段 2 执行物理删除 + `go mod tidy`。
- `BashApprovalTool.go` 文件保留，`BashApprovalInterruptInfo` 的 gob.Register 也保留，以免阶段 2 之前编译器报 unused 以外的问题。controller 中 `extractBashToolResultByMessage` 也保留（无编译错误）。
- `OpsPanel.tsx` 文件保留，只删 Header 中的 import 和渲染——保持前端无死代码错误同时简化 diff。
- `deploy/` 目录名优先于 `manifest/docker/`（原来 manifest/docker/ 不存在）；如果团队偏好放 `manifest/docker/`，Task 10 的路径相应调整，同时更新 `scripts/dev.sh` 中的 `COMPOSE_FILE` 变量。
