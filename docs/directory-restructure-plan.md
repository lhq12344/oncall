# OnCall 前后端分离与目录重组计划

> 状态：Phase 1、Phase 2A-2D 已完成；剩余内部整理只保留低风险批次继续推进。
> 日期：2026-08-18

## 目标

当前仓库重组的第一目标是把前端、后端、文档和仓库级配置分开；第二目标是让后端目录按职责命名，减少所有能力都塞在 `internal/agent/` 下造成的交织。

- `backend/`：GoFrame + Eino ADK 后端服务、API 契约、工作流、执行、知识、工具网关、部署配置、测试数据。
- `frontend/`：React + Vite + TypeScript 前端应用。
- `docs/`：项目文档、架构记录和迁移计划。
- 根目录：仓库级说明、忽略规则、`go.work` 工作区文件和本地环境文件。

## 当前顶层结构

```text
oncall/
  backend/        # Go 后端模块，保留原 go module：go_agent
  frontend/       # 原 Front_page，前端独立 npm/Vite 项目
  docs/           # 文档与迁移计划
  go.work         # 根目录 workspace，指向 ./backend
  .env            # 本地密钥/环境，暂留根目录，后端启动会兼容读取
```

## Phase 1：前后端顶层物理分离

状态：已完成。

### 移动

- `Front_page/` -> `frontend/`
- `main.go`, `go.mod`, `go.sum` -> `backend/`
- `api/`, `cmd/`, `internal/`, `utility/`, `manifest/`, `hack/`, `test/`, `testdata/`, `examples/`, `logs/` -> `backend/`

### 保留在根目录

- `docs/`
- `.gitignore`
- `AGENTS.md`
- `CLAUDE.md`
- `.env`：暂时保留在根目录，`backend/main.go` 支持从 `backend/.env` 或根目录 `../.env` 读取，避免移动本地密钥文件。

### 验收

- 后端：`cd backend && go test ./...`
- 根目录 workspace：`go test ./backend/...`
- 前端：`cd frontend && cmd /c npm run lint && cmd /c npm run build`
- 目录：确认 `backend/` 与 `frontend/` 存在，旧 `Front_page/` 不存在。

## Phase 2：后端内部边界整理

目标结构：

```text
backend/
  api/                    # HTTP API 契约，短期保留
  cmd/                    # CLI/辅助命令，例如 ragctl
  internal/
    bootstrap/            # 应用装配
    controller/           # HTTP/SSE transport
    workflow/
      ops/                # 故障处置主工作流
      dialogue/           # 普通对话/聊天工作流
      agentteams/         # 多 agent team/workflow builder
    execution/            # 执行计划、验证、回滚
    knowledge/            # 知识上传、检索编排、案例归档
    rag/                  # Hybrid RAG / BM25 / rerank / rewrite 检索引擎
    toolkit/              # 工具网关、权限包装、文件读写工具
    slash/                # slash command 解析、注册与内置命令
    agent/
      rca/                # legacy/reserved RCA agent；ops 当前只复用部分 tools
      strategy/           # legacy/reserved strategy agent
    ai/                   # 旧 AI indexer/retriever/embedder 适配，待后续归并
    context/              # 会话、checkpoint、memory
    compact/              # 上下文压缩
    hooks/                # hook engine
    permissions/          # 权限规则与 sandbox 检查
    prompt/               # prompt 构建
    toolresult/           # tool result budget/spill
  utility/                # Redis/MySQL/ES/Milvus/tokenizer 等基础设施适配，待后续 platform 化
```

内部重组必须保持以下不变量：

1. `ExecutionPlan` 的 canonical 来源仍是 Graph State 中的 `PlanState`。
2. `execute_plan` 只能消费已通过 `plan_gate` 和 `plan_approval` 的计划。
3. `replan_decider` 仍是 `complete` / `refresh_observation` / `manual_required` 的唯一收敛出口。
4. `rca` / `strategy` 先保留为 legacy/reserved，确认主链依赖后再删除、迁移或工具化。
5. 每批只移动职责明确的边界，并在同一批修 imports、文档和测试。

## Phase 2A：Ops 主工作流包迁移

