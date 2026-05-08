# Plan: 三 Agent 协同对话编排（compose.Graph + AddBranch，含流式 + Interrupt/Resume）

## Summary

将现有单一 `adk.ResumableAgent + adk.Runner` 替换为基于 `compose.NewGraph` 的三 Agent 编排图。Gate Agent 负责 RAG 检索并路由，Answer Agent 在 RAG 充足时流式回复，Complex Agent 在 RAG 不足时使用全工具集流式处理（含 BashApproval / DetailSelection 的 interrupt/resume）。Graph 原生接管流式输出和 checkpoint，Service 层不再依赖 `adk.Runner`。

## User Story

作为 OnCall 系统用户，我希望系统先检索知识库做快速路由；知识库命中时直接流式整理回复；未命中时调用含 Skill 和工具审批的复杂 Agent，且全程保留选择卡片、命令审批等中断能力。

## Problem → Solution

**现状**：单一 `adk.Runner(ResumableAgent)` 处理所有对话，token 流式通过 `AgentEvent` 迭代推送 SSE。

**目标**：`compose.Graph` 编排三 Agent，Gate(Invoke) → Branch → Answer/Complex(Stream)，图层原生处理 checkpoint/interrupt/resume，Service 层调用 `graph.Stream()` 替代 `runner.Run()`。

## Metadata

- **Complexity**: XL
- **Source PRD**: N/A
- **PRD Phase**: N/A
- **Estimated Files**: 8 文件（2 新建 + 6 修改）

---

## 架构设计（先读此节）

### 图类型

```go
compose.NewGraph[[]*schema.Message, *schema.Message](
    compose.WithGenLocalState(func(ctx context.Context) *OrchState { ... }),
)
```

- **输入**：`[]*schema.Message`（由 SessionMemory 构建的完整对话历史）
- **输出**：`*schema.Message`（最终助手消息，流式推送）
- **图状态** `OrchState`：跨节点共享，存储 inner checkpoint ID 和 resume 数据

### 节点设计

| 节点 | Lambda 类型 | 职责 |
|---|---|---|
| `gate_node` | `InvokableLambda` | 运行 Gate Agent（RAG 检索 + 意图标记），批量返回 `[]*schema.Message` |
| Branch | `NewGraphBranch` | 检查 Gate 输出中的 `[RESOLVED]`/`[TO_COMPLEX]` 路由到 answer 或 complex |
| `answer_node` | `StreamableLambda` | 运行 Answer Agent，流式返回 `*schema.StreamReader[*schema.Message]` |
| `complex_node` | `StreamableLambda` | 运行 Complex Agent（含完整工具集），流式 + interrupt/resume |

### Interrupt/Resume 机制

```
第一次请求（ChatStream）:
  graph.Stream(ctx, msgs, compose.WithCheckPointID(outerCPID))
    → complex_node 检测到 ADK interrupt
    → 预存 innerCPID 到本地状态
    → compose.StatefulInterrupt(ctx, adkInterruptData, innerCPID)
    → 图保存 checkpoint，stream 返回 interrupt error
    → Service 提取 interrupt info → SSE 推送 interrupt payload

恢复请求（ChatResumeStream）:
  graph.Stream(ctx, nil,
    compose.WithCheckPointID(outerCPID),
    compose.WithStateModifier(func sets orchState.ResumeData))
    → complex_node 读取 GetInterruptState[string] → innerCPID
    → complex_node 读取 orchState.ResumeData
    → complexRunner.ResumeWithParams(innerCPID, buildTargets(resumeData))
    → 继续流式输出
```

### 双层 Checkpoint

| 层 | 使用者 | 存储内容 |
|---|---|---|
| Outer Graph Checkpoint | compose.Graph | 图执行状态（在哪个节点中断、OrchState） |
| Inner ADK Checkpoint | complex_node 内的 complexRunner | Agent 内部 ReAct 状态（工具调用链、中断点） |

两者共用同一个 `CheckPointStore`（Redis），但 key 不同（outer 用 `sessionID:uuid`，inner 用 `complex:uuid`）。

---

## UX Design

### Before（当前实现）
```
ChatStream → runner.Run() → AgentEvent iterator → writeSSEData(chunk)
ChatResumeStream → runner.ResumeWithParams() → AgentEvent iterator → writeSSEData(chunk)
```

### After（新实现）
```
ChatStream → graph.Stream() → schema.StreamReader[*schema.Message] → writeSSEData(msg.Content)
ChatResumeStream → graph.Stream(WithCheckPointID + WithStateModifier) → 同上
```

**前端体验**：SSE 协议不变（`data: ...` 格式），`[DONE]` 结束标记不变，interrupt payload JSON 格式不变（`type: interrupt, checkpoint_id, interrupt_contexts`）。

### Interaction Changes

| Touchpoint | Before | After | Notes |
|---|---|---|---|
| SSE 内容推送 | AgentEvent.Content token 块 | `*schema.Message` 块（token 级别，StreamableLambda） | 语义相同 |
| Interrupt SSE | `buildInterruptPayload(cpID, adkInterruptInfo)` | 相同，数据来源从 event 改为 graph interrupt info | 前端格式不变 |
| Resume API | `runner.ResumeWithParams` | `graph.Stream + WithStateModifier` | 后端内部变化，前端 API 不变 |

---

## Mandatory Reading

| Priority | File | Lines | Why |
|---|---|---|---|
| P0 | `Back_part/internal/logic/agent/dialogue/agent.go` | 全部 | 现有 Agent 构建模式、Config、noFormatGenModelInput、buildDialogueTools |
| P0 | `Back_part/internal/logic/chat/service.go` | 31-91 | Service 结构体、NewService、checkpoint store 创建 |
| P0 | `Back_part/internal/logic/chat/service.go` | 141-251 | ChatStream 主循环（需要完全重写） |
| P0 | `Back_part/internal/logic/chat/service.go` | 277-370 | ChatResumeStream（需要重写核心 resume 逻辑） |
| P1 | `Back_part/internal/logic/agent/knowledge/orchestration.go` | 全部 | 项目中唯一 compose.Graph 使用示例 |
| P1 | `Back_part/internal/logic/app/app.go` | 全部 | Application 结构体和初始化入口 |
| P1 | `Back_part/internal/logic/agent/dialogue/tools/BashApprovalTool.go` | 全部 | interrupt 触发方式（`compose.NewInterruptAndRerunErr`），resume 数据接收 |
| P1 | `Back_part/internal/logic/agent/dialogue/tools/detail_selection_tool.go` | 全部 | interrupt 触发方式，resume 数据接收 |
| P2 | `Back_part/internal/logic/session/session_memory.go` | 全部 | BuildMessages、SaveTurnWithSource 接口（不变） |

## External Documentation

| Topic | Source | Key Takeaway |
|---|---|---|
| compose.Graph Branch | `/home/lhq/.codex/skills/eino-compose/reference/graph.md` | `NewGraphBranch(conditionFn, endNodes)` + `g.AddBranch("node", branch)` |
| compose.Graph Checkpoint/Interrupt | `/home/lhq/.codex/skills/eino-compose/reference/checkpoint-and-state.md` | `StatefulInterrupt`、`GetInterruptState`、`GetResumeContext`、`WithStateModifier`、`ExtractInterruptInfo` |
| compose.Graph Stream | `/home/lhq/.codex/skills/eino-compose/reference/stream.md` | `StreamableLambda`、`schema.Pipe`、`StreamReader.Recv` |

---

## Patterns to Mirror

### AGENT_CONSTRUCTION
```go
// SOURCE: Back_part/internal/logic/agent/dialogue/agent.go:95-128
agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    Name:          "dialogue_agent",
    Model:         cfg.ChatModel.Client,
    GenModelInput: noFormatGenModelInput,
    ToolsConfig:   adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: toolsList}},
    Handlers:      handlers,
    Instruction:   `...`,
})
```

### COMPOSE_GRAPH_PATTERN
```go
// SOURCE: Back_part/internal/logic/agent/knowledge/orchestration.go:33-56
g := compose.NewGraph[document.Source, []string]()
g.AddLoaderNode("file_loader", fileLoader)
g.AddEdge(compose.START, "file_loader")
return g.Compile(ctx, compose.WithGraphName("knowledge_indexing"))
```

### STREAMABLE_LAMBDA_PATTERN
```go
// SOURCE: /home/lhq/.codex/skills/eino-compose/reference/stream.md
compose.StreamableLambda(func(ctx context.Context, in InputType) (*schema.StreamReader[OutputType], error) {
    sr, sw := schema.Pipe[OutputType](capacity)
    go func() {
        defer sw.Close()
        // produce chunks via sw.Send(chunk, nil)
    }()
    return sr, nil
})
```

### STATEFUL_INTERRUPT_PATTERN
```go
// SOURCE: /home/lhq/.codex/skills/eino-compose/reference/checkpoint-and-state.md
// 触发中断（在 Lambda 的 goroutine 内）：
interruptErr := compose.StatefulInterrupt(ctx, interruptData, localStateToPreserve)
sw.Send(nil, interruptErr)
// 检测恢复（在同一 Lambda 的下次调用）：
wasInterrupted, _, localState := compose.GetInterruptState[LocalStateType](ctx)
```

### GRAPH_INTERRUPT_EXTRACT_PATTERN
```go
// SOURCE: /home/lhq/project/My_oncall/example.go（safeToolMiddleware）
// compose.IsInterruptRerunError 返回 (interruptData any, ok bool)
// interruptData 是 compose.StatefulInterrupt 调用时传入的第一个参数
stream, err := r.Stream(ctx, input, compose.WithCheckPointID(checkpointID))
msg, recvErr := stream.Recv()
if interruptData, ok := compose.IsInterruptRerunError(recvErr); ok {
    // 处理中断：interruptData 即 StatefulInterrupt 第一参数（本项目传入 *adk.InterruptInfo）
}
```

### GRAPH_RESUME_PATTERN
```go
// SOURCE: /home/lhq/.codex/skills/eino-compose/reference/checkpoint-and-state.md
stream, err = r.Stream(ctx, nil,
    compose.WithCheckPointID(checkpointID),
    compose.WithStateModifier(func(ctx context.Context, _ compose.NodePath, state any) error {
        s := state.(*MyState)
        s.Approved = true
        return nil
    }),
)
```

### ERROR_HANDLING
```go
// SOURCE: Back_part/internal/logic/agent/dialogue/agent.go:82-84
if err != nil {
    return nil, fmt.Errorf("failed to create xxx: %w", err)
}
```

---

## Files to Change

| File | Action | Justification |
|---|---|---|
| `Back_part/internal/logic/agent/dialogue/tools/middleware.go` | CREATE | `approvalMiddleware`（拦截 bash/selection 工具触发中断）+ `safeToolMiddleware`（所有工具错误转字符串，保留中断透传） |
| `Back_part/internal/logic/agent/dialogue/graph.go` | CREATE | 三 Agent 编排图的全部构建逻辑 |
| `Back_part/internal/logic/agent/dialogue/tools/BashApprovalTool.go` | UPDATE | 删除工具内部 interrupt 逻辑（`tool.GetInterruptState`/`tool.Interrupt`/`tool.GetResumeContext`），`InvokableRun` 变为纯执行 |
| `Back_part/internal/logic/agent/dialogue/tools/detail_selection_tool.go` | UPDATE | 同上，删除 interrupt 逻辑，`InvokableRun` 变为纯参数校验 + 返回选项列表 |
| `Back_part/internal/logic/agent/dialogue/agent.go` | UPDATE | 添加三个 Agent 构建函数、OrchState 导出、Agent 指令常量；`newComplexAgent` 注册两个新 middleware |
| `Back_part/internal/logic/app/app.go` | UPDATE | 替换 DialogueAgent 为 OrchestrationGraph + ComplexAgent |
| `Back_part/internal/logic/chat/service.go` | UPDATE | 替换 chatStreamRunner 为 orchGraph；重写 ChatStream + ChatResumeStream |
| `Back_part/internal/cmd/cmd.go` | UPDATE | 更新 chat.NewService 调用参数 |

