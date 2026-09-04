# 12 第二轮学习计划：从“能解释”到“能安全修改”

> 目标：第一轮已经建立 OnCall 的架构、主链路、工具、RAG、前端 SSE 和构建测试地图；第二轮开始按专题深挖，目标是能定位关键 bug、判断修改影响、补测试并解释安全边界。

## 1. 第二轮完成标准

第二轮不追求读完所有文件，而是围绕高风险、高耦合、高收益的模块建立“可改代码”的理解：

- 能说清每个核心 gate 为什么挡住或放行 workflow。
- 能判断一次 AIOps 修改会影响 Graph State、审批、恢复、最终报告中的哪些字段。
- 能区分 prompt 约束、代码 gate、权限系统三层安全边界。
- 能解释 Hybrid RAG 的 query rewrite、embedding、BM25、RRF、rerank 和降级路径。
- 能为修改点找到现有测试入口，并知道缺失测试在哪里补。

## 2. 优先级地图

| 优先级 | 专题 | 为什么先读 | 产出文档 | 状态 |
| --- | --- | --- | --- | --- |
| P0 | `diagnosis_gate` 与 incident contract | 它决定是否允许从诊断进入计划，是 workflow 的第一道业务闸门 | `13-diagnosis-gate-deep-dive.md` | 已完成第一版 |
| P0 | prompt 系统与角色边界 | prompt 决定 agent 能做什么、应该调用哪些 deferred tools，但不能替代代码 gate | `14-prompt-system-deep-dive.md` | 已完成第一版 |
| P1 | permissions / RuleEngine / deferred gateway | 这是工具执行与人工审批的安全边界 | `15-permissions-rule-engine-deep-dive.md` | 已完成第一版 |
| P1 | Hybrid RAG 检索与 eval | 知识和历史案例会影响诊断质量，需要理解降级和质量门 | `16-hybrid-rag-eval-deep-dive.md` | 已完成第一版 |
| P1 | 前端 interrupt 三种交互实测 | 当前源码已读，下一步要结合浏览器验证 approve/reject/detail selection | `17-frontend-interrupt-qa.md` | 已完成第一版 |
| P2 | Milvus / embedding adapter / schema | 属于 RAG 底层存储，适合在 Hybrid RAG 主链路后读 | `18-milvus-embedding-schema.md` | 已完成第一版 |
| P2 | context compact / SessionMemory 深挖 | 与长会话稳定性相关，但不直接决定一次 AIOps gate | `19-context-compact-memory.md` | 已完成第一版 |
| P2 | final report archive 与 ops_case 闭环 | 影响历史案例沉淀和下一轮召回 | `20-final-report-archive-loop.md` | 已完成第一版 |

## 3. 第二轮阅读方法

每个专题都固定按下面顺序写：

1. **问题定义**：这个模块到底在防什么、决定什么？
2. **源码入口**：只列真实入口，不把保留代码当主链路。
3. **函数链路**：按调用顺序解释，数据结构融入函数讲解。
4. **状态字段**：只解释本专题会读写的 Graph State / store / UI state 字段。
5. **测试证据**：现有测试证明了什么，没证明什么。
6. **可修改边界**：如果后续要改，应该优先补哪些测试。
7. **Evidence / Inference / Unknown**：区分代码证据、推断和未验证事项。

## 4. 当前第一批源码锚点

第二轮第一批先从这些文件开始：

- `backend/internal/workflow/ops/incident_workflow.go`：workflow 成员注册、loop stage 顺序、职责注释。
- `backend/internal/workflow/ops/diagnosis_gate.go`：诊断证据门控、执行前守卫、approved plan 注入。
- `backend/internal/workflow/ops/state_bridge.go`：`IncidentState`、`PlanState`、`PlanGateState`、`PlanApprovalState`、`ReplanState`。
- `backend/internal/workflow/ops/plan_gate.go`：canonical plan 校验与审批绑定。
- `backend/internal/workflow/ops/diagnosis_gate_test.go`：contract/gate 的现有测试样例。
- `backend/internal/prompt/builder.go` 与 `backend/internal/prompt/sections.go`：prompt 装配与角色段落。
- `backend/internal/permissions/permissions.go` 与 `backend/internal/toolkit/adapter.go`：权限判定与工具调用拦截。
- `backend/internal/rag/hybrid.go` 与 `backend/internal/rag/rewrite.go`：Hybrid RAG 主流程。
- `frontend/src/services/api.ts` 与 `frontend/src/components/InterruptCard.tsx`：前端 SSE 与恢复交互。

## 5. 第二轮自测问题

先记录至少 10 个问题，后续每写完一篇就回填答案：

| 问题 | 当前答案位置 | 状态 |
| --- | --- | --- |
| `diagnosis_gate` 到底校验 RCA 还是校验 remediation proposal？ | `13-diagnosis-gate-deep-dive.md` | 已回答 |
| 为什么 `execute_plan` 还要再经过 `contractGuardedExecutionAgent`？ | `13-diagnosis-gate-deep-dive.md` | 已回答 |
| `IncidentContractValid`、`ValidationBlocked`、`ReplanState.Decision` 分别在什么时候写入？ | `13-diagnosis-gate-deep-dive.md` | 已回答 |
| prompt 说“不要执行变更”时，代码层是否还有 gate 兜底？ | `14-prompt-system-deep-dive.md` | 已回答 |
| ToolSearch 和 InvokeDeferredTool 为什么能减少工具滥用？ | `15-permissions-rule-engine-deep-dive.md` | 已回答 |
| `permissions.Checker` 的 allow / ask / deny 如何由 mode、rule、command/path 共同决定？ | `15-permissions-rule-engine-deep-dive.md` | 已回答 |
| Hybrid RAG 的 degraded 状态有哪些来源？ | `16-hybrid-rag-eval-deep-dive.md` | 已回答 |
| ops_case 本地 final report fallback 什么时候生效？ | `16-hybrid-rag-eval-deep-dive.md` / `20-final-report-archive-loop.md` | 已回答 |
| 前端 approve / reject / detail selection 最终分别发什么 resume payload？ | `17-frontend-interrupt-qa.md` | 已回答 |
| context compact 和 checkpoint resume 是否是同一层机制？ | `19-context-compact-memory.md` | 已回答 |

## 6. 本轮边界

- 本轮仍是源码学习笔记，不直接修改业务逻辑。
- 文档引用源码行号时，以当前 checkout 为准；后续重构后需要重新跑源码扫描。
- 浏览器交互、真实 Redis/Milvus/ES/K8s 依赖不在本篇中验证，相关项会在对应专题单独标注。