状态：已完成。

- `backend/internal/agent/ops` -> `backend/internal/workflow/ops`
- `go_agent/internal/agent/ops` -> `go_agent/internal/workflow/ops`
- `go_agent/internal/agent/ops/tools` -> `go_agent/internal/workflow/ops/tools`

目的：把故障处置主链从泛化 agent 目录移到 workflow 边界下，明确「编排工作流」职责。

## Phase 2B：Execution 执行边界迁移

状态：已完成。

- `backend/internal/agent/execution` -> `backend/internal/execution`
- `go_agent/internal/agent/execution` -> `go_agent/internal/execution`
- `go_agent/internal/agent/execution/tools` -> `go_agent/internal/execution/tools`

目的：把 plan execution / validate / rollback 从泛化 agent 目录移到执行边界下，保持 ops workflow 只编排，execution 只消费已审批计划。

## Phase 2C：Knowledge 与 Toolkit 边界迁移

状态：已完成。

- `backend/internal/agent/knowledge` -> `backend/internal/knowledge`
- `go_agent/internal/agent/knowledge` -> `go_agent/internal/knowledge`
- `backend/internal/agent/toolkit` -> `backend/internal/toolkit`
- `go_agent/internal/agent/toolkit` -> `go_agent/internal/toolkit`

目的：

- `knowledge` 是知识上传、索引、检索编排模块，不再归类为通用 agent。
- `toolkit` 是共享工具网关/权限层，被 dialogue、ops、execution、rca、strategy 复用，不应放在 agent 子树下。
- `internal/rag` 暂时保留独立：它是底层检索引擎，`knowledge` 与 dialogue/ops 都会调用，不强行塞入 `knowledge/rag`，避免形成反向耦合。

## Phase 2D：Dialogue、Agentteams、Slash 边界迁移

状态：已完成。

- `backend/internal/agent/dialogue` -> `backend/internal/workflow/dialogue`
- `go_agent/internal/agent/dialogue` -> `go_agent/internal/workflow/dialogue`
- `backend/internal/agent/agentteams` -> `backend/internal/workflow/agentteams`
- `go_agent/internal/agent/agentteams` -> `go_agent/internal/workflow/agentteams`
- `backend/internal/agent/slash` -> `backend/internal/slash`
- `go_agent/internal/agent/slash` -> `go_agent/internal/slash`

目的：

- dialogue 是用户聊天链路，归入 `workflow/dialogue`。
- agentteams 是工作流编排 builder，归入 `workflow/agentteams`。
- slash 是命令解析/注册系统，归入 `internal/slash`，供 controller 使用。

## 验收记录

2026-08-18 已通过：

- `cd backend && go test ./...`
- 根目录：`go test ./backend/...`
- `cd frontend && cmd /c npm run lint`
- `cd frontend && cmd /c npm run build`

说明：Windows 默认 Go build cache 出现过 `Access is denied`，验证时使用仓库本地 `.gocache` 作为 `GOCACHE`。前端 build 仅保留 Vite chunk-size / mixed static+dynamic import 警告，不影响构建成功。

## 下一步建议

1. 判断 `internal/agent/rca` 与 `internal/agent/strategy` 是否仍需作为 agent 存在；如只是工具集合，迁入 `internal/rca` / `internal/strategy` 或 ops 专用 diagnostics。
2. 把 `utility/` 和 `internal/ai/` 统一评估为 `internal/platform/` / `internal/infra/`，但需要先梳理 Redis、MySQL、ES、Milvus、tokenizer 的调用方向。
3. 若继续做 domain 分层，再抽 `domain/incident` 前必须先锁定 Graph State / PlanState / ReplanState 测试，避免破坏 ops 主链不变量。

## 回滚策略

- Phase 1：将 `frontend/` 反向移动为 `Front_page/`，将 `backend/` 下原后端目录反向移动回根目录，并还原 `go.work` / `.gitignore` / 文档路径。
- Phase 2：每批都是纯目录移动 + import 路径替换；可按对应 Phase 的移动清单反向移动并反向替换 import。
- 密钥文件不参与移动；根目录 `.env` 未纳入迁移。