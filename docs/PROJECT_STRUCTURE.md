# OnCall 主文档（当前实现一致版）

> 更新时间：2026-03-22
> 校准范围：`main.go`、`api/chat/v1/chat.go`、`internal/controller/chat/chat_v1.go`、`internal/agent/*`、`internal/context/*`、`utility/mem/*`

## 1. 文档定位

- 这是 `oncall/docs` 下的主文档（source of truth）。
- `面试亮点.md`、`面试应答指南.md`、`项目介绍.md`、`interview-analysis.md` 作为补充材料保留。
- 如补充文档与实现冲突，以本文为准。

## 2. 一句话介绍

OnCall 是一个基于 GoFrame + Eino ADK 的多 Agent 运维系统：用自然语言发起故障处理，完成观测、RCA、修复执行（含审批中断/恢复）、报告产出和知识沉淀。

## 3. 当前架构（双轨）

```text
Frontend (React + Vite + TypeScript + Zustand)
        │
        ▼
HTTP/SSE API (/api/v1/*, port 6872)
        │
        ▼
Controller(chat_v1) + ADK Runner
   ├─ 轨道 A: dialogue_agent（聊天/工具编排）
   └─ 轨道 B: incident_workflow_agent（故障处置工作流）
```

### 3.1 轨道 A：对话轨

- 入口：`POST /api/v1/chat_stream`
- Runner：`chatStreamRunner`
- 主要能力：意图识别、知识检索、K8s/指标查询、受控 Bash 执行、外部检索
- 中断恢复：`POST /api/v1/chat_resume_stream`

### 3.2 轨道 B：运维工作流轨

- 入口：`POST /api/v1/ai_ops_stream`
- Runner：`opsStreamRunner`
- 工作流结构（源码真实）：

```text
Sequential(
  Loop(
    incident_analysis,
    diagnosis_gate,
    plan,
    plan_gate,
    plan_approval,
    execute_plan,
    verify_plan,
    replan_decider
  ),
  final_report
)
```

- Loop 最大轮次：`MaxExecutionLoops`，默认 `3`
- 恢复接口：`POST /api/v1/ai_ops_resume_stream`

## 4. 对外接口与流式协议

### 4.1 HTTP 接口（当前实现）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/chat_stream` | 普通对话流式输出 |
| POST | `/api/v1/chat_resume_stream` | 对话中断恢复 |
| POST | `/api/v1/ai_ops_stream` | 运维工作流流式输出 |
| POST | `/api/v1/ai_ops_resume_stream` | 运维中断恢复 |
| POST | `/api/v1/upload` | 知识文件上传（`multipart/form-data`） |
| GET | `/api/v1/monitoring` | 监控占位接口（当前返回默认值） |

### 4.2 SSE 事件语义

- `chat_stream`：
  - 普通内容：直接文本 chunk（`data: <text>`）
  - 中断：JSON（`type=interrupt`）
  - 结束：`[DONE]`
  - 错误：`[ERROR] ...`
- `ai_ops_stream` / `ai_ops_resume_stream`：
  - 步骤：`{"type":"step","step":n,"content":"..."}`
  - 内容：`{"type":"content","content":"..."}`
  - 中断：`{"type":"interrupt", ...}`
  - 结束：`{"type":"done"}`
  - 错误：`{"type":"error","content":"..."}`

### 4.3 中断恢复请求关键字段

- `checkpoint_id`：定位一次可恢复执行实例
- `interrupt_ids[]`：定位具体待恢复中断点（可空，空则 checkpoint 级恢复）
- `approved/resolved/comment/selection_value`：恢复决策载荷

## 5. Agent 分层职责

| Agent | 职责 | 核心工具/输出 |
|---|---|---|
| `dialogue_agent` | 对话入口、工具编排 | `intent_analysis`、`request_detail_selection`、`knowledge_retrieve`、`ops_case_retrieve`、`k8s_monitor`、`metrics_collector`、`web_search`、`bash_execute_with_approval` |
| `knowledge_agent` | 文本知识上传、分片索引 | 上传链路 `BuildKnowledgeUploadChain` |
| `incident_analysis` | 观测、RCA 与修复意图分析 | 输出 Diagnosis / RemediationProposal，写入 `IncidentState` |
| `diagnosis_gate` | 诊断证据与修复意图门控 | 校验证据、根因、影响面、fallback 等进入计划前条件 |
| `plan` | 生成 canonical ExecutionPlan | `normalize_plan -> generate_plan`，写入 `PlanState` |
| `plan_gate` | 校验 canonical ExecutionPlan | `validate_plan`，检查风险、回滚、成功标准与审批边界 |
| `plan_approval` | 整体计划审批绑定 | 绑定 `plan_id + plan_revision + approval_snapshot_hash` |
| `execute_plan` | 仅执行已批准计划 | `execute_step -> validate_result -> rollback`；不生成或改写计划 |
| `verify_plan` | 全计划执行结果验证 | 校验 executed_steps 覆盖率和 canonical success criteria |
| `replan_decider` | 重规划决策与循环收敛 | 输出 complete / refresh_observation / manual_required / abort |
| `final_report` | 最终总结输出与报告落盘 | 汇总 `IncidentState`、PlanState、ReplanState 生成最终报告 |

## 6. 状态模型与恢复机制

### 6.1 Session Memory（对话记忆）

- 实现：
  - 上层：`internal/context/session_memory.go`
  - 底层：`utility/mem/mem.go`
- 存储：Redis（turns + summary + meta + sys）
- 目标：控制上下文 token，避免长日志污染输入
- 压缩策略（当前实现）：
  - 触发阈值：历史轮次 > 40
  - 每次压缩：最旧 20 轮
  - 摘要方式：规则拼接（`- 用户: ... / - 助手: ...`），不是 LLM 摘要
  - 摘要长度：默认 1200 runes
- 补充说明：`dialogue_agent` 还挂了 Eino summarization middleware，但故障工作流主链的上下文控制核心仍然是 `Graph State + HistoryRewriter`

### 6.2 Checkpoint Store（执行检查点）

- 实现：`internal/context/checkpoint_store.go`
- Redis Key：`oncall:checkpoint:<checkpoint_id>`
- Value：ADK checkpoint bytes
- TTL：默认 24h
- 用途：`Runner.Resume/ResumeWithParams` 恢复执行图

### 6.3 Graph State（工作流状态）

- 类型：`IncidentState`（`internal/agent/ops/state_bridge.go`）
- 通过 session values 维护，字段覆盖 observation/rca/proposal/execution/final status
- `incidentHistoryRewriter` 在每轮模型输入前只注入：
  - 最新用户输入
  - 裁剪后的 Graph State（JSON）

### 6.4 Execution Tool State（执行内状态）

- Key：`_execution_tool_state_v1`
- 结构：`executionToolState`（gob 编解码）
- 作用：记录计划准备、步骤执行、验证状态、重复失败计数，保证 checkpoint 恢复后执行状态连续

## 7. 执行安全机制

### 7.1 静态计划风险校验（`validate_plan`）

- 绝对禁止（blocked）：`rm -rf /`、`mkfs`、`dd if=`、`shutdown/reboot`、fork bomb
- 高风险（审阅/确认）：`kubectl delete/drain/scale/patch/...`、`docker stop/restart/rm`、`systemctl stop/restart/disable`、`helm upgrade/rollback/uninstall`
- 只读命令识别：`kubectl get/describe/logs/top`、`cat/ls/ps/...`

### 7.2 运行时审批与执行（`execute_step`）

- 白名单命令集（bash/kubectl/docker/systemctl/curl 等）
- 变更类步骤执行前触发 `tool.Interrupt(...)`
- 恢复时通过 `tool.GetResumeContext(...)` 读取 `approved/resolved/comment` 决策
- 命令执行有 timeout，输出有裁剪，防止长日志失控

### 7.3 循环收敛与熔断

- 外层：`verify_plan` 与 `replan_decider` 按执行/验证事实决定完成、重新观测、转人工或终止
- 重复问题上限：默认 `3` 次同类失败后停止自动重试并转人工
- 内层：execution tool state 对同一步骤重复失败有额外阈值保护

## 8. 前端实现口径（当前）

