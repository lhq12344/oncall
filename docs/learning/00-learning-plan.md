# OnCall 项目重新学习计划

> 版本：v0.1  
> 日期：2026-08-18  
> 目标：从“知道怎么跑起来”逐步过渡到“能解释核心链路、定位问题、修改关键模块并验证”。  
> 当前仓库结构基线：根目录拆成 `backend/` Go 后端、`frontend/` React/Vite 前端、`docs/` 学习与设计文档。

## 0. 对你原计划的评价

你的计划方向是对的：从架构全景、领域模型、启动入口、源码路线、依赖生态、构建测试、踩坑笔记逐步深入，非常适合作为长期学习笔记目录。

我建议做三点调整：

1. **先跑通，再全景**：先确认项目能构建、测试、启动，再画架构图，避免文档和真实代码脱节。
2. **前后端分开学，再按链路合并**：现在已经是 `backend/` 和 `frontend/` 两个边界，学习时也应分别建立地图，最后用一次请求链路串起来。
3. **围绕 OnCall 主链路学习**：本项目最核心不是普通 CRUD，而是 AI 运维工作流、SSE、审批/恢复、执行计划、知识检索和工具调用。

## 1. 学习笔记目录建议

建议后续在 `docs/learning/` 下按以下结构沉淀：

```text
docs/learning/
  00-learning-plan.md              # 本文件：学习计划与路线
  01-architecture-overview.md      # 架构全景图与模块职责
  02-bootstrap-and-request-flow.md # 启动流程与第一次请求追踪
  03-domain-model-glossary.md      # 核心术语、状态、数据结构
  04-ops-workflow.md               # AIOps / incident workflow 核心链路
  05-execution-plan-tools.md       # ExecutionPlan、审批、执行与回滚工具
  06-knowledge-rag.md              # Knowledge / RAG / 检索链路
  07-frontend-sse-ui.md            # 前端 SSE、审批交互、状态管理
  08-build-test-debug.md           # 构建、测试、本地调试
  09-pitfalls-and-open-questions.md# 踩坑、不解之处、后续问题
```

每篇笔记固定包含：

- **我想搞懂什么**
- **入口文件**
- **关键类型/函数**
- **调用链**
- **我画出的结构图或时序图**
- **已验证命令**
- **不懂的问题**

## 2. 阶段一：跑通项目与建立目录地图

**目标**：确认项目当前状态，建立“文件夹职责地图”。

**必读路径**：

- `go.work`
- `backend/go.mod`
- `backend/main.go`
- `backend/internal/bootstrap/app.go`
- `frontend/package.json`
- `frontend/src/services/api.ts`

**要产出的笔记**：

- 后端目录职责表：`api/`、`internal/bootstrap/`、`internal/controller/`、`internal/workflow/`、`internal/execution/`、`internal/knowledge/`、`internal/toolkit/`、`utility/`
- 前端目录职责表：`src/`、`services/`、`store/`、组件目录
- 根目录职责表：`backend/`、`frontend/`、`docs/`、`manifest/`、`.omx/`

**验证命令**：

```powershell
cd D:\Code\project\oncall

$env:GOCACHE = (Resolve-Path ".gocache").Path
go test ./backend/...

cd frontend
cmd /c npm run lint
cmd /c npm run build
```

## 3. 阶段二：架构全景图

**目标**：建立上帝视角，知道请求、Agent、工具、存储、前端 UI 如何协作。

**建议架构分层**：

1. **Client 层**：`frontend/`，React + Vite，负责聊天界面、SSE 消费、人工审批/恢复交互。
2. **API/Gateway 层**：`backend/api/chat/v1/` 与 `backend/internal/controller/chat/`，负责 HTTP/SSE 接口与请求分发。
3. **Application Bootstrap 层**：`backend/internal/bootstrap/`，集中初始化 DialogueAgent、OpsAgent、KnowledgeAgent、HookEngine、Redis/MySQL/ES 等依赖。
4. **Workflow 层**：`backend/internal/workflow/`，承载对话与 AIOps 主工作流。
5. **Execution/Tools 层**：`backend/internal/execution/`、`backend/internal/toolkit/`，负责计划生成、校验、命令执行、工具注册与调用。
6. **Knowledge/RAG 层**：`backend/internal/knowledge/`、`backend/internal/rag/`、`backend/internal/ai/`，负责知识检索、索引、向量/LLM 相关能力。
7. **Infrastructure 层**：`backend/utility/`，负责 Redis、MySQL、Elasticsearch、配置、中间件等基础设施适配。

**要画的图**：

```text
Frontend UI
   |
   | HTTP/SSE
   v
backend/api/chat/v1
   |
   v
internal/controller/chat
   |
   v
internal/bootstrap.Application
   |
   +--> workflow/dialogue
   +--> workflow/ops
   +--> knowledge / rag
   +--> execution / toolkit
   |
   v
utility: Redis / MySQL / ES / Config
```

## 4. 阶段三：入口与启动流程

**目标**：找到第一条执行线，知道服务如何启动、路由如何注册、依赖如何注入。

**阅读顺序**：

1. `backend/main.go`
2. `backend/internal/bootstrap/app.go`
3. `backend/api/chat/v1/chat.go`
4. `backend/internal/controller/chat/chat_v1.go`

**需要拆解的问题**：

- 配置从哪里读取？
- Redis、MySQL、ES 如何初始化？
- `bootstrap.NewApplication` 创建了哪些 Agent？
- `chat.NewV1WithHooks` 绑定了哪些依赖？
- `/api/v1/ai_ops_stream` 和 `/api/v1/ai_ops_resume_stream` 分别走哪条链路？
- 没有 Redis 时 checkpoint 是否退化到内存实现？

**第一次请求追踪产出**：

从前端一次 AIOps 请求开始记录：

```text
frontend/src/services/api.ts
  -> POST /api/v1/ai_ops_stream
  -> backend/api/chat/v1/chat.go
  -> backend/internal/controller/chat/chat_v1.go
  -> opsStreamRunner
  -> workflow/ops
  -> execution / knowledge / toolkit
  -> SSE response / interrupt / resume
```

## 5. 阶段四：核心概念与领域模型

**目标**：搞懂项目“在说什么”，先把术语统一起来。

**第一批术语表**：

- **AIOps**：AI 运维诊断、计划、执行、验证的主链路。
- **DialogueAgent**：普通对话 Agent。
- **OpsAgent / IncidentWorkflowAgent**：故障诊断与处置工作流入口。
- **KnowledgeAgent**：知识检索与上下文增强入口。
- **ExecutionPlan**：命令级执行计划，是后续审批、执行、验证的核心对象。
- **PlanState**：Graph State 中保存 canonical ExecutionPlan 的状态。
- **ReplanState**：验证失败或需要补充观察时的重规划状态。
- **checkpoint_id**：SSE 中断/恢复、会话延续的关键标识。
- **interrupt/resume**：人工审批、工具确认或恢复执行的机制。
- **tool**：Agent 可调用的外部动作，如生成计划、校验计划、执行步骤、检索知识。

**重点数据结构**：

- `backend/api/chat/v1/chat.go`：请求/响应结构。
- `backend/internal/workflow/ops/incident_contract.go`：Ops 工作流契约。
- `backend/internal/workflow/ops/diagnosis_gate.go`：诊断门控与状态转换。
- `backend/internal/execution/tools/generate_plan.go`：`ExecutionPlan`、`ExecutionStep`。
- `backend/internal/execution/tools/validate_plan.go`：计划校验结果。
- `backend/internal/execution/tools/tool_call_state.go`：执行工具状态缓存。

## 6. 阶段五：AIOps 核心工作流

**目标**：理解项目最核心的业务链路。

**重点路径**：

- `backend/internal/workflow/ops/`
- `backend/internal/execution/`
- `backend/internal/execution/tools/`
- `backend/internal/prompt/role_prompts.go`
- `backend/internal/prompt/sections.go`

**主链路应重点验证**：

```text
incident_analysis
  -> diagnosis_gate
  -> plan
  -> plan_gate
  -> plan_approval
  -> execute_plan
  -> verify_plan
  -> replan_decider
  -> final_report
```

**必须搞懂的几个边界**：

- 谁负责生成 `ExecutionPlan`？
- 谁负责把 `ExecutionPlan` 写入 Graph State？
- 谁有权修改计划？
- 什么情况下进入人工审批？
- `execute_plan` 为什么只能消费已批准计划？
- `replan_decider` 如何决定 complete / refresh_observation / manual_required？

**产出物**：

- 一张状态机图。
- 一条完整成功链路。
- 一条需要人工审批的链路。
- 一条验证失败后重规划的链路。

## 7. 阶段六：知识检索、RAG 与工具系统

**目标**：理解 Agent 如何拿到外部知识和执行能力。

**阅读顺序**：

1. `backend/internal/knowledge/`
2. `backend/internal/rag/`
3. `backend/internal/ai/`
4. `backend/internal/toolkit/`
5. `backend/internal/execution/tools/`

**要回答的问题**：

- 用户问题如何转成检索请求？
- 知识库索引在哪里构建？
- Milvus / ES / MySQL / Redis 分别承担什么角色？
- 工具是在哪里注册的？
- Agent 如何决定调用哪个工具？
- 工具调用失败后如何记录和恢复？

## 8. 阶段七：前端 SSE 与审批交互

**目标**：理解前端如何消费后端流式响应，并支持中断恢复。

**必读路径**：

- `frontend/src/services/api.ts`
- `frontend/src/store/`
- `frontend/src/components/`
- `frontend/src/App.tsx`
- `frontend/src/main.tsx`

**要回答的问题**：

- 前端如何配置后端 API 地址？
- `ai_ops_stream` 流式响应如何解析？
- interrupt 卡片在哪里渲染？
- 用户点击 approve / reject / resume 后如何回到后端？
- 前端状态和 checkpoint_id 如何关联？

## 9. 阶段八：构建、测试与本地调试

**目标**：形成“看代码 -> 改代码 -> 验证”的闭环。

**后端常用命令**：

```powershell
cd D:\Code\project\oncall
$env:GOCACHE = (Resolve-Path ".gocache").Path
go test ./backend/...

cd backend
$env:GOCACHE = (Resolve-Path "..\.gocache").Path
go test ./...
go run main.go
```

**前端常用命令**：

```powershell
cd D:\Code\project\oncall\frontend
cmd /c npm run lint
cmd /c npm run build
cmd /c npm run dev
```

**重点测试入口**：

- `backend/internal/controller/chat/*_test.go`
- `backend/internal/workflow/ops/*_test.go`
- `backend/internal/execution/tools/*_test.go`
- `backend/internal/prompt/*_test.go`
- `frontend` 的 lint/build 结果

## 10. 阶段九：源码阅读路线图

### 第一层：简单但必须读

- `backend/internal/consts/`
- `backend/internal/context/`
- `backend/internal/toolresult/`
- `backend/internal/permissions/`
- `backend/utility/config/`
- `frontend/src/services/api.ts`

目标：知道项目的基础类型、上下文、错误、权限、配置和 API 调用方式。

### 第二层：核心接口与抽象

- `backend/internal/bootstrap/app.go`
- `backend/internal/controller/chat/chat_v1.go`
- `backend/internal/toolkit/`
- `backend/internal/hooks/`
- `backend/internal/knowledge/`

目标：知道依赖如何组合，Agent 如何挂载到 API 上。

### 第三层：业务精髓

- `backend/internal/workflow/ops/`
- `backend/internal/execution/tools/`
- `backend/internal/prompt/role_prompts.go`
- `backend/internal/prompt/sections.go`

目标：理解 AIOps 的计划、审批、执行、验证、重规划。

### 第四层：高级主题

- SSE 中断/恢复协议。
- Graph State / checkpoint 状态恢复。
- 工具调用状态缓存。
- Redis / MySQL / ES / Milvus 的真实运行依赖。
- 前端与后端字段契约变更如何同步。

## 11. 阶段十：踩坑与开放问题

先预留以下坑位，后续边学边补：

| 问题 | 现象 | 初步解决方案 | 是否已验证 |
| --- | --- | --- | --- |
| PowerShell 直接执行 npm 可能被策略拦截 | `npm.ps1` 执行失败 | 使用 `cmd /c npm run ...`，详见 `10-build-test-local-debug.md` | 已补充 |
| Go 默认缓存目录可能 Access denied | `go test` 写缓存失败 | 设置 repo-local `GOCACHE=.gocache`，详见 `10-build-test-local-debug.md` | 已补充 |
| 前端旧路径命名残留 | 文档或 AGENTS 可能仍写 `Front_page` | 以当前 `frontend/` 为准，详见 `01-architecture-overview.md` 和 `11-source-roadmap-pitfalls.md` | 已补充 |
| `.env / dist / node_modules` 不应提交 | 敏感配置或构建产物混入 Git | 提交前检查 `git status --short` 与 staged diff，详见 `11-source-roadmap-pitfalls.md` | 已补充 |

## 12. 推荐学习节奏

### Day 1：能跑起来

- 跑后端测试。
- 跑前端 lint/build。
- 读 `main.go` 和 `bootstrap/app.go`。
- 输出 `02-bootstrap-and-request-flow.md` 初稿。

### Day 2：画架构图

- 按前端、API、Controller、Workflow、Execution、Knowledge、Infra 分层。
- 输出 `01-architecture-overview.md` 初稿。

### Day 3-4：读 AIOps 主链路

- 精读 `workflow/ops`。
- 跟踪 `ai_ops_stream` 到 `final_report`。
- 输出 `04-ops-workflow.md`。

### Day 5：读 ExecutionPlan 与工具

- 精读 `execution/tools`。
- 搞懂生成、规范化、校验、执行、验证、回滚。
- 输出 `05-execution-plan-tools.md`。

### Day 6：读知识检索与 RAG

- 看 `knowledge`、`rag`、`ai`、`toolkit`。
- 输出 `06-knowledge-rag.md`。

### Day 7：读前端交互

- 看 SSE、审批卡片、状态管理。
- 输出 `07-frontend-sse-ui.md`。

### Day 8：回头整理术语和坑

- 补 `03-domain-model-glossary.md`。
- 补 `09-pitfalls-and-open-questions.md`。
- 列出下一轮要深挖的问题。

## 13. 第一轮学习的完成标准

第一轮不要求看懂所有代码，只要求你能做到：

- 能解释根目录下 `backend/`、`frontend/`、`docs/` 的职责。
- 能从 `backend/main.go` 讲清服务启动流程。
- 能从 `frontend/src/services/api.ts` 追踪一次 `ai_ops_stream` 请求到后端 Controller。
- 能画出 AIOps 主链路：诊断、计划、审批、执行、验证、重规划、最终报告。
- 能说清 `ExecutionPlan`、`PlanState`、`ReplanState`、`checkpoint_id` 的作用。
- 能跑通后端测试、前端 lint 和 build。
- 能记录至少 5 个“我还不懂的问题”，作为第二轮学习入口。