## NOT Building

- 多轮 Gate → Complex → Gate 循环（单次路由，不回流）
- Gate Agent 的 interrupt（Gate 仅做 RAG + 分类，无 interrupt 工具）
- Answer Agent 的 interrupt（Answer 无工具，无 interrupt）
- compose.Graph 的并行节点（当前为串行路由）

---

## Step-by-Step Tasks

### Task 1: 重构 BashApprovalTool — 去除 interrupt 逻辑，变为纯执行工具

- **ACTION**: 删除 `BashApprovalTool.go` 中 `InvokableRun` 的 interrupt/resume 逻辑，保留参数解析、白名单校验、命令执行
- **IMPLEMENT**:

  删除以下代码段（`InvokableRun` 中，约第 188—261 行）：
  ```go
  // 删除这段：首次执行触发中断
  wasInterrupted, _, _ := tool.GetInterruptState[any](ctx)
  if !wasInterrupted {
      return "", tool.Interrupt(ctx, &BashApprovalInterruptInfo{...})
  }
  // 删除这段：恢复执行路径（检查 isResumeTarget / approved / resolved / comment）
  isResumeTarget, hasData, resumeData := tool.GetResumeContext[map[string]any](ctx)
  if !isResumeTarget || !hasData { ... }
  approved, resolved, comment := parseBashApprovalDecision(resumeData)
  if resolved { ... return marshalBashExecuteResult(...) }
  if !approved { ... return marshalBashExecuteResult(...) }
  ```

  重构后的 `InvokableRun` 结构（参数校验通过后直接执行）：
  ```go
  func (t *BashApprovalTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
      in, err := parseBashApprovalArgs(argumentsInJSON)
      if err != nil {
          return "", fmt.Errorf("invalid arguments: %w", err)
      }
      // ... 现有的 in.Command/Script/Timeout 校验逻辑（保留）...
      if _, ok := t.allowedCommands[in.Command]; !ok {
          return "", fmt.Errorf("command not in whitelist: %s", in.Command)
      }
      if err := t.validateArgs(in.Args); err != nil {
          return "", err
      }
      // 直接执行（审批由 approvalMiddleware 在调用前处理）
      result := t.executeCommand(ctx, in.Command, in.Args, in.Timeout)
      result.Approved = true
      result.Executed = true
      if t.logger != nil {
          t.logger.Info("bash command executed",
              zap.String("command", in.Command),
              zap.Bool("success", result.Success))
      }
      return marshalBashExecuteResult(result)
  }
  ```

  同时删除不再使用的 imports：`tool.GetInterruptState`、`tool.Interrupt`、`tool.GetResumeContext` 相关导入（如果 `tool` 包只剩 `tool.BaseTool`/`tool.Option` 则保留）。  
  `parseBashApprovalDecision`、`boolFromBashAny` 函数**保留**（供 middleware 使用）。

- **MIRROR**: 现有 `executeCommand` 函数（第 365—395 行）
- **GOTCHA**: `gob.Register(&BashApprovalInterruptInfo{})` 的 `init()` 函数**保留**，因为 middleware 仍然在 `tool.StatefulInterrupt` 中使用该类型，需要 gob 序列化
- **VALIDATE**: `go build ./internal/logic/agent/dialogue/tools/...`

---

### Task 2: 重构 DetailSelectionTool — 去除 interrupt 逻辑，变为纯校验工具

- **ACTION**: 删除 `detail_selection_tool.go` 中 `InvokableRun` 的 interrupt/resume 逻辑

- **IMPLEMENT**:

  删除以下代码段（约第 154—185 行）：
  ```go
  // 删除：tool.GetInterruptState / tool.Interrupt / tool.GetResumeContext 逻辑
  wasInterrupted, _, _ := tool.GetInterruptState[any](ctx)
  if !wasInterrupted {
      return "", tool.Interrupt(ctx, info)
  }
  isResumeTarget, hasData, resumeData := tool.GetResumeContext[map[string]any](ctx)
  if !isResumeTarget || !hasData {
      return "", tool.Interrupt(ctx, info)
  }
  selectionValue := parseDetailSelectionValue(resumeData)
  selectedOption, ok := findDetailSelectionOption(in.Options, selectionValue)
  if !ok { ... return "", tool.Interrupt(ctx, info) }
  result := DetailSelectionResult{...}
  out, err := json.Marshal(result)
  return string(out), nil
  ```

  重构后的 `InvokableRun`（校验通过后返回选项列表，实际选择由 middleware 处理）：
  ```go
  func (t *DetailSelectionTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
      // ... 现有的参数解析和选项校验（保留）...
      
      // 工具自身不再负责中断/恢复，仅返回选项列表供 middleware 或调试使用
      // 正常执行路径不应到达此处（approvalMiddleware 会在调用前中断）
      out, err := json.Marshal(map[string]any{
          "field":    in.Field,
          "question": in.Question,
          "options":  in.Options,
          "status":   "awaiting_selection",
      })
      if err != nil {
          return "", fmt.Errorf("failed to marshal options: %w", err)
      }
      return string(out), nil
  }
  ```

  `parseDetailSelectionValue`、`findDetailSelectionOption`、`normalizeDetailSelectionOptions` 函数**全部保留**（供 middleware 使用）。

