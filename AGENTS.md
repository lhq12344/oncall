# PROJECT KNOWLEDGE BASE

**Generated:** 2026-03-15 16:42:21 CST
**Commit:** `2b5b547`
**Branch:** `rebuild`

适用范围：仓库根目录及全部子目录（若子目录存在更近的 `AGENTS.md`，以子目录规则优先）。

## OVERVIEW

OnCall 是一个基于 **GoFrame + Eino ADK** 的多 Agent 运维系统：后端负责对话/知识/运维工作流与中断恢复，`Front_page` 负责 SSE 前端交互与人工审批回路。

## STRUCTURE

```text
oncall/
├── main.go                    # 后端唯一进程入口（非 cmd/<app>/main.go）
├── api/chat/                  # GoFrame API 契约与路由元信息（含生成文件）
├── cmd/                        # 当前为空（预留），不要假设为实际入口
├── docs/                       # 文档目录（存在历史漂移，以代码现状为准）
├── examples/                   # 当前为空（示例代码不在此目录维护）
├── internal/
│   ├── bootstrap/             # 应用装配：logger/redis/model/agents
│   ├── controller/chat/       # SSE 接口、interrupt/resume 入口
│   ├── context/               # Session Memory + CheckPointStore
│   ├── agent/                 # dialogue/ops/rca/execution/strategy/knowledge
│   └── ai/                    # 模型/嵌入/检索/索引基础设施
├── utility/                   # Redis/MySQL/ES/middleware/tokenizer 等共享基础组件
├── Front_page/                # React + Vite 前端（SSE 消费与审批交互）
├── logs/                      # 运行期报告与日志产物
├── manifest/                  # 运行配置与 K8s 运维脚本
├── test/                      # 测试目录（当前基本为空）
└── hack/                      # gf 相关构建与代码生成入口
```

## WHERE TO LOOK

| 任务 | 位置 | 说明 |
|---|---|---|
| 服务启动失败 | `main.go`, `internal/bootstrap/app.go` | 依赖初始化与路由绑定都在这里 |
| 流式对话/恢复中断 | `internal/controller/chat/chat_v1.go` | `ChatStream`/`ChatResumeStream`/SSE 输出 |
| API 路由与字段契约 | `api/chat/v1/chat.go` | `g.Meta` 声明的 path/method 与请求结构 |
| 工作流编排（RCA→Ops→Execution→Strategy） | `internal/agent/ops/incident_workflow.go` | 顺序+循环工作流骨架 |
| 高风险命令审批与执行 | `internal/agent/execution/**`, `internal/agent/dialogue/tools/BashApprovalTool.go` | 计划校验、逐步执行、审批中断 |
| 会话记忆与 token 预算 | `utility/mem/mem.go`, `internal/context/session_memory.go` | 历史裁剪与上下文成本控制 |
| 前端 SSE 解析/中断卡片 | `Front_page/src/services/api.ts`, `Front_page/src/components/InterruptCard.tsx` | `content/step/interrupt/error/done` 协议消费 |
| 运行配置与基础设施 | `manifest/config/config.yaml`, `manifest/k8s/deploy.sh` | 配置项与 K8s 一键脚本 |

## CODE MAP

> 当前环境未安装 `gopls`（LSP 不可用），以下映射来自代码静态检索（grep/AST）。

| Symbol | Type | Location | Role |
|---|---|---|---|
| `main` | func | `main.go` | 进程入口、依赖初始化、HTTP 启动 |
| `NewApplication` | func | `internal/bootstrap/app.go` | 装配全局依赖与核心 Agents |
| `NewV1` | func | `internal/controller/chat/chat_v1.go` | 控制器初始化与 Runner 绑定 |
| `ChatStream` | method | `internal/controller/chat/chat_v1.go` | 对话 SSE 主链路 |
| `ChatResumeStream` | method | `internal/controller/chat/chat_v1.go` | 中断恢复 SSE 链路 |
| `AIOpsStream` | method | `internal/controller/chat/chat_v1.go` | AIOps 流式链路 |
| `NewIncidentWorkflowAgent` | func | `internal/agent/ops/incident_workflow.go` | 故障处置统一工作流 |
| `NewExecutionAgent` | func | `internal/agent/execution/agent.go` | 命令级计划、执行、回滚代理 |

## CONVENTIONS

- 统一中文回复；优先最小改动，不顺手重构。
- 本仓入口固定为 `main.go`；`cmd/` 目前为空，不要按传统多二进制布局假设修改。
- `api/chat/chat.go` 为 GoFrame 生成文件（`DO NOT EDIT`），接口契约变更应先改 `api/chat/v1/*` 再走生成流程。
- 中断恢复改动必须成对检查：`interrupt` 触发路径 + `resume` 恢复路径。
- Agent 分层边界必须保持：对话（dialogue）/知识（knowledge）/运维（ops）职责不混用。
- 上下文记忆优先控制 token 成本：避免把大日志、大对象直接回灌模型历史。

## ANTI-PATTERNS (THIS PROJECT)

- 禁止修改与当前任务无关的文件、接口或配置。
- 禁止跳过高风险操作的人审中断机制（approval/interrupt）。
- 禁止在 controller 中写复杂业务逻辑（控制器只做参数、编排、SSE、落库）。
- 禁止破坏 SSE 事件语义：`content` / `step` / `interrupt` / `error` / `done`。
- 禁止仅口头宣称“已修复”；必须以工具执行结果与校验结果为准。

## UNIQUE STYLES

- 工作流以 Eino ADK 的可恢复 Runner 为中心：`WithCheckPointID` + `ResumeWithParams`。
- `utility/mem` 做请求级历史裁剪与 token 预算，默认限制大推理内容回灌。
- 前端以单点 `services/api.ts` 管理 SSE 协议解析，支持 JSON 事件与 `[DONE]/[ERROR]` 回退。

## COMMANDS

```bash
# 后端运行
go run main.go

# GoFrame 构建/生成（需同时加载 hack-cli 目标）
make -f hack/hack.mk -f hack/hack-cli.mk build
make -f hack/hack.mk -f hack/hack-cli.mk ctrl
make -f hack/hack.mk -f hack/hack-cli.mk dao
make -f hack/hack.mk -f hack/hack-cli.mk service

# 测试（Go 变更后至少执行一次）
go test ./...

# 前端
cd Front_page && npm install
cd Front_page && npm run dev
cd Front_page && npm run build
cd Front_page && npm run lint

# K8s 基础设施
cd manifest/k8s && ./deploy.sh status
```

## AGENTS HIERARCHY

```text
./AGENTS.md
├── ./internal/AGENTS.md
│   ├── ./internal/agent/AGENTS.md
│   └── ./internal/controller/AGENTS.md
├── ./Front_page/AGENTS.md
└── ./utility/AGENTS.md
```

## NOTES

- `manifest/config/config.yaml` 含敏感信息（API key/DSN 等），严禁在日志、PR 描述、示例代码中明文扩散。
- 文档存在历史漂移：部分文档提及的 `manifest/docker`、`Front_page/start.sh` 在当前仓库中不存在；以现有目录与脚本为准。
