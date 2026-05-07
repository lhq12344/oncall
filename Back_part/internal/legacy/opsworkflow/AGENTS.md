# AGENTS.md

适用范围：`internal/legacy/opsworkflow` 及其子目录（更深层 `AGENTS.md` 可覆盖）。

## OVERVIEW

`internal/legacy/opsworkflow` 归档旧运维多 Agent 工作流：根因分析、修复策略、命令执行、复盘沉淀。
该目录不属于当前对话/知识/搜索主启动路径。

## STRUCTURE

```text
internal/legacy/opsworkflow/
├── ops/         # Incident 工作流编排与状态桥接
├── rca/         # 根因推理与影响分析
├── execution/   # 命令级计划/校验/执行/回滚
├── strategy/    # 复盘报告与经验沉淀
└── infra/       # 旧运维依赖的 MySQL/Elasticsearch 客户端
```

## WHERE TO LOOK

| 任务 | 位置 | 说明 |
|---|---|---|
| 故障全流程编排 | `ops/incident_workflow.go` | Sequential + Loop 工作流骨架 |
| 执行门控与重试升级 | `ops/incident_nodes.go` | 重复问题熔断、人工升级、最终收口 |
| 根因分析 | `rca/agent.go`, `rca/tools/*` | 证据收集、关联、推理、影响评估 |
| 命令执行 | `execution/agent.go`, `execution/tools/*` | normalize/generate/validate/execute/rollback |
| 复盘策略 | `strategy/agent.go`, `strategy/tools/*` | 输出最终报告与知识沉淀 |

## CONVENTIONS

- 职责必须单一：`ops` 编排，`execution` 落命令，`strategy` 复盘。
- Agent 之间优先传结构化字段，不依赖“自然语言隐式契约”。
- 提示词改动后，必须保证 JSON 字段稳定（尤其 `ops/rca/execution/strategy` 输出对象）。
- 高风险执行必须走审批中断：先 `validate_plan`，再 `execute_step`，命中高风险后进入 interrupt/resume。
- 会话历史与图状态分离：用户上下文走 `Session Memory`，流程内状态走 `IncidentState/Graph State`。
- 大日志、大对象禁止直接塞进模型历史，优先摘要后回灌。

## ANTI-PATTERNS

- 跳过 `validate_plan` 直接执行命令。
- 在 `ops` 或 `dialogue` 内直接“口头宣布修复成功”而无工具证据。
- 忽略 `execution_gate` 的重复问题重试上限，导致循环失控。
- 新增工具却不复用现有同类工具，造成提示词与实现漂移。

## QUALITY GATES

- 修改 `internal/legacy/opsworkflow/**` 后至少执行：`go test ./...`。
- 若改动涉及 interrupt/resume：需联动检查 `internal/controller/chat/chat_v1.go` 与前端 `Front_page/src/services/api.ts`。