- **MIRROR**: 现有 normalizeDetailSelectionOptions 校验逻辑（第 188—216 行）
- **GOTCHA**: `gob.Register(&DetailSelectionInterruptInfo{})` 的 `init()` **保留**，理由同上
- **VALIDATE**: `go build ./internal/logic/agent/dialogue/tools/...`

---

### Task 3: 新建 tools/middleware.go — approvalMiddleware + safeToolMiddleware

- **ACTION**: 新建 `Back_part/internal/logic/agent/dialogue/tools/middleware.go`

- **IMPLEMENT**:

```go
package tools

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"

    "github.com/cloudwego/eino/adk"
    "github.com/cloudwego/eino/components/tool"
    "github.com/cloudwego/eino/compose"
    "github.com/cloudwego/eino/schema"
    "go.uber.org/zap"
)

// approvalMiddleware 为 bash_execute_with_approval 和 request_detail_selection 工具
// 提供中断门控能力。工具本身只负责执行，中断/恢复逻辑完全在此 middleware 处理。
type ApprovalMiddleware struct {
    *adk.BaseChatModelAgentMiddleware
    Logger *zap.Logger
}

// approvalToolNames 需要中断门控的工具名称集合。
var approvalToolNames = map[string]struct{}{
    "bash_execute_with_approval": {},
    "request_detail_selection":   {},
}

func (m *ApprovalMiddleware) WrapInvokableToolCall(
    _ context.Context,
    endpoint adk.InvokableToolCallEndpoint,
    tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
    if _, needsApproval := approvalToolNames[tCtx.Name]; !needsApproval {
        return endpoint, nil
    }

    return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
        wasInterrupted, _, storedArgs := tool.GetInterruptState[string](ctx)

        if !wasInterrupted {
            // 首次调用：构建中断信息并触发中断，args 作为本地状态保存
            interruptInfo, err := buildInterruptInfo(tCtx.Name, args)
            if err != nil {
                return "", fmt.Errorf("failed to build interrupt info for %s: %w", tCtx.Name, err)
            }
            return "", tool.StatefulInterrupt(ctx, interruptInfo, args)
        }

        // 恢复路径：读取审批/选择结果
        isTarget, hasData, resumeData := tool.GetResumeContext[map[string]any](ctx)
        if !isTarget || !hasData {
            // 不是本次 resume 的目标，或数据缺失：重新中断
            interruptInfo, err := buildInterruptInfo(tCtx.Name, storedArgs)
            if err != nil {
                return "", fmt.Errorf("failed to rebuild interrupt info for %s: %w", tCtx.Name, err)
            }
            return "", tool.StatefulInterrupt(ctx, interruptInfo, storedArgs)
        }

        return handleResumeResult(ctx, tCtx.Name, storedArgs, resumeData, endpoint, opts)
    }, nil
}

func (m *ApprovalMiddleware) WrapStreamableToolCall(
    _ context.Context,
    endpoint adk.StreamableToolCallEndpoint,
    tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
    if _, needsApproval := approvalToolNames[tCtx.Name]; !needsApproval {
        return endpoint, nil
    }

    return func(ctx context.Context, args string, opts ...tool.Option) (*schema.StreamReader[string], error) {
        wasInterrupted, _, storedArgs := tool.GetInterruptState[string](ctx)

        if !wasInterrupted {
            interruptInfo, err := buildInterruptInfo(tCtx.Name, args)
            if err != nil {
                return nil, fmt.Errorf("failed to build interrupt info for %s: %w", tCtx.Name, err)
            }
            return nil, tool.StatefulInterrupt(ctx, interruptInfo, args)
        }

        isTarget, hasData, resumeData := tool.GetResumeContext[map[string]any](ctx)
        if !isTarget || !hasData {
            interruptInfo, err := buildInterruptInfo(tCtx.Name, storedArgs)
            if err != nil {
                return nil, fmt.Errorf("failed to rebuild interrupt info for %s: %w", tCtx.Name, err)
            }
            return nil, tool.StatefulInterrupt(ctx, interruptInfo, storedArgs)
        }

        result, err := handleResumeResult(ctx, tCtx.Name, storedArgs, resumeData, nil, nil)
        if err != nil {
            return nil, err
        }
        return singleChunkReader(result), nil
    }, nil
}

// buildInterruptInfo 根据工具名称和 args JSON 构建对应的中断信息结构。
func buildInterruptInfo(toolName, argsJSON string) (any, error) {
    switch toolName {
    case "bash_execute_with_approval":
        var in bashApprovalArgs
        if err := json.Unmarshal([]byte(argsJSON), &in); err != nil {
            return nil, fmt.Errorf("invalid bash args: %w", err)
        }
        timeout := in.Timeout
        if timeout <= 0 {
            timeout = defaultBashTimeoutSeconds
        }
        return &BashApprovalInterruptInfo{
            Command: strings.TrimSpace(in.Command),
            Args:    in.Args,
            Script:  strings.TrimSpace(in.Script),
            Timeout: timeout,
            Reason:  strings.TrimSpace(in.Reason),
        }, nil

    case "request_detail_selection":
        type selArgs struct {
            Field    string                  `json:"field"`
            Question string                  `json:"question"`
            Reason   string                  `json:"reason"`
            Options  []DetailSelectionOption `json:"options"`
        }
        var in selArgs
        if err := json.Unmarshal([]byte(argsJSON), &in); err != nil {
            return nil, fmt.Errorf("invalid detail selection args: %w", err)
        }
        return &DetailSelectionInterruptInfo{
            Field:    strings.TrimSpace(in.Field),
            Question: strings.TrimSpace(in.Question),
            Reason:   strings.TrimSpace(in.Reason),
            Options:  in.Options,
        }, nil

    default:
        return map[string]any{"tool_name": toolName, "args": argsJSON}, nil
    }
}

// handleResumeResult 根据工具名称和审批数据决定执行方式并返回结果。
// bash_execute_with_approval：审批通过则调用 endpoint 执行命令；拒绝则返回拒绝消息。
// request_detail_selection：从 resumeData 提取选择值，直接构造结果，不调用 endpoint。
func handleResumeResult(
    ctx context.Context,
    toolName, storedArgs string,
    resumeData map[string]any,
    endpoint adk.InvokableToolCallEndpoint,
    opts []tool.Option,
) (string, error) {
    switch toolName {
    case "bash_execute_with_approval":
        approved, resolved, comment := parseBashApprovalDecision(resumeData)
        if resolved {
            result := BashExecuteResult{
                Approved: true, Resolved: true, Executed: false, Success: true,
                Comment: comment,
            }
            return marshalBashExecuteResult(result)
        }
        if !approved {
            result := BashExecuteResult{
                Approved: false, Executed: false, Success: false,
                Error: "command execution rejected by user", ExitCode: -1, Comment: comment,
            }
            return marshalBashExecuteResult(result)
        }
        // 审批通过：调用工具本体执行命令
        if endpoint == nil {
            return "", fmt.Errorf("endpoint is nil for bash tool resume")
        }
        return endpoint(ctx, storedArgs, opts...)

    case "request_detail_selection":
        // 不调用 endpoint；直接从 resumeData 构造选择结果
        type selArgs struct {
            Field    string                  `json:"field"`
            Question string                  `json:"question"`
            Options  []DetailSelectionOption `json:"options"`
        }
        var in selArgs
        if err := json.Unmarshal([]byte(storedArgs), &in); err != nil {
            return "", fmt.Errorf("failed to parse stored detail selection args: %w", err)
        }
        selectionValue := parseDetailSelectionValue(resumeData)
        selectedOption, ok := findDetailSelectionOption(in.Options, selectionValue)
        if !ok {
            return "", fmt.Errorf("invalid or missing selection_value: %q", selectionValue)
        }
        result := DetailSelectionResult{
            Field:         in.Field,
            Question:      in.Question,
            SelectedValue: selectedOption.Value,
            SelectedLabel: selectedOption.Label,
        }
        out, err := json.Marshal(result)
        if err != nil {
            return "", fmt.Errorf("failed to marshal detail selection result: %w", err)
        }
        return string(out), nil

    default:
        return "", fmt.Errorf("unknown approval tool: %s", toolName)
    }
}

// singleChunkReader 创建只含一个 chunk 的 StreamReader[string]。
func singleChunkReader(msg string) *schema.StreamReader[string] {
    r, w := schema.Pipe[string](1)
    _ = w.Send(msg, nil)
    w.Close()
    return r
}

// SafeToolMiddleware 包装所有工具调用，将普通 error 转为字符串结果（防止工具错误中断 Agent ReAct 循环）。
// 唯一例外：interrupt rerun error 必须原样透传（由 compose.IsInterruptRerunError 判断）。
type SafeToolMiddleware struct {
    *adk.BaseChatModelAgentMiddleware
}

func (m *SafeToolMiddleware) WrapInvokableToolCall(
    _ context.Context,
    endpoint adk.InvokableToolCallEndpoint,
    _ *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
    return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
        result, err := endpoint(ctx, args, opts...)
        if err != nil {
            // 中断错误必须透传，不能转字符串
            if _, ok := compose.IsInterruptRerunError(err); ok {
                return "", err
            }
            return fmt.Sprintf("[tool error] %v", err), nil
        }
        return result, nil
    }, nil
}

func (m *SafeToolMiddleware) WrapStreamableToolCall(
    _ context.Context,
    endpoint adk.StreamableToolCallEndpoint,
    _ *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
    return func(ctx context.Context, args string, opts ...tool.Option) (*schema.StreamReader[string], error) {
        sr, err := endpoint(ctx, args, opts...)
        if err != nil {
            if _, ok := compose.IsInterruptRerunError(err); ok {
                return nil, err
            }
            return singleChunkReader(fmt.Sprintf("[tool error] %v", err)), nil
        }
        return safeWrapReader(sr), nil
    }, nil
}

// safeWrapReader 将 StreamReader 的 error 转为字符串 chunk（中断错误除外）。
func safeWrapReader(sr *schema.StreamReader[string]) *schema.StreamReader[string] {
    r, w := schema.Pipe[string](64)
    go func() {
        defer w.Close()
        for {
            chunk, err := sr.Recv()
            if err != nil {
                if isEOF(err) {
                    return
                }
                if _, ok := compose.IsInterruptRerunError(err); ok {
                    _ = w.Send("", err)
                    return
                }
                _ = w.Send(fmt.Sprintf("\n[tool error] %v", err), nil)
                return
            }
            _ = w.Send(chunk, nil)
        }
    }()
    return r
}

func isEOF(err error) bool {
    return err != nil && err.Error() == "EOF"
}
```