- 技术栈：React 19 + TypeScript + Vite 6 + Zustand
- 关键文件：
  - `Front_page/src/services/api.ts`（SSE 客户端与事件解析）
  - `Front_page/src/store/useStore.ts`（全局状态与持久化）
  - `Front_page/src/components/InterruptCard.tsx`（中断审批 UI）
- 前端对 SSE 采用“JSON 优先 + 文本回退”解析策略

## 9. 运行与依赖

- 进程入口：`main.go`
- 监听端口：`6872`（`main.go` 显式 `SetPort(6872)`）
- 关键依赖：
  - Redis：会话记忆 + checkpoint
  - MySQL：业务持久化初始化
  - Elasticsearch：可选，失败时降级
  - Prometheus/K8s：运维工具查询
  - Milvus：知识检索/案例检索
- K8s 清单说明：本仓库 `manifest/k8s/README.md` 已声明统一清单位于 `/home/lihaoqian/project/k8s`

## 10. 已校准口径（避免旧文档冲突）

1. 前端不是“Vanilla JS 页面”，是 React + TS + Zustand。
2. 当前 API 不是 `/api/v1/chat` / `/api/v1/chat_resume`，而是 `*_stream` 路径。
3. 会话压缩摘要是规则拼接，不是 LLM 摘要生成。
4. Incident Loop 不是无限循环，默认最多 3 轮。
5. Checkpoint 是 Redis bytes 存储实现，恢复由 ADK Runner 驱动。
6. SSE 协议是“文本+JSON 混合”，不是单一 JSON 流。
7. `/api/v1/upload` 是知识上传链路，不是对象存储文件服务。

## 11. 面试速讲模板

### 11.1 30 秒版

OnCall 是一个多 Agent 运维系统，后端用 GoFrame + Eino ADK。它把故障处理拆成诊断、计划、审批、执行、验证、重规划和最终报告，并用 SSE 实时输出。高风险计划先经过 `plan_gate` / `plan_approval`，命令级变更在 `execute_step` 继续中断等人工审批，审批后用 `checkpoint_id + interrupt_ids` 从断点恢复。状态上把会话记忆、Graph State、Checkpoint 分层，既能控 token，也能保证长流程可恢复。

### 11.2 2 分钟版

项目有两条主链路：一条是 `chat_stream` 的对话链，做意图识别、知识检索和轻量运维工具调用；另一条是 `ai_ops_stream` 的故障处置链。故障链是 `Sequential + Loop`：`incident_analysis` 统一完成观测/RCA/修复意图，`diagnosis_gate` 决定是否能进入计划，`plan` 产出 canonical ExecutionPlan，`plan_gate` 与 `plan_approval` 绑定整份计划，`execute_plan` 只消费已批准计划，`verify_plan` 做全计划验收，`replan_decider` 决定完成、重新观测、转人工或终止，最后 `final_report` 汇总落盘。
安全上是两层加一条回路：`plan_gate` 做计划级完整性/风险/回滚筛查，`execute_step` 对变更命令逐步审批并可恢复执行，`replan_decider` 把失败统一收敛成结构化 ReplanDecision。状态层分三块：SessionMemory 管 token、IncidentState/PlanState/ReplanState 管流程语义、Checkpoint 管可恢复执行。这样既保证可操作性，也保证生产安全边界。

## 12. 关键代码索引

- 服务入口：`main.go`
- 路由契约：`api/chat/v1/chat.go`
- 控制器与 SSE：`internal/controller/chat/chat_v1.go`
- 应用装配：`internal/bootstrap/app.go`
- 工作流编排：`internal/agent/ops/incident_workflow.go`
- Gate 与最终报告：`internal/agent/ops/incident_nodes.go`
- Graph State 与历史重写：`internal/agent/ops/state_bridge.go`
- Execution Agent：`internal/agent/execution/agent.go`
- 计划校验：`internal/agent/execution/tools/validate_plan.go`
- 步骤执行与审批：`internal/agent/execution/tools/execute_step.go`
- 执行内状态：`internal/agent/execution/tools/tool_call_state.go`
- Session Memory：`internal/context/session_memory.go`
- Redis Checkpoint：`internal/context/checkpoint_store.go`
- 记忆压缩实现：`utility/mem/mem.go`
- 前端 API：`Front_page/src/services/api.ts`
- 前端状态：`Front_page/src/store/useStore.ts`
