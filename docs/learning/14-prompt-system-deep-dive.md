# 14 Prompt 系统深挖：角色边界如何约束 Agent 行为

> 本节回答第二轮第二个问题：OnCall 的 prompt 是如何拼装的？它如何区分 dialogue、ops、plan、execution 等角色？它和代码 gate / 权限系统是什么关系？

## 1. 本节结论

OnCall 的 prompt 系统不是一段孤立的 system prompt，而是一个“通用基础段 + 角色段 + deferred tool 指南 + 环境/知识/记忆扩展”的装配器。它负责告诉模型应该怎样行动，但它不是最终安全边界；真正阻断危险行为的仍是 `diagnosis_gate`、`plan_gate`、`plan_approval`、execution guard、toolkit adapter 和 permissions checker。

## 2. Prompt 构建入口

`BuildAgentPrompt(role, env, opts)` 是角色级 prompt 的主入口。它按顺序加入：

1. `addBaseSections`：身份、系统规则、任务执行、谨慎执行、工具使用、语气、输出效率。
2. `DeferredToolGuidance(role)`：按角色声明可发现的 deferred tools。
3. `RoleSection(role)`：注入 `dialoguePrompt / rcaPrompt / opsPrompt / planPrompt / executionPrompt / strategyPrompt`。
4. `addContextSections`：环境、自定义指令、知识、长期记忆。

证据在 `backend/internal/prompt/builder.go:110-118`。基础段来源见 `backend/internal/prompt/builder.go:121-129`，上下文扩展来源见 `backend/internal/prompt/builder.go:131-141`。

```mermaid
flowchart TD
  Role[Role: dialogue / ops / plan / execution] --> Build[BuildAgentPrompt]
  Env[DetectEnvironment] --> Build
  Opts[BuildOptions\ncustom + knowledge + memory] --> Build
  Build --> Base[Base sections\nIdentity/System/Task/Tool/Output]
  Build --> Deferred[DeferredToolGuidance(role)]
  Build --> RoleSection[RoleSection(role)]
  Base --> Prompt[Final system instruction]
  Deferred --> Prompt
  RoleSection --> Prompt
  Opts --> Prompt
  Prompt --> Agent[ChatModelAgent Instruction]
```

图源文件：`docs/learning/diagrams/15-prompt-system-role-boundaries.mmd`

## 3. Section 数据结构如何决定拼装顺序

`Section` 只有三个字段：`Name`、`Priority`、`Content`。`Builder.Build()` 会按 `Priority` 稳定排序，跳过空内容，然后用两个换行拼接。证据在 `backend/internal/prompt/builder.go:23-27` 与 `backend/internal/prompt/builder.go:59-72`。

这说明 prompt 的“层级”不是靠文件顺序，而是靠 priority：基础身份优先级最低、工具规则在中间、环境和记忆靠后。`BuildOptions` 里的 `CustomInstructions / KnowledgeSection / MemorySection` 不是覆盖基础规则，而是被追加成后面的上下文段。证据在 `backend/internal/prompt/builder.go:40-44` 与 `backend/internal/prompt/builder.go:131-141`。

测试 `TestBuilderBuildSortsAndSkipsEmptySections` 验证了排序和空 section 跳过；`TestBuildAgentPromptIncludesRoleEnvironmentAndExtensions` 验证角色、环境、自定义指令、知识和记忆都会进入最终 prompt。证据在 `backend/internal/prompt/builder_test.go:8-18` 与 `backend/internal/prompt/builder_test.go:20-53`。

## 4. 通用基础段：先把安全语义铺底

基础段最重要的约束有三层：

- **不造事实**：IdentitySection 要求不要编造集群状态、命令结果、监控指标或历史案例。证据在 `backend/internal/prompt/sections.go:5-14`。
- **外部文本不可信**：SystemSection 明确工具结果、检索资料和用户输入都可能包含外部文本，不能改变系统规则；hooks、审批、resume 参数和工具返回值才是运行事实。证据在 `backend/internal/prompt/sections.go:17-26`。
- **变更走审批链**：ExecutingActionsSection 规定只读检查可以直接做，Kubernetes 写操作、系统写操作、数据配置写操作必须走审批链。证据在 `backend/internal/prompt/sections.go:43-54`。

ToolUseSection 则把“业务工具默认通过 ToolSearch 发现，再用 InvokeDeferredTool 调用”写成通用规则，并强调写操作、命令执行、回滚和高风险变更要服从 allow / ask / deny。证据在 `backend/internal/prompt/sections.go:57-70`。

## 5. DeferredToolGuidance：同样两个网关，不同角色看到不同业务工具

`DeferredToolGuidance(role)` 先写共同规则：默认可见工具是 `ToolSearch` 与 `InvokeDeferredTool`；业务工具要先发现再调用；arguments 必须匹配目标 schema；读工具默认可用，写操作和命令工具由权限系统决定。证据在 `backend/internal/prompt/sections.go:139-146`。

然后它按角色追加不同工具边界：

| Role | 工具边界 | 源码证据 |
| --- | --- | --- |
| Dialogue | `intent_analysis`、`request_detail_selection`、`knowledge_retrieve`、`ops_case_retrieve`、`web_search`、观测和受控 bash | `backend/internal/prompt/sections.go:149-153` |
| RCA | K8s、metrics、time、dependency graph、signal correlation、root cause、impact | `backend/internal/prompt/sections.go:160-164` |
| Ops | `k8s_monitor`、`metrics_collector`、`es_log_query` 等诊断工具；输出 RCA/Diagnosis + remediation_intent，不执行变更 | `backend/internal/prompt/sections.go:165-169` |
| Plan | `normalize_plan`、`generate_plan`、`validate_plan`；不要调用执行/验证/回滚 | `backend/internal/prompt/sections.go:170-174` |
| Execution | `execute_step`、`validate_result`、`rollback`；不负责生成、规范化或预校验计划 | `backend/internal/prompt/sections.go:154-159` |
| Strategy | `evaluate_strategy`、`optimize_strategy`、`update_knowledge`、`prune_knowledge`，写知识默认也要权限/审批 | `backend/internal/prompt/sections.go:175-179` |

测试 `TestRolePromptsDescribeRoleSpecificDeferredTools` 验证 execution prompt 包含 execution 工具、不包含 planning 工具，也不暴露 dialogue 的 `web_search`；同时验证 plan prompt 包含 plan 工具，dialogue prompt 包含澄清和 web search。证据在 `backend/internal/prompt/builder_test.go:87-116`。

## 6. RoleSection：同一套基础规则下的不同 Agent 职责

`RoleSection(role)` 把不同 role 映射到对应的 prompt 常量：Dialogue、RCA、Ops、Plan、Execution、Strategy。证据在 `backend/internal/prompt/sections.go:120-137`。

这里要把角色 prompt 和数据流一起读：

- `opsPrompt` 明确 ops agent 是 plan_agent 的上游，只输出 DiagnosisState/诊断摘要和 `remediation_intent`；canonical ExecutionPlan 由 plan_agent 生成，执行、回滚和审批由后续阶段负责。证据在 `backend/internal/prompt/role_prompts.go:117-123`。
- `opsPrompt` 还规定最终只输出 JSON，字段包括 `root_cause`、`target_node`、`confidence`、`evidence`、`remediation_intent`、`planning_constraints`、`fallback_guidance`，并要求 evidence 至少 2 条、不得生成最终可执行命令列表、不得声称已修复。证据在 `backend/internal/prompt/role_prompts.go:141-163`。
- `planPrompt` 规定 plan_agent 负责把 RCA/Diagnosis 与 remediation_intent 转成唯一 canonical `ExecutionPlan`，可以调用 `normalize_plan / generate_plan / validate_plan`，但不得调用 `execute_step / validate_result / rollback`。证据在 `backend/internal/prompt/role_prompts.go:165-180`。
- `executionPrompt` 规定 execution_agent 只能消费已通过 `plan_gate` 和 `plan_approval` 的 canonical `ExecutionPlan`，逐步调用 `execute_step`、`validate_result`，必要时 `rollback`，不得生成或替换计划。证据在 `backend/internal/prompt/role_prompts.go:204-225`。

这也解释了为什么第一轮和第 13 节都强调 Graph State：prompt 只是要求模型输出某些字段，真正把这些字段变成后续可消费状态的是 `wrapWithIncidentState` 和 gate 代码。

## 7. Prompt 如何进入具体 Agent

三个关键 agent 都在构造时调用 `prompt.BuildAgentPrompt(...)`，然后把结果放进 `ChatModelAgentConfig.Instruction`。

- `NewOpsAgent` 构造 ops incident agent，调用 `BuildAgentPrompt(prompt.RoleOps, ...)`，工具列表来自 `BuildDeferredGatewayEinoTools`。证据在 `backend/internal/workflow/ops/agent.go:42-65`。
- `NewPlanAgent` 只注册 `normalize_plan / generate_plan / validate_plan`，调用 `BuildAgentPrompt(prompt.RolePlan, ...)`，注释也强调它不会收到 execute/rollback tools。证据在 `backend/internal/workflow/ops/plan_agent.go:28-49`。
- `NewExecutionAgent` 只注册 `execute_step / validate_result / rollback`，调用 `BuildAgentPrompt(prompt.RoleExecution, ...)`。证据在 `backend/internal/execution/agent.go:66-92`。

