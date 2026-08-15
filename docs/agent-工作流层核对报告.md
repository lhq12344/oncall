# Agent 与工作流层核对报告（任务 ta575a1f9）

> 基于 D:/Code/project/oncall 当前代码（module `go_agent`，HEAD=8e7e922），代码实测，与原文档 docs/项目介绍.md 逐条对照。生成时间：见 git log HEAD。

## 1. Agent 目录结构（internal/agent/，9 个子目录）

| 目录          | 职责（实测）                                                                                                            |
| ------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `dialogue/`   | 对话 Agent（意图分析 + 工具编排）                                                                                       |
| `ops/`        | 故障处置核心：workflow 编排、execution_gate、incident_contract_gate、final_reporter、observation_collector、Pod 日志→ES |
| `rca/`        | 根因分析（**已脱离工作流**，无调用方）                                                                                  |
| `execution/`  | 命令级计划/校验/执行/回滚（工作流实际使用）                                                                             |
| `strategy/`   | 策略评估/优化/知识更新（**已脱离工作流**，无调用方）                                                                    |
| `knowledge/`  | 知识上传专用 Chain                                                                                                      |
| `agentteams/` | **新增**（提交 0a72b06）：团队/多阶段工作流声明式构建                                                                   |
| `slash/`      | **新增**（提交 a49ead7）：slash 命令路由                                                                                |
| `toolkit/`    | **新增**：统一工具注册/网关/权限/hook                                                                                   |

另注意：`ops/` 下还有 `incident_observer.go`（observation_collector）、`integration.go`（IntegratedOpsExecutor）、`pod_log_shipper.go`（Pod 日志→ES 采集）、`state_bridge.go`（Graph State）、`incident_contract*.go`（契约校验）。

## 2. 工作流编排（最大变化点，与文档矛盾）

文件：`internal/agent/ops/incident_workflow.go`，关键函数 `NewIncidentWorkflowAgent`（行 56）。

当前形态：

```
Sequential(
  Loop(incident, incident_contract_gate, execution, gate),  // 最多 MaxExecutionLoops 次
  final_report
)
```

- 由 `newIncidentWorkflowTeam`（行 132）用 agentteams 构建：`AddLoopStage("incident_response_loop", ..., maxLoops, "incident","incident_contract_gate","execution","gate")` + `AddSequentialStage("incident_final_report_stage", ..., "final_report")`。
- 5 个成员：incident(ops_incident_agent)、incident_contract_gate、execution、gate、final_report。

### 与原文档的矛盾点

1. 文档 4.3 节 `Sequential(observation_collector, rca_agent, Loop(ops_agent, execution_agent, execution_gate), strategy_agent, final_reporter)` **已不成立**。
   - `newObservationCollectorAgent`（incident_observer.go）、`NewRCAAgent`（rca/agent.go）、`NewStrategyAgent`（strategy/agent.go）在工作流中**均无引用**；bootstrap/app.go 只初始化 dialogue 与 knowledge agent。
   - 观测/RCA 职责并入 ops_incident_agent（提示词 RoleOps，诊断工具集内嵌 rca 工具）；strategy 复盘不再出现在编排里。
2. **新增 incident_contract_gate 节点**（`internal/agent/ops/incident_contract_gate.go`）：执行前校验 RCA 证据与修复提案契约（缺失根因/证据 0/置信度<0.35/高风险缺 fallback 等），不通过打回 ops 重规划；execution 外层包 `newContractGuardedExecutionAgent`（行 74）防绕过。
3. **Loop 默认迭代 3 次：文档仍正确**。`internal/agent/agentteams/types.go:14` `DefaultLoopMaxIterations = 3`，经 `incident_workflow.go:15` `incidentDefaultMaxExecutionLoops` 传入；`cfg.MaxExecutionLoops<=0` 时取 3。
4. `incident_nodes.go`：execution_gate（`newExecutionGateAgent`，行 33）成功→break loop；失败→重规划；step_validation 触发 replan；重复问题达阈值（`maxRepeatedIssueRetries = 3`，行 20）→转人工（`buildRepeatedIssueStopEvent`，行 150）；审批中断走 `Resume`（行 387）。final_reporter（`newFinalReportAgent`，行 455）从 Graph State 生成报告并归档知识库。
5. `state_bridge.go`：`wrapWithIncidentState("incident"/"execution")` 将结构化结果写入 session values 防大日志回灌；`IncidentState`（行 28）相比文档新增 `IncidentContractValid/Issues`、`RepeatedIssue*`、`ExecutionPlan*`、`ValidationBlocked/Risk` 等字段。

## 3. 各 Agent 职责与工具集

### dialogue agent（`internal/agent/dialogue/agent.go`，`NewDialogueAgent`）

- 工具集（`buildDialogueTools`，行 122）8 个与文档一致、全部仍在：
  - `intent_analysis`（IntentAnalysisTool.go）、`request_detail_selection`（detail_selection_tool.go）、`knowledge_retrieve`（KnowledgeRetrieveTool.go）、`ops_case_retrieve`（OpsCaseRetrieveTool.go）、`bash_execute_with_approval`（BashApprovalTool.go）、`web_search`（WebSearchTool.go）、`k8s_monitor`、`metrics_collector`（后两者由 `NewDialogueK8sMonitorTool`/`NewDialogueMetricsCollectorTool` 转发到 ops/tools）。
- 注意：k8s/metrics 在 kubeconfig/Prom 不可用时降级 warn 而非报错；dialogue 用 `toolkit.BuildAlwaysEinoTools`（含通用文件工具）。