- **MIRROR**: `safeToolMiddleware` 和 `approvalMiddleware` 来自 example.go，完全对应
- **IMPORTS**: 见代码顶部
- **GOTCHA 1**: `compose.IsInterruptRerunError(err)` 返回 `(any, bool)`，第一个返回值是中断数据（当前用不到，用 `_` 接收）
- **GOTCHA 2**: `singleChunkReader` 中 `w.Send` 返回 bool，用 `_` 接收（`false` 表示 reader 已关闭，不影响结果）
- **GOTCHA 3**: middleware 文件在 `tools` 包内，可以直接使用 `BashApprovalInterruptInfo`、`parseBashApprovalDecision`、`DetailSelectionInterruptInfo`、`parseDetailSelectionValue`、`findDetailSelectionOption` 等包内函数
- **VALIDATE**: `go build ./internal/logic/agent/dialogue/tools/...`

---

### Task 4: 导出 OrchState 并添加三 Agent 指令常量（agent.go）

- **ACTION**: 在 `agent.go` 末尾添加 `OrchState`、三个 Agent 指令常量

（内容与原计划 Task 1 相同，略）

---

### Task 5: 添加三个 Agent 构建函数（agent.go）— 含 middleware 注册

- **ACTION**: 在 `agent.go` 中 `NewDialogueAgent` 之后添加三个工厂函数

  与原计划 Task 2 基本相同，**新增**：`newComplexAgent` 的 `handlers` 中加入两个 middleware：