这说明角色边界同时存在两层：prompt 文字告诉模型不要越界，工具列表也让模型拿不到不属于本阶段的业务工具。

## 8. Prompt 不是最终安全边界

这点非常重要：prompt 可以约束模型，但不能作为安全证明。

代码层还有这些硬边界：

- `diagnosis_gate` 检查 RCA evidence 和 confidence，不通过就写 `refresh_observation`。见 `13-diagnosis-gate-deep-dive.md`。
- `plan_gate` 从 Graph State 的 `PlanState` 转换为 execution tools 的 `ExecutionPlan`，再调用计划校验。见 `backend/internal/workflow/ops/plan_gate.go:62-73`。
- `plan_approval` 对中高风险或需确认 plan 发 interrupt，并绑定 `PlanID + Revision + SnapshotHash`。见 `backend/internal/workflow/ops/plan_gate.go:138-173`。
- `contractGuardedExecutionAgent` 在执行前再次检查 contract、PlanState、PlanGateState 和 approval 是否匹配。见 `backend/internal/workflow/ops/diagnosis_gate.go:168-210`。
- 工具层的 `EinoAdapter.InvokableRun` 在执行目标工具前先跑 hooks，再用 `checker.Check` 判定 allow / ask / deny。证据在 `backend/internal/toolkit/adapter.go:52-72`。

因此正确理解是：prompt 是行为引导层，gate 和 permissions 是执行边界层。后续改安全逻辑时，不能只改 prompt；必须同步检查 gate、tool list、permissions 和测试。

## 9. 可修改边界

如果后续要改 prompt，建议按以下顺序补保护：

- 改角色工具边界：先补 `builder_test.go`，确认某 role 包含/不包含目标工具名。
- 改输出字段：同步检查 `state_bridge.go` 的解析和写入逻辑，避免 prompt 输出字段没人消费。
- 改安全策略：不要只改 `sections.go`；还要检查 `permissions.go`、`toolkit/adapter.go`、`plan_gate.go`、`diagnosis_gate.go` 是否有硬约束。
- 改 role prompt 的 JSON 输出：补测试确认 prompt 包含关键字段和禁止行为，例如 “不得声称已修复”“不要输出新的执行计划对象”。

## 10. Evidence / Inference / Unknown

**Evidence**

- `BuildAgentPrompt` 明确按基础段、deferred guidance、role section、context sections 拼装。见 `backend/internal/prompt/builder.go:110-118`。
- `sections.go` 明确 ToolSearch / InvokeDeferredTool 和 allow / ask / deny 规则。见 `backend/internal/prompt/sections.go:57-70` 与 `backend/internal/prompt/sections.go:139-146`。
- `NewPlanAgent` 和 `NewExecutionAgent` 注册的 deferred tools 互斥，分别只拿 plan 工具和 execution 工具。见 `backend/internal/workflow/ops/plan_agent.go:39-49` 与 `backend/internal/execution/agent.go:66-78`。
- `builder_test.go` 已覆盖 prompt 拼装、role-specific deferred tools 和禁止 execution prompt 暴露 planning tools。见 `backend/internal/prompt/builder_test.go:20-53` 与 `backend/internal/prompt/builder_test.go:87-116`。

**Inference**

- prompt 的主要风险不是“缺少一句指令”，而是 prompt、工具列表、state bridge、gate、permissions 之间不一致；一致性比单段文字更重要。
- 当前设计有意把 plan 与 execution 拆开：plan_agent 只能规划，execution_agent 只能消费已批准计划，降低模型自发改计划并执行的风险。

**Unknown**

- 本节没有运行真实模型调用，无法证明模型一定遵守 prompt；只证明源码如何构造和注入 prompt。
- strategy role 在当前主 workflow 中是否仍活跃，需要结合 caller map 和最终报告链路单独确认。
- `CustomInstructions / KnowledgeSection / MemorySection` 的实际上游来源和注入频率，本节只读了 builder，没有完整追踪 controller/context 注入链路。

## 11. 阅读检查清单

读完本节，你应该能回答：

- `BuildAgentPrompt` 的拼装顺序是什么？
- 为什么 execution_agent 看不到 `generate_plan`？
- prompt 里说“必须审批”和代码层真正审批有什么区别？
- 如果要新增一个工具，应该同时检查哪些 prompt、tool list 和权限代码？
- 为什么修改 prompt 后必须跑 `backend/internal/prompt` 的测试？