### rca agent（`internal/agent/rca/agent.go`，`NewRCAAgent`）

- 工具 7 个：`k8s_monitor`、`metrics_collector`（复用 ops/tools）、`time_query`、`build_dependency_graph`、`correlate_signals`、`infer_root_cause`、`analyze_impact`。
- 输出契约 `RCAReport`（`ops/incident_contract.go:18`）：`root_cause`/`target_node`/`path`/`impact`/`confidence`/`evidence`——与文档 5.4 节一致。
- **当前无调用方**，仅独立保留。

### ops agent（`internal/agent/ops/agent.go`，`NewOpsAgent`，名 `ops_incident_agent`）

- 产出 `RemediationProposal`（`incident_contract.go:39`）：proposal_id/summary/root_cause/target_node/risk_level/actions[]（goal/command_hint/success_criteria/rollback_hint/read_only）/fallback_plan。
- 工具 8 个全 deferred（`ops/diagnostic_toolset.go` `BuildDeferredTools`）：k8s_monitor/metrics_collector/es_log_query/time_query/build_dependency_graph/correlate_signals/infer_root_cause/analyze_impact。
- `defaultOpsIncidentAgentMaxIterations = 48`（agent.go:28）。

### execution agent（`internal/agent/execution/agent.go`，`NewExecutionAgent`）

- 工具链 6 个（文档 5.6 节正确）：`normalize_plan`→`generate_plan`→`validate_plan`→`execute_step`→`validate_result`→`rollback`（均 deferred）。
- `defaultExecutionAgentMaxIterations = 96`（agent.go:32）。
- 工作流外层包 `newContractGuardedExecutionAgent`。

### strategy agent（`internal/agent/strategy/agent.go`，`NewStrategyAgent`）

- 工具 4 个：`evaluate_strategy`/`optimize_strategy`/`update_knowledge`/`prune_knowledge`。
- **未接入工作流**（文档 5.8 节"参与复盘"不成立）。

### knowledge agent（`internal/agent/knowledge/agent.go`，`NewKnowledgeAgent`）

- 上传专用 Agent（非文件服务），`BuildKnowledgeUploadChain`（orchestration.go:59）走 file_loader→markdown_splitter→milvus_indexer，输出分片 ID。文档 5.2 节基本正确。

## 4. 新增模块

### agentteams/（提交 0a72b06）

- `types.go`：`Team`/`Member`/`Stage`（StageSequential/StageLoop，`DefaultLoopMaxIterations = 3`）。
- `builder.go`：`Build` 编译为 Eino ADK `NewSequentialAgent`/`NewLoopAgent`。
- 作用：incident workflow 从手写编排改为"成员注册+阶段声明"，loop 上限复用常量保证确定性。

### slash/（提交 a49ead7）

- `parser.go`（`Parse` 识别 `/cmd args`）、`registry.go`（`Registry` 命令+别名+冲突检测）、`builtin.go`（`CreateDefaultRegistry`）、`loader.go`（frontmatter 解析，加载 `.oncall/commands` SourceProject 与 `.mewcode/commands` SourceMewCompat，`$ARGUMENTS` 替换）。
- 内置 13 个命令（builtin.go:29-47）：
  - 本地/信息：`/help`(h)、`/commands`、`/status`(s)、`/hooks`(hook)、`/session`、`/memory`
  - prompt 类：`/review`、`/diagnose`(diag)、`/k8s`(pods)、`/metrics`(prom)、`/logs`(last-error,errors)、`/cases`
  - ops 工作流类：`/ops`(incident,aiops)（TypeOpsWorkflow 触发完整 AI 运维处置工作流）
  - 客户端动作：`/clear`
- 消费方：`internal/controller/chat/chat_v1.go:635` `handleSlashCommand`。

## 5. 工具集 toolkit/

- `types.go`：`Tool` 接口（Name/Description/Category/Schema/Execute，Category 分 read/write/command）+ `Registry`（deferred 集合 + 按 session 的发现状态）。
- `gateway.go`：`ToolSearch`（select: 精确或关键词检索 deferred 工具）与 `InvokeDeferredTool`（调用已发现 deferred 工具，链路 pre-hook→permission check→审批中断→执行→post-hook）。
- `adapter.go`：`EinoAdapter` 适配 eino BaseTool；两个构建入口——`BuildAlwaysEinoTools`（含通用文件工具，dialogue/rca/strategy 用）与 `BuildDeferredGatewayEinoTools`（只暴露 ToolSearch/InvokeDeferredTool 两 always 工具、无文件编辑能力，ops/execution 用，作为执行安全边界）。
- 通用文件工具（PascalCase）：`ReadFile`/`EditFile`/`WriteFile`/`Glob`/`Grep`。
- `hooks.go`：pre/post/approval-request 三类 hook 挂点（提交 f5bb856）。
- 其它：`file_state_cache.go`（EditFile 前必须 ReadFile）、`MaxOutputChars=10000`、`SkipDirs`。

## 6. 矛盾汇总（重写时修正）

1. 工作流结构变更：`Sequential(Loop(incident, incident_contract_gate, execution, gate), final_report)`。
2. rca_agent/strategy_agent/observation_collector 代码仍在但**不在编排中**（bootstrap 仅初始化 dialogue、knowledge）。
3. 新增 incident_contract_gate 节点及 agentteams/、slash/、toolkit/ 三个目录，文档未提及。
4. Loop 默认 3 次、dialogue 8 工具、execution 6 工具链、RCA 六字段输出：**文档均正确，无需改动**。
5. slash `/ops` 路由是入口层的重要补充（归属入口/API 层范围）。