```go
func newComplexAgent(ctx context.Context, cfg *Config, retriever einoretriever.Retriever) (adk.ResumableAgent, error) {
    toolsList := buildDialogueTools(cfg, retriever)

    summaryHandler, err := summarization.New(ctx, &summarization.Config{
        Model:   cfg.ChatModel.Client,
        Trigger: &summarization.TriggerCondition{ContextTokens: 300000},
    })
    if err != nil {
        return nil, fmt.Errorf("failed to create complex agent summarization: %w", err)
    }

    // 中间件顺序：approval（改变 tool 行为）→ safe（兜底错误转换）→ summarization（历史压缩）
    // 注意：middleware 按 slice 顺序依次包装，后加的在最外层先执行
    handlers := []adk.ChatModelAgentMiddleware{
        summaryHandler,
        &tools.SafeToolMiddleware{},     // 最外层：兜底
        &tools.ApprovalMiddleware{Logger: cfg.Logger}, // 中间：中断门控
    }

    skillHandler, err := newDialogueSkillMiddleware(ctx, cfg.SkillsDir, cfg.Logger)
    if err != nil {
        return nil, fmt.Errorf("failed to create complex agent skill middleware: %w", err)
    }
    if skillHandler != nil {
        handlers = append(handlers, skillHandler)
    }

    agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
        Name:          "complex_agent",
        Description:   "高级专家 Agent，处理复杂问题",
        Model:         cfg.ChatModel.Client,
        GenModelInput: noFormatGenModelInput,
        ToolsConfig: adk.ToolsConfig{
            ToolsNodeConfig: compose.ToolsNodeConfig{Tools: toolsList},
        },
        Handlers:    handlers,
        Instruction: complexAgentInstruction,
    })
    if err != nil {
        return nil, fmt.Errorf("failed to create complex agent: %w", err)
    }
    return agent, nil
}
```

- **MIRROR**: AGENT_CONSTRUCTION 模式
- **GOTCHA**: middleware 顺序影响执行链。`ApprovalMiddleware` 在内层（靠近工具），`SafeToolMiddleware` 在外层（兜底错误）。`adk.ChatModelAgentMiddleware` 的 Handlers 是从后往前包装还是从前往后，需查阅 Eino ADK 文档确认 —— 可用 `go doc github.com/cloudwego/eino/adk ChatModelAgentConfig Handlers` 验证。如果顺序相反，交换 `SafeToolMiddleware` 和 `ApprovalMiddleware` 的位置
- **VALIDATE**: `go build ./internal/logic/agent/dialogue/...`

---

### Task 6: 创建 graph.go — 编排图主体

（与原计划 Task 3 相同，核心逻辑不变。以下只列出与中断相关的修正点）

`drainAgentIterToStream` 中检测中断的部分：

```go
// 检测中断：将 ADK interrupt 转换为 compose 图层中断
// tool.StatefulInterrupt 产生的错误会通过 agent 的 compose 图传播到 event.Action.Interrupted
if event.Action != nil && event.Action.Interrupted != nil {
    // compose.StatefulInterrupt 将中断向上传播到外层图，同时保存 innerCPID 为本地状态
    // 这样恢复时 compose.GetInterruptState[string] 能取到 innerCPID
    interruptErr := compose.StatefulInterrupt(ctx, event.Action.Interrupted, innerCPID)
    sw.Send(nil, interruptErr)
    return
}
```

`safeToolMiddleware` 的 `compose.IsInterruptRerunError` 已确保中断错误穿透工具层到达 agent event 层，此处只需检测 `event.Action.Interrupted` 即可，**不需要**额外处理 `event.Err`（instrument 错误已被 safe middleware 转字符串）。

---

### Task 7: 修改 app.go — 替换 DialogueAgent

（与原计划 Task 4 相同，内容不变）

---

### Task 8: 修改 service.go — 使用编排图（含修正的中断检测 API）

与原计划 Task 5 基本相同，以下是**修正的关键部分**：

**修正 1 — 中断检测 API**：原计划用 `compose.ExtractInterruptInfo(err)`，实际应用 `compose.IsInterruptRerunError(err)`：

```go
// 在 ChatStream 的 stream.Recv() 循环中：
msg, recvErr := stream.Recv()
if recvErr == io.EOF {
    break
}
// 正确写法：compose.IsInterruptRerunError 返回 (interruptData any, ok bool)
if interruptData, ok := compose.IsInterruptRerunError(recvErr); ok {
    interrupted = true
    _, err := c.handleGraphInterrupt(r, checkpointID, interruptData)
    return &v1.ChatStreamRes{}, err
}
if recvErr != nil {
    writeSSEData(r, "[ERROR] "+recvErr.Error())
    return nil, nil
}
```

**修正 2 — handleGraphInterrupt 签名**：

