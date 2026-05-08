# Implementation Report: 三 Agent 协同对话编排

## Summary

将原有单一 `adk.ResumableAgent + adk.Runner` 架构重构为基于 `compose.NewGraph` 的三 Agent 协同模式。新增 `tools/middleware.go` 和 `graph.go`，重构 BashApprovalTool / DetailSelectionTool 剥离中断逻辑，修改 agent.go / app.go / service.go / cmd.go 以接入编排图。全量编译通过，所有测试绿灯。

## Assessment vs Reality

| Metric | Predicted (Plan) | Actual |
|---|---|---|
| Complexity | XL | XL |
| Files Changed | 8 | 9（含 controller/chat/chat_v1.go，计划未列） |
| New Files | 2 | 2（graph.go, tools/middleware.go） |

## Tasks Completed

| # | Task | Status | Notes |
|---|---|---|---|
| 1 | Refactor BashApprovalTool | ✅ Complete | 删除 interrupt 逻辑，保留纯执行 |
| 2 | Refactor DetailSelectionTool | ✅ Complete | 删除 interrupt 逻辑，保留纯校验 |
| 3 | Create tools/middleware.go | ✅ Complete | ApprovalMiddleware + SafeToolMiddleware |
| 4 | Add OrchState + instructions | ✅ Complete | |
| 5 | Add three agent builder functions | ✅ Complete | |
| 6 | Create graph.go | ✅ Complete | 含 collectAgentMessages、streamAgentMessages、drainIterToStream |
| 7 | Update app.go | ✅ Complete | |
| 8 | Update service.go | ✅ Complete | |
| 9 | Update cmd.go | ✅ Complete | 同步更新了 controller/chat/chat_v1.go |

## Validation Results

| Level | Status | Notes |
|---|---|---|
| Build | ✅ Pass | `go build ./...` 零错误 |
| Vet | ✅ Pass | `go vet ./...` 零警告 |
| Unit Tests | ✅ Pass | 所有测试绿灯；更新 agent_tools_test.go 反映新架构 |

## Files Changed

| File | Action | Notes |
|---|---|---|
| `internal/logic/agent/dialogue/tools/middleware.go` | CREATE | ApprovalMiddleware + SafeToolMiddleware |
| `internal/logic/agent/dialogue/graph.go` | CREATE | 三 Agent 编排图 |
| `internal/logic/agent/dialogue/tools/BashApprovalTool.go` | UPDATE | 去除 interrupt 逻辑 |
| `internal/logic/agent/dialogue/tools/detail_selection_tool.go` | UPDATE | 去除 interrupt 逻辑 |
| `internal/logic/agent/dialogue/agent.go` | UPDATE | +OrchState +三 Agent 构建函数 +指令常量 |
| `internal/logic/agent/dialogue/agent_tools_test.go` | UPDATE | 更新测试反映新架构 |
| `internal/logic/app/app.go` | UPDATE | DialogueAgent → OrchGraph |
| `internal/logic/chat/service.go` | UPDATE | 重写 ChatStream / ChatResumeStream |
| `internal/controller/chat/chat_v1.go` | UPDATE | 更新 NewV1 签名 |
| `internal/cmd/cmd.go` | UPDATE | DialogueAgent → OrchGraph |

## Deviations from Plan

1. **`controller/chat/chat_v1.go` 计划未列但需更新** — NewV1 接受 `adk.ResumableAgent` 参数，需同步改为 `compose.Runnable`。

2. **`complexRunner` 不在 Service 持有，而在 BuildOrchestrationGraph 内创建** — 图构建时直接创建并在 Lambda 闭包中捕获，Service 层无需感知。

3. **`agent_tools_test.go` 断言方向调整** — 测试原来断言 bash 不在 buildDialogueTools 中，新架构 bash 进入 complex agent 是预期行为，测试更新为文档化新行为而非阻断。

## Architecture Summary

```
ChatStream → orchGraph.Stream(ctx, messages, WithCheckPointID)
               ↓ gate_node (InvokableLambda)
               → Gate Agent: RAG 检索 + [RESOLVED]/[TO_COMPLEX] 标记
               ↓ ragResultRouter (AddBranch)
     answer_node ← [RESOLVED]          [TO_COMPLEX] → complex_node
  (StreamableLambda)                            (StreamableLambda)
   Answer Agent 流式输出             Complex Agent + ApprovalMiddleware
                                      interrupt → compose.StatefulInterrupt
                                      → stream.Recv() 返回 interruptData
                                      → handleGraphInterrupt → SSE

ChatResumeStream → orchGraph.Stream(nil, WithCheckPointID, WithStateModifier)
                    StateModifier 注入 OrchState.ResumeData + InterruptIDs
                    complex_node 读取 → complexRunner.ResumeWithParams(innerCPID)
                    → tool.GetResumeContext → ApprovalMiddleware 处理审批
```

## Next Steps
- [ ] 集成测试（启动服务，验证 RAG 命中路由到 answer_node，未命中路由到 complex_node）
- [ ] 验证 DetailSelection 中断/恢复端到端流程
- [ ] 验证 BashApproval 中断/恢复端到端流程
- [ ] Code review via `/code-review`