```go
// handleGraphInterrupt 处理图层中断，构建 SSE interrupt payload 并推送。
// interruptData 是 compose.StatefulInterrupt 的第一个参数（我们传入的是 *adk.InterruptInfo）。
func (c *Service) handleGraphInterrupt(r *ghttp.Request, checkpointID string, interruptData any) (*v1.ChatStreamRes, error) {
    payload := map[string]any{
        "type":          "interrupt",
        "checkpoint_id": checkpointID,
    }
    // 尝试将 interruptData 断言为 *adk.InterruptInfo（compose.StatefulInterrupt 第一参数）
    if adkInterrupt, ok := interruptData.(*adk.InterruptInfo); ok {
        payload["interrupt_contexts"] = convertInterruptContexts(adkInterrupt.InterruptContexts)
        payload["message"] = buildInterruptMessage(adkInterrupt.Data)
        if structured := normalizeInterruptData(adkInterrupt.Data); structured != nil {
            payload["interrupt_data"] = structured
            if detailRequest := extractDetailSelectionPayload(structured); detailRequest != nil {
                payload["detail_request"] = detailRequest
            }
        }
    } else {
        payload["interrupt_contexts"] = []v1.InterruptContext{}
        payload["message"] = "流程已暂停，等待你的确认。"
    }
    payloadBytes, _ := json.Marshal(payload)
    writeSSEData(r, string(payloadBytes))
    writeSSEData(r, "[DONE]")
    return &v1.ChatStreamRes{}, nil
}
```

**修正 3 — ChatResumeStream 的中断检测**：同样用 `compose.IsInterruptRerunError`：
```go
if interruptData, ok := compose.IsInterruptRerunError(recvErr); ok {
    interrupted = true
    c.handleGraphInterrupt(r, req.CheckpointID, interruptData)
    return &v1.ChatResumeStreamRes{}, nil
}
```

其余内容（Service 结构体、NewService、ChatStream 主流程、ChatResumeStream WithStateModifier）与原计划 Task 5 相同。

---

### Task 9: 修改 cmd.go — 更新 NewService 调用参数

（与原计划 Task 6/7 相同，内容不变）

- **ACTION**: 在 `agent.go` 末尾添加以下内容
- **IMPLEMENT**:

```go
// OrchState 对话编排图的共享状态，跨节点传递 interrupt/resume 数据。
// 需要导出（首字母大写）供 service.go 跨包使用。
type OrchState struct {
    // InnerCheckpointID 是 complex_node 内部 adk.Runner 的 checkpoint ID。
    // 在首次运行时由 complex_node 生成并写入；中断恢复时用于调用 ResumeWithParams。
    InnerCheckpointID string
    // ResumeData 是 ChatResumeStream 通过 StateModifier 注入的审批/选择数据。
    // 格式与 adk.Runner 的 ResumeParams.Targets 的 value 一致（map[string]any）。
    ResumeData        map[string]any
    // ResumeInterruptIDs 是本次 resume 针对的 interrupt ID 列表。
    ResumeInterruptIDs []string
}

const gateAgentInstruction = `你是意图识别与知识库检索网关。

工作流程：
1. 分析用户问题，调用 knowledge_retrieve 检索知识库
2. 判断检索结果是否足以完整回答用户问题

输出规则（严格遵守）：
- 检索结果充足时：在最终回复开头包含 [RESOLVED]，并概述检索到的内容要点
- 检索结果不足、为空或无关时：在最终回复开头包含 [TO_COMPLEX]，说明为何需要进一步处理

你的回复仅用于内部路由判断，不是最终用户回答，请保持简洁。`

const answerAgentInstruction = `你是知识整理员。

你将收到包含知识库检索结果的对话上下文。请基于这些结果，用亲和、简洁的语气为用户提供完整答案。

要求：
- 使用 Markdown 格式，必要时用列表、表格、代码块
- 明确注明"来源：知识库检索结果"
- 检索内容不完整时，坦诚说明
- 优先用中文回复（除非用户使用其他语言）`

const complexAgentInstruction = `你是高级专家 Agent，处理需要专业技能和工具的复杂问题。

你将收到包含知识库检索结果（如果有）的完整对话上下文，请综合利用：
- 已有的 RAG 检索信息（如果有）
- 可用工具（知识检索、网络搜索、K8s 监控、指标查询、Bash 命令等）
- 专业 Skill 能力

工作原则：
- 涉及上传文档和内部资料时，优先 knowledge_retrieve
- 涉及最新信息时，调用 web_search
- 缺少关键上下文且可枚举时，使用 request_detail_selection 追问用户
- 执行 Bash 命令前，通过 bash_approval 获取用户确认
- 给出可执行的专业解答，明确信息来源`
```

- **MIRROR**: 现有 agent.go 中的多行字符串指令风格（见 Instruction: `` `...` `` 字段）
- **GOTCHA**: `OrchState` 必须导出（大写），字段必须可序列化（无 unexported 字段），否则 compose checkpoint 无法序列化
- **VALIDATE**: `go build ./internal/logic/agent/dialogue/...`


## Testing Strategy

### 手动验证矩阵

| 场景 | 预期路由 | 验证点 |
|---|---|---|
| 问"项目里有哪些文档" | answer_node | Gate 输出 [RESOLVED]，SSE 推送知识库整理内容 |
| 问"Kubernetes OOMKilled 如何排查" | complex_node | Gate 输出 [TO_COMPLEX]，SSE 推送工具调用结果 |
| Milvus 不可用 | complex_node | 服务正常启动，RAG 降级，路由走 complex |
| 触发 detail_selection（问题模糊） | complex_node interrupt | SSE 推送 `type:interrupt`，含 `detail_request` |
| 触发 bash_approval | complex_node interrupt | SSE 推送 `type:interrupt`，含 bash 命令信息 |
| Resume detail_selection | complex_node resume | 选择卡片选项后继续，SSE 推送最终回答 |

### Edge Cases Checklist

- [ ] Gate Agent 未调用 RAG 时（直接回答），路由回退到 complex_node
- [ ] outputMsgs 或 stream 为空时，service 不 panic
- [ ] `compose.ExtractInterruptInfo` 在非中断 error 上返回 false
- [ ] StateModifier 中 state 为 nil 时不 panic
- [ ] 同一 session 的多次请求 checkpoint ID 不冲突（每次生成新 uuid）

---

## Validation Commands

```bash
# 全量编译
cd /home/lhq/project/My_oncall/Back_part && go build ./...

# Vet 检查
go vet ./...

# 启动服务
go run main.go

# 基本对话测试
curl -N -X POST http://localhost:6872/api/v1/chat/stream \
  -H "Content-Type: application/json" \
  -d '{"id":"test-001","question":"你好"}'

# 知识库相关问题（期望 answer_node）
curl -N -X POST http://localhost:6872/api/v1/chat/stream \
  -H "Content-Type: application/json" \
  -d '{"id":"test-002","question":"有哪些关于告警处理的内部文档？"}'

# 复杂技术问题（期望 complex_node）
curl -N -X POST http://localhost:6872/api/v1/chat/stream \
  -H "Content-Type: application/json" \
  -d '{"id":"test-003","question":"帮我查一下 k8s 集群里所有 OOMKilled 的 pod"}'
```

---

## Acceptance Criteria

- [ ] `go build ./...` 零错误
- [ ] `go vet ./...` 零警告
- [ ] 编排图包含三个节点 + 一个路由分支
- [ ] RAG 命中时路由到 `answer_node`（日志可见 `gate_agent` 输出 `[RESOLVED]`）
- [ ] RAG 未命中时路由到 `complex_node`（日志可见 `gate_agent` 输出 `[TO_COMPLEX]`）
- [ ] SSE `content` 块正常推送（token 级别流式）
- [ ] SSE `[DONE]` 正常发送
- [ ] 输出内容不含 `[RESOLVED]` / `[TO_COMPLEX]` 标记
- [ ] interrupt 触发时 SSE 推送 `type:interrupt` JSON（含 `checkpoint_id`）
- [ ] `ChatResumeStream` 恢复后 SSE 继续推送内容并以 `[DONE]` 结束

## Completion Checklist

- [ ] 代码注释使用中文
- [ ] 所有 error 用 `fmt.Errorf("context: %w", err)` 包装
- [ ] `OrchState` 导出且字段可 JSON 序列化
- [ ] `schema.RegisterName[*OrchState]` 在 Compile 前调用
- [ ] `Application.DialogueAgent` 字段已删除（或注释说明废弃）
- [ ] `Service.chatStreamRunner`、`Service.rootAgentName` 字段已删除
- [ ] `cmd.go` 中 `NewService` 调用参数已更新

## Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| `compose.IsInterruptRerunError` 实际签名与计划不符 | Low | High | 实现前 `go doc github.com/cloudwego/eino/compose IsInterruptRerunError` 确认；example.go 已验证此函数存在 |
| Handlers 执行顺序（approvalMiddleware 与 safeToolMiddleware 先后）不符合预期 | Medium | Medium | `go doc github.com/cloudwego/eino/adk ChatModelAgentConfig Handlers` 确认包装顺序；通过集成测试快速验证 |
| `compose.StatefulInterrupt` 在 goroutine 内调用是否安全 | Low | High | example.go 的 approvalMiddleware 内部同样从 goroutine 路径调用；实测验证 |
| Gate Agent 输出格式不稳定（LLM 不总输出 [RESOLVED]） | High | Medium | `ragResultRouter` 有 fallback（检查 `schema.Tool` 消息内容）；大部分场景可兜底 |
| `OrchState` 类型断言失败（StateModifier 中 state 为 nil 或类型不同） | Low | Medium | 加 `if s, ok := state.(*OrchState); ok` 双检查，失败时 `logger.Warn` 跳过，不 panic |
| `complexAgent.ResumeWithParams` 是否可直接调用（不经 adk.Runner） | Medium | High | `adk.ResumableAgent` 接口定义了 `ResumeWithParams` 方法；若不可直接调用则改为通过 `complexRunner` |

## Notes

1. **中断传播链**（完整路径）：
   ```
   tool.StatefulInterrupt(ctx, info, args)         ← approvalMiddleware 触发
     → compose.IsInterruptRerunError = true         ← safeToolMiddleware 透传
       → agent 内部 compose 图 checkpoint 保存
         → event.Action.Interrupted != nil           ← complex_node Lambda 检测
           → compose.StatefulInterrupt(ctx, adkInfo, innerCPID)  ← 外层图中断
             → sw.Send(nil, interruptErr)
               → stream.Recv() 返回 interruptData    ← Service 层检测
                 → compose.IsInterruptRerunError(recvErr)
   ```

2. **Resume 数据传递链**：
   ```
   ChatResumeStreamReq.InterruptIDs + ApprovalData
     → graph.Stream(WithStateModifier sets OrchState.ResumeData + ResumeInterruptIDs)
       → complex_node Lambda: GetInterruptState[string] → innerCPID
         → complexAgent.ResumeWithParams(innerCPID, Targets{ic.ID: resumeData})
           → tool.GetResumeContext[map[string]any](ctx) ← approvalMiddleware 读取
   ```

3. **middleware Handlers 顺序**：按 Eino ADK 的包装语义，Handlers 列表中越靠前的越接近 LLM（内层），越靠后的越接近外部（外层）。建议顺序：`[summaryHandler, approvalMiddleware, safeToolMiddleware]`，这样 safe 最外层兜底，approval 在内层门控。如果 Eino 实际相反，交换 approval 和 safe 的位置。

4. **`complexRunner` 的实际用途**：目前 complex_node Lambda 直接调用 `complexAgent.Resume/ResumeWithParams`，不通过 `complexRunner`。但 `complexRunner` 持有同一个 `CheckPointStore`，使 inner checkpoint 能正确存入 Redis。若 `ResumableAgent` 的 `ResumeWithParams` 内部需要 runner 的上下文才能访问 checkpoint store，则改为通过 `complexRunner` 调用（两者 API 对称）。

5. **`DetailSelectionTool` 的 resume 构造**：middleware 在 resume 路径对 `request_detail_selection` 不调用 `endpoint`，直接从 `storedArgs` + `resumeData["selection_value"]` 构造 `DetailSelectionResult`。工具本体的纯校验路径（`InvokableRun`）在正常对话中不会被到达。
