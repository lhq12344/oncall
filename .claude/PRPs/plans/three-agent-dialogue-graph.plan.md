# Plan: 三 Agent 协同对话编排（Gate → Answer/Complex Graph）

## Summary

将现有单一 `DialogueAgent`（`adk.ResumableAgent`）拆分为三 Agent 协同模式，使用 `compose.NewGraph[[]*schema.Message, []*schema.Message]()` 编排：Gate Agent 负责意图识别 + RAG 检索，Answer Agent 负责在 RAG 充足时整理回复，Complex Agent 在 RAG 不足时使用 SkillMiddleware 处理复杂问题。Service 层调用编排图替代原来的 `adk.Runner`，SSE 流式输出方式保持不变。

## User Story

作为 OnCall 系统用户，我希望系统先检索知识库，若知识库能解决问题则直接整理回复，若不能再调用复杂 Agent（含专业 Skill 和 BashApproval 能力），这样可以节省 LLM Token、提升响应速度，并保证复杂问题的专业处理能力。

## Problem → Solution

**现状**：一个包含 5 个工具（intent_analysis、detail_selection、knowledge_retrieve、web_search、bash_approval）的单一 `adk.ResumableAgent`，无论问题复杂度，每次都经过相同的 ReAct 循环，可能浪费 Token，且调度逻辑不透明。

**目标**：通过 `compose.Graph` 明确路由，Gate Agent 先尝试 RAG 解答，命中则 Answer Agent 直接整理；未命中则 Complex Agent 使用完整工具集解决。

## Metadata

- **Complexity**: Large
- **Source PRD**: N/A
- **PRD Phase**: N/A
- **Estimated Files**: 4 修改 + 1 新建 = 5 文件

---

## UX Design

### Before

```
用户问题 → Gate(意图+RAG) → [RESOLVED] 或 [TO_COMPLEX]
              ↓ 单 Agent 处理所有情况
           全工具集 ReAct 循环 → SSE 流式输出
```

### After

```
用户问题 → gate_node(RAG+意图识别) → routerFunc → answer_node（RAG 充足）→ SSE
                                                 ↘ complex_node（RAG 不足） → SSE
```

内部变化（前端体验不变）：SSE 事件格式保持不变，`[DONE]` 结束标记保持不变，interrupt/resume 流程兼容现有实现。

### Interaction Changes

| Touchpoint | Before | After | Notes |
|---|---|---|---|
| SSE 内容输出 | 实时 token 流 | 先 Invoke 再推送 | 初始版本会有延迟感，可后续优化 |
| Interrupt 处理 | adk.Runner 负责 | Complex Agent 内部 interrupt，图层捕获 | BashApproval 中断仍需 resume |
| 前端 API 请求格式 | 不变 | 不变 | 零前端改动 |

---

## ⚠️ 重要设计决策（阅读前必看）

### 关于流式输出

`compose.Graph.Stream()` 返回的是 `*schema.StreamReader[[]*schema.Message]`，每个 chunk 是消息列表的增量，**不是 LLM token 级别的增量**。为保持前端流式体验，初始方案使用 `graph.Invoke()` + 事后按字符推送（类 typewriter）。后续优化可将叶节点改为 `StreamableLambda` 并通过 channel 推送。

### 关于 BashApprovalTool Interrupt/Resume

BashApprovalTool 通过 `compose.NewInterruptAndRerunErr()` 触发中断。在 `compose.Graph` 中，这个 error 会向上传播，被图层捕获（因为图使用了 `compose.WithCheckPointStore`）。**但 Graph 的 Resume 接口与 `adk.Runner.ResumeWithParams` 接口不同**——图使用 `compose.CallOption` 传递 resume 参数，不是 `adk.ResumeParams`。

**初始方案**：Complex Agent 仍使用独立的 `adk.ResumableAgent + adk.Runner`，Graph 路由后分派给 complex runner 执行。这样 interrupt/resume 路径零改动。Graph 仅用于 gate + 路由 + answer 三个节点。

详见 Task 4 的 GOTCHA。

---

## Mandatory Reading

必须在实现前阅读这些文件：

| Priority | File | Lines | Why |
|---|---|---|---|
| P0 (critical) | `Back_part/internal/logic/agent/dialogue/agent.go` | 1-219 | 现有 Agent 构建模式、Config 结构、noFormatGenModelInput |
| P0 (critical) | `Back_part/internal/logic/chat/service.go` | 31-91 | Service 结构体、NewService、chatStreamRunner 创建 |
| P0 (critical) | `Back_part/internal/logic/chat/service.go` | 141-251 | ChatStream 主循环、事件处理、SSE 推送逻辑 |
| P1 (important) | `Back_part/internal/logic/agent/knowledge/orchestration.go` | 1-57 | 项目中唯一使用 compose.Graph 的示例 |
| P1 (important) | `Back_part/internal/logic/app/app.go` | 73-149 | Application 初始化、DialogueAgent 创建入口 |
| P1 (important) | `Back_part/internal/logic/agent/dialogue/tools/KnowledgeRetrieveTool.go` | all | RAG 工具的输出格式（router 依赖此） |
| P2 (reference) | `Back_part/internal/logic/session/session_memory.go` | all | BuildMessages、SaveTurnWithSource 接口 |
| P2 (reference) | `Back_part/internal/logic/agent/dialogue/tools/BashApprovalTool.go` | all | Interrupt 触发方式 |

## External Documentation

| Topic | Source | Key Takeaway |
|---|---|---|
| compose.Graph | `/home/lhq/.codex/skills/eino-compose/SKILL.md` | AddLambdaNode、AddBranch、Compile 用法 |
| compose.Graph Branch | `/home/lhq/.codex/skills/eino-compose/reference/graph.md` | `compose.NewGraphBranch(conditionFn, map[string]bool{})` |
| adk.ChatModelAgent | `/home/lhq/.codex/skills/eino-agent/SKILL.md` | Middleware、ToolsConfig、Handlers 字段 |

---

## Patterns to Mirror

### NAMING_CONVENTION
```go
// SOURCE: Back_part/internal/logic/agent/dialogue/agent.go:49-50
// 函数名：NewXxxAgent；Config 结构体：Config（包内唯一）或 XxxConfig
func NewDialogueAgent(ctx context.Context, cfg *Config) (adk.ResumableAgent, error) {
```

### ERROR_HANDLING
```go
// SOURCE: Back_part/internal/logic/agent/dialogue/agent.go:82-84
summaryHandler, err := summarization.New(ctx, summaryConfig)
if err != nil {
    return nil, fmt.Errorf("failed to create dialogue summarization middleware: %w", err)
}
```

### LOGGER_PATTERN
```go
// SOURCE: Back_part/internal/logic/agent/dialogue/agent.go:63-67
if cfg.Logger != nil {
    cfg.Logger.Warn("failed to initialize milvus retriever for dialogue agent, fallback to degraded mode",
        zap.Error(err))
}
```

### AGENT_CONSTRUCTION
```go
// SOURCE: Back_part/internal/logic/agent/dialogue/agent.go:95-128
agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    Name:          "dialogue_agent",
    Description:   "...",
    Model:         cfg.ChatModel.Client,
    GenModelInput: noFormatGenModelInput,
    ToolsConfig: adk.ToolsConfig{
        ToolsNodeConfig: compose.ToolsNodeConfig{
            Tools: toolsList,
        },
    },
    Handlers:    handlers,
    Instruction: `...`,
})
```

### COMPOSE_GRAPH_PATTERN
```go
// SOURCE: Back_part/internal/logic/agent/knowledge/orchestration.go:33-56
g := compose.NewGraph[document.Source, []string]()
if err := g.AddLoaderNode("file_loader", fileLoader); err != nil {
    return nil, err
}
if err := g.AddEdge(compose.START, "file_loader"); err != nil {
    return nil, err
}
return g.Compile(ctx, compose.WithGraphName("knowledge_indexing"))
```

### BRANCH_PATTERN（参考 eino-compose skill）
```go
// SOURCE: /home/lhq/.codex/skills/eino-compose/reference/graph.md
condition := func(ctx context.Context, in OutputType) (string, error) {
    if shouldGoLeft(in) {
        return "left", nil
    }
    return "right", nil
}
endNodes := map[string]bool{"left": true, "right": true}
branch := compose.NewGraphBranch(condition, endNodes)
g.AddBranch("source_node", branch)
```

### LAMBDA_NODE_PATTERN（参考 eino-compose skill）
```go
// SOURCE: /home/lhq/.codex/skills/eino-compose/SKILL.md
g.AddLambdaNode("fn", compose.InvokableLambda(myFunc))
// myFunc signature: func(ctx context.Context, in InputType) (OutputType, error)
```

### SSE_PUSH_PATTERN
```go
// SOURCE: Back_part/internal/logic/chat/service.go:222-224
fullAnswer.WriteString(chunk)
contentChunkCount++
writeSSEData(r, chunk)
```

---

## Files to Change

| File | Action | Justification |
|---|---|---|
| `Back_part/internal/logic/agent/dialogue/graph.go` | CREATE | 三 Agent 编排图的构建函数 |
| `Back_part/internal/logic/agent/dialogue/agent.go` | UPDATE | 添加 newGateAgent、newAnswerAgent、newComplexAgent；OrchestrationConfig 结构体 |
| `Back_part/internal/logic/app/app.go` | UPDATE | 将 dialogueAgent 从单 Agent 改为调用 BuildOrchestrationGraph |
| `Back_part/internal/logic/chat/service.go` | UPDATE | Service 结构体新增 graphRunnable 字段；ChatStream 使用 graph 而非 runner |

**不需要改动**：
- `tools/` 目录下所有工具（完全复用）
- `session/` 目录（SessionMemory 接口不变）
- `ai/` 目录（ChatModel、Retriever 复用）
- 前端代码
- API 定义文件

## NOT Building

- 完整的图级 interrupt/resume（Complex Agent 仍使用现有 `adk.Runner` 处理 BashApproval）
- LLM token 级别的实时流式输出（初始版本使用 Invoke + 事后字符串推送）
- 新的工具（所有工具完全复用现有实现）
- 对话状态持久化跨 Agent 节点（各 Agent 通过 messages 传递上下文）
- 多轮 Gate → Complex → Gate 循环（单次路由，不支持回流）

---

## Step-by-Step Tasks

### Task 1: 定义 OrchestrationConfig 并添加三个 Agent 构建函数

- **ACTION**: 在 `agent.go` 中添加 `OrchestrationConfig` 结构体和 `newGateAgent`、`newAnswerAgent`、`newComplexAgent` 工厂函数
- **IMPLEMENT**:
  ```go
  // OrchestrationConfig 三 Agent 编排配置（复用 Config 所有字段）
  type OrchestrationConfig = Config // 别名，保持兼容性
  
  // newGateAgent 创建 Gate Agent（意图识别 + RAG 检索）。
  // 仅挂载 KnowledgeRetrieveTool，输出中标记 [RESOLVED] 或 [TO_COMPLEX]。
  func newGateAgent(ctx context.Context, cfg *Config, retriever einoretriever.Retriever) (adk.Agent, error) {
      agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
          Name:          "gate_agent",
          Description:   "意图识别与知识库检索网关，负责判断 RAG 是否能解决问题",
          Model:         cfg.ChatModel.Client,
          GenModelInput: noFormatGenModelInput,
          ToolsConfig: adk.ToolsConfig{
              ToolsNodeConfig: compose.ToolsNodeConfig{
                  Tools: []tool.BaseTool{
                      tools.NewKnowledgeRetrieveTool(retriever, cfg.Logger),
                  },
              },
          },
          Instruction: gateAgentInstruction,
      })
      if err != nil {
          return nil, fmt.Errorf("failed to create gate agent: %w", err)
      }
      return agent, nil
  }
  
  // newAnswerAgent 创建 Answer Agent（整理 RAG 结果回复用户）。
  // 无工具，仅整理已有上下文。
  func newAnswerAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
      agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
          Name:          "answer_agent",
          Description:   "知识整理回复 Agent，负责将 RAG 检索结果整理为友好回答",
          Model:         cfg.ChatModel.Client,
          GenModelInput: noFormatGenModelInput,
          Instruction:   answerAgentInstruction,
      })
      if err != nil {
          return nil, fmt.Errorf("failed to create answer agent: %w", err)
      }
      return agent, nil
  }
  
  // newComplexAgent 创建 Complex Agent（复杂问题处理，含全工具集 + Skill 中间件）。
  // 挂载与原 DialogueAgent 相同的工具集和中间件。
  func newComplexAgent(ctx context.Context, cfg *Config, retriever einoretriever.Retriever) (adk.ResumableAgent, error) {
      toolsList := buildDialogueTools(cfg, retriever) // 复用现有函数
      
      summaryHandler, err := summarization.New(ctx, &summarization.Config{
          Model: cfg.ChatModel.Client,
          Trigger: &summarization.TriggerCondition{ContextTokens: 300000},
      })
      if err != nil {
          return nil, fmt.Errorf("failed to create complex agent summarization: %w", err)
      }
      
      handlers := []adk.ChatModelAgentMiddleware{summaryHandler}
      skillHandler, err := newDialogueSkillMiddleware(ctx, cfg.SkillsDir, cfg.Logger)
      if err != nil {
          return nil, fmt.Errorf("failed to create complex agent skill middleware: %w", err)
      }
      if skillHandler != nil {
          handlers = append(handlers, skillHandler)
      }
      
      agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
          Name:          "complex_agent",
          Description:   "高级专家 Agent，处理 RAG 无法解答的复杂问题",
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
- **IMPORTS**: 与现有 agent.go 相同；`tool.BaseTool` 已存在
- **GOTCHA**: `newComplexAgent` 返回 `adk.ResumableAgent`（不是 `adk.Agent`），因为 Complex Agent 需要 interrupt/resume 能力（BashApprovalTool）
- **VALIDATE**: `go build ./internal/logic/agent/dialogue/...` 无编译错误

### Task 2: 添加 Agent 指令常量

- **ACTION**: 在 `agent.go` 中添加三个 Agent 的指令常量
- **IMPLEMENT**:
  ```go
  const gateAgentInstruction = `你是意图识别与知识库检索网关。
  
  工作流程：
  1. 分析用户问题意图
  2. 调用 knowledge_retrieve 工具检索知识库
  3. 判断检索结果是否足以完整回答用户问题
  
  输出规则（非常重要）：
  - 如果检索结果充足，在你的最终回复中包含标记 [RESOLVED]，并简要说明检索到了什么
  - 如果检索结果不足、为空或与问题无关，在你的最终回复中包含标记 [TO_COMPLEX]，并说明为什么需要进一步处理
  
  注意：你的回复仅用于路由判断，不是最终用户回答。`
  
  const answerAgentInstruction = `你是一个知识整理员。
  
  你将收到包含知识库检索结果的对话上下文（已由检索 Agent 获取）。
  请基于这些检索结果，用亲和、简洁的语气为用户提供完整答案。
  
  要求：
  - 使用 Markdown 格式组织内容
  - 明确标注"来源：知识库检索结果"
  - 如果检索内容不完整，坦诚说明并提供力所能及的回答
  - 用中文回复，除非用户使用其他语言`
  
  const complexAgentInstruction = `你是一个高级专家 Agent，处理需要专业技能和工具的复杂问题。
  
  你将收到包含知识库检索结果（如果有）的完整对话上下文。请综合利用：
  - 已有的 RAG 检索结果（如果有）
  - 你的专业 Skill 能力
  - 可用工具（知识检索、网络搜索、Bash 命令等）
  
  解决用户面临的复杂难题，给出可执行的专业解答。
  
  对于需要执行 Bash 命令的操作，请先获得用户确认再执行（BashApprovalTool 会自动处理）。`
  ```
- **MIRROR**: 现有 `Instruction` 字段（见 agent.go:106-127）的注释风格和多行字符串写法
- **GOTCHA**: 指令中的 `[RESOLVED]` / `[TO_COMPLEX]` 标记必须与 Task 4 的路由函数一致
- **VALIDATE**: `go vet ./internal/logic/agent/dialogue/...` 通过

### Task 3: 创建 graph.go — 三 Agent 编排图

- **ACTION**: 新建 `Back_part/internal/logic/agent/dialogue/graph.go`
- **IMPLEMENT**:
  ```go
  package dialogue
  
  import (
      "context"
      "fmt"
      "strings"
  
      "github.com/cloudwego/eino/adk"
      "github.com/cloudwego/eino/compose"
      "github.com/cloudwego/eino/schema"
      airetriever "go_agent/internal/logic/ai/retriever"
  )
  
  // BuildOrchestrationGraph 构建三 Agent 协同对话编排图。
  //
  // 图拓扑：
  //   START → gate_node → [routerFunc] → answer_node → END
  //                                    ↘ complex_node → END
  //
  // 输入/输出类型均为 []*schema.Message（会话消息列表）。
  // gate_node 追加 RAG 检索结果消息后，路由函数检查是否命中，
  // 命中则由 answer_node 整理回复，否则由 complex_node 使用完整工具集处理。
  //
  // 注意：复杂路径（complex_node）的 BashApproval interrupt/resume 通过调用方
  // 持有的 complexRunner 处理，不经由图层。参见 Service.complexRunner 字段。
  func BuildOrchestrationGraph(ctx context.Context, cfg *Config) (compose.Runnable[[]*schema.Message, []*schema.Message], adk.ResumableAgent, error) {
      if cfg == nil {
          return nil, nil, fmt.Errorf("config is required")
      }
  
      // 初始化 Milvus 检索器（允许降级）
      retrieverCtx, cancel := context.WithTimeout(ctx, milvusRetrieverInitTimeout)
      defer cancel()
      knowledgeRetriever, err := airetriever.NewMilvusRetriever(retrieverCtx)
      if err != nil {
          if cfg.Logger != nil {
              cfg.Logger.Warn("milvus retriever unavailable for orchestration graph, RAG degraded",
                  zap.Error(err))
          }
          knowledgeRetriever = nil
      }
  
      // 创建三个 Agent
      gateAgent, err := newGateAgent(ctx, cfg, knowledgeRetriever)
      if err != nil {
          return nil, nil, fmt.Errorf("failed to build gate agent: %w", err)
      }
      answerAgent, err := newAnswerAgent(ctx, cfg)
      if err != nil {
          return nil, nil, fmt.Errorf("failed to build answer agent: %w", err)
      }
      complexAgent, err := newComplexAgent(ctx, cfg, knowledgeRetriever)
      if err != nil {
          return nil, nil, fmt.Errorf("failed to build complex agent: %w", err)
      }
  
      // 构建图
      g := compose.NewGraph[[]*schema.Message, []*schema.Message]()
  
      // gate_node：运行 Gate Agent，收集所有输出消息
      if err := g.AddLambdaNode("gate_node", compose.InvokableLambda(
          func(ctx context.Context, msgs []*schema.Message) ([]*schema.Message, error) {
              return collectAgentMessages(ctx, gateAgent, msgs)
          },
      )); err != nil {
          return nil, nil, fmt.Errorf("failed to add gate_node: %w", err)
      }
  
      // answer_node：运行 Answer Agent，整理 RAG 结果
      if err := g.AddLambdaNode("answer_node", compose.InvokableLambda(
          func(ctx context.Context, msgs []*schema.Message) ([]*schema.Message, error) {
              return collectAgentMessages(ctx, answerAgent, msgs)
          },
      )); err != nil {
          return nil, nil, fmt.Errorf("failed to add answer_node: %w", err)
      }
  
      // complex_node：直接透传消息，实际执行由 Service 层的 complexRunner 完成
      // 这里透传是为了保持图结构完整；Service 检测到路由为 complex_node 后
      // 会从图的输出中识别路由决策并切换到 complexRunner。
      // 简化版：complex_node 也直接调用 complexAgent（不支持 interrupt）
      if err := g.AddLambdaNode("complex_node", compose.InvokableLambda(
          func(ctx context.Context, msgs []*schema.Message) ([]*schema.Message, error) {
              return collectAgentMessages(ctx, complexAgent, msgs)
          },
      )); err != nil {
          return nil, nil, fmt.Errorf("failed to add complex_node: %w", err)
      }
  
      // 边
      if err := g.AddEdge(compose.START, "gate_node"); err != nil {
          return nil, nil, fmt.Errorf("failed to add START→gate edge: %w", err)
      }
      if err := g.AddEdge("answer_node", compose.END); err != nil {
          return nil, nil, fmt.Errorf("failed to add answer→END edge: %w", err)
      }
      if err := g.AddEdge("complex_node", compose.END); err != nil {
          return nil, nil, fmt.Errorf("failed to add complex→END edge: %w", err)
      }
  
      // 分支：gate_node → answer_node 或 complex_node
      branch := compose.NewGraphBranch(
          ragResultRouter,
          map[string]bool{"answer_node": true, "complex_node": true},
      )
      if err := g.AddBranch("gate_node", branch); err != nil {
          return nil, nil, fmt.Errorf("failed to add router branch: %w", err)
      }
  
      runnable, err := g.Compile(ctx, compose.WithGraphName("dialogue_orchestration"))
      if err != nil {
          return nil, nil, fmt.Errorf("failed to compile orchestration graph: %w", err)
      }
  
      // 返回 complexAgent 供 Service 层用于 interrupt/resume 路径（备用）
      return runnable, complexAgent, nil
  }
  
  // collectAgentMessages 同步运行 Agent，收集所有输出消息（含工具调用消息）。
  // 输入：ctx、agent、输入消息列表。
  // 输出：输入消息 + Agent 产生的新消息。
  func collectAgentMessages(ctx context.Context, agent adk.Agent, inputMsgs []*schema.Message) ([]*schema.Message, error) {
      iter := agent.Run(ctx, &adk.AgentInput{
          Messages:       inputMsgs,
          EnableStreaming: false,
      })
      
      result := make([]*schema.Message, len(inputMsgs))
      copy(result, inputMsgs)
      
      for {
          event, ok := iter.Next()
          if !ok {
              break
          }
          if event == nil {
              continue
          }
          if event.Err != nil {
              return nil, fmt.Errorf("agent %s error: %w", agent.Name(ctx), event.Err)
          }
          if event.Output != nil && event.Output.MessageOutput != nil {
              msg, err := event.Output.MessageOutput.GetMessage()
              if err == nil && msg != nil {
                  result = append(result, msg)
              }
          }
      }
      return result, nil
  }
  
  // ragResultRouter 路由函数：根据 Gate Agent 的输出消息决定分流方向。
  //
  // 路由逻辑：
  // 1. 优先检查 Gate Agent 最终助手消息中是否包含 [RESOLVED] 标记
  // 2. 其次检查是否存在有效的 knowledge_retrieve 工具结果（非空、非"未找到"）
  // 3. 两者均不满足则路由到 complex_node
  func ragResultRouter(ctx context.Context, msgs []*schema.Message) (string, error) {
      // 从后往前查找 Gate Agent 的最终助手消息
      for i := len(msgs) - 1; i >= 0; i-- {
          msg := msgs[i]
          if msg.Role == schema.Assistant && strings.TrimSpace(msg.ToolCallID) == "" {
              content := strings.TrimSpace(msg.Content)
              if strings.Contains(content, "[RESOLVED]") {
                  return "answer_node", nil
              }
              if strings.Contains(content, "[TO_COMPLEX]") {
                  return "complex_node", nil
              }
              break
          }
      }
      
      // 回退：检查工具结果消息内容
      for i := len(msgs) - 1; i >= 0; i-- {
          msg := msgs[i]
          if msg.Role == schema.Tool {
              content := strings.TrimSpace(msg.Content)
              if content == "" || isEmptyRAGResult(content) {
                  return "complex_node", nil
              }
              return "answer_node", nil
          }
      }
      
      // 无工具调用记录（Gate 未调用 RAG），转交 complex
      return "complex_node", nil
  }
  
  // isEmptyRAGResult 判断 RAG 工具结果是否为空或无效。
  func isEmptyRAGResult(content string) bool {
      lower := strings.ToLower(content)
      return strings.Contains(lower, "未找到") ||
          strings.Contains(lower, "no results") ||
          strings.Contains(lower, "没有找到") ||
          strings.Contains(lower, "检索结果为空") ||
          len([]rune(content)) < 10
  }
  ```
- **MIRROR**: COMPOSE_GRAPH_PATTERN、BRANCH_PATTERN、LAMBDA_NODE_PATTERN
- **IMPORTS**:
  ```go
  import (
      "context"
      "fmt"
      "strings"
  
      "go_agent/internal/logic/ai/retriever"
      
      "github.com/cloudwego/eino/adk"
      "github.com/cloudwego/eino/compose"
      "github.com/cloudwego/eino/schema"
      "go.uber.org/zap"
  )
  ```
- **GOTCHA 1**: `compose.NewGraph` 的泛型参数 `[[]*schema.Message, []*schema.Message]` 要求所有边的类型完全一致。Gate、Answer、Complex 节点的输入输出都必须是 `[]*schema.Message`。
- **GOTCHA 2**: `agent.Run()` 直接调用（不经 Runner），不支持 checkpoint。BashApprovalTool 的 interrupt 会通过 `event.Err` 或 `event.Action.Interrupted` 传出——在 `collectAgentMessages` 中需要检测并传播。见 Task 4 的处理。
- **GOTCHA 3**: `GetMessage()` 对于 streaming MessageOutput 会阻塞到流完成。由于我们传 `EnableStreaming: false`，这是安全的。
- **VALIDATE**: `go build ./internal/logic/agent/dialogue/...` 通过

### Task 4: 处理 Complex Agent 的 Interrupt 传播

- **ACTION**: 修改 `collectAgentMessages` 使其能感知并传播 interrupt 事件；在 Service 层特殊处理 complex 路径
- **IMPLEMENT**:

  **方案 A（简化，推荐初始实现）**: 在 `graph.go` 中添加专用 interrupt 错误类型：

  ```go
  // interruptErr 封装 Complex Agent 中断信息，用于从图层传播到 Service 层。
  type interruptErr struct {
      info     *adk.InterruptInfo
      agentName string
  }
  
  func (e *interruptErr) Error() string {
      return fmt.Sprintf("agent %s interrupted", e.agentName)
  }
  
  // collectAgentMessagesOrInterrupt 同 collectAgentMessages，但在检测到 interrupt 时
  // 返回特殊错误（interruptErr）供调用方识别和处理。
  func collectAgentMessagesOrInterrupt(ctx context.Context, agent adk.Agent, inputMsgs []*schema.Message) ([]*schema.Message, error) {
      iter := agent.Run(ctx, &adk.AgentInput{
          Messages:        inputMsgs,
          EnableStreaming: false,
      })
      
      result := make([]*schema.Message, len(inputMsgs))
      copy(result, inputMsgs)
      
      for {
          event, ok := iter.Next()
          if !ok {
              break
          }
          if event == nil {
              continue
          }
          if event.Err != nil {
              return nil, fmt.Errorf("agent %s error: %w", agent.Name(ctx), event.Err)
          }
          // 检测中断
          if event.Action != nil && event.Action.Interrupted != nil {
              return nil, &interruptErr{
                  info:      event.Action.Interrupted,
                  agentName: event.AgentName,
              }
          }
          if event.Output != nil && event.Output.MessageOutput != nil {
              msg, err := event.Output.MessageOutput.GetMessage()
              if err == nil && msg != nil {
                  result = append(result, msg)
              }
          }
      }
      return result, nil
  }
  ```

  **方案 B（完整，后续优化）**: Complex Agent 使用独立 `adk.Runner`，Service 层持有 `complexRunner *adk.Runner`，路由后绕过图层直接用 runner 处理（interrupt/resume 完全兼容现有实现）。

  **初始实现选择方案 A**：接受 interrupt 时返回 error，Service 层检测 `interruptErr` 并通过 SSE 推送 interrupt 事件，resume 通过重新调用 complex_node 实现（简化的单次 resume）。

- **MIRROR**: 现有 `buildInterruptPayload` 和 SSE_PUSH_PATTERN
- **GOTCHA**: 方案 A 的 resume 不支持多轮审批（BashApprovalTool 的 checkpoint 语义丢失）。如果项目大量依赖 BashApproval，需要切换到方案 B。
- **VALIDATE**: 手动测试包含 bash 命令的请求是否能正确触发 interrupt 事件

### Task 5: 修改 app.go — 使用编排图替代单 Agent

- **ACTION**: 修改 `NewApplication` 函数，调用 `BuildOrchestrationGraph` 替代 `dialogue.NewDialogueAgent`
- **IMPLEMENT**:
  ```go
  // 替换 app.go:117-128 的 DialogueAgent 初始化
  
  // 5. 初始化三 Agent 对话编排图
  logger.Info("initializing three-agent dialogue orchestration graph")
  orchGraph, complexAgent, err := dialogue.BuildOrchestrationGraph(ctx, &dialogue.Config{
      ChatModel: chatModel,
      Embedder:  dialogueEmbedder,
      SkillsDir: os.Getenv("EINO_EXT_SKILLS_DIR"),
      Logger:    logger,
  })
  if err != nil {
      return nil, fmt.Errorf("failed to build dialogue orchestration graph: %w", err)
  }
  logger.Info("three-agent dialogue orchestration graph initialized")
  ```

  同时修改 `Application` 结构体：
  ```go
  type Application struct {
      OrchestrationGraph compose.Runnable[[]*schema.Message, []*schema.Message]
      ComplexAgent       adk.ResumableAgent  // 用于 interrupt/resume 路径
      KnowledgeAgent     adk.Agent
      Logger             *zap.Logger
      RedisClient        *redis.Client
  }
  ```

  **注意**：删除原有 `DialogueAgent adk.ResumableAgent` 字段，替换为 `OrchestrationGraph` 和 `ComplexAgent`。
- **MIRROR**: ERROR_HANDLING、LOGGER_PATTERN
- **IMPORTS**: 新增 `"github.com/cloudwego/eino/compose"` 和 `"github.com/cloudwego/eino/schema"`
- **GOTCHA**: `cmd.go` 中调用 `NewService` 的参数需要同步更新（`dialogueAgent` → `orchGraph` + `complexAgent`）
- **VALIDATE**: `go build ./internal/logic/app/...` 通过

### Task 6: 修改 service.go — 使用编排图执行对话

- **ACTION**: 修改 `Service` 结构体、`NewService`、`ChatStream` 三处以使用编排图
- **IMPLEMENT**:

  **Service 结构体**（替换 `dialogueAgent` 和 `chatStreamRunner`）：
  ```go
  type Service struct {
      orchGraph      compose.Runnable[[]*schema.Message, []*schema.Message]
      complexRunner  *adk.Runner   // 用于 interrupt/resume 的 complex 路径（方案 B 预留）
      rootAgentName  string
      sessionMemory  *appcontext.SessionMemory
      logger         *zap.Logger
      knowledgeAgent adk.Agent
  }
  ```

  **NewService**（替换 `adk.Runner` 创建逻辑）：
  ```go
  func NewService(
      orchGraph compose.Runnable[[]*schema.Message, []*schema.Message],
      complexAgent adk.ResumableAgent,
      logger *zap.Logger,
      redisClient *redis.Client,
      knowledgeAgent adk.Agent,
  ) *Service {
      ctrl := &Service{
          orchGraph:      orchGraph,
          rootAgentName:  "dialogue_orchestration",
          sessionMemory:  appcontext.NewSessionMemory(nil, logger),
          logger:         logger,
          knowledgeAgent: knowledgeAgent,
      }
  
      // 为 Complex Agent 创建独立 Runner（interrupt/resume 支持）
      if complexAgent != nil {
          var checkpointStore compose.CheckPointStore
          if redisClient != nil {
              checkpointStore = appcontext.NewRedisCheckPointStore(redisClient, "oncall", 24*time.Hour)
          } else {
              checkpointStore = newInMemoryCheckPointStore()
          }
          ctrl.complexRunner = adk.NewRunner(context.Background(), adk.RunnerConfig{
              Agent:           complexAgent,
              EnableStreaming:  true,
              CheckPointStore: checkpointStore,
          })
      }
  
      return ctrl
  }
  ```

  **ChatStream** 核心改动（替换 `c.chatStreamRunner.Run()` 调用）：
  ```go
  func (c *Service) ChatStream(ctx context.Context, req *v1.ChatStreamReq) (res *v1.ChatStreamRes, err error) {
      question, sessionID, err := validateChatStreamInput(req)
      if err != nil {
          return nil, err
      }
      if c.orchGraph == nil {
          return nil, fmt.Errorf("orchestration graph is not initialized")
      }
  
      r, err := setupSSE(ctx)
      if err != nil {
          return nil, err
      }
  
      messages, err := c.sessionMemory.BuildMessages(ctx, sessionID, question)
      if err != nil {
          return nil, err
      }
  
      checkpointID := generateCheckpointID(sessionID)
      if c.logger != nil {
          c.logger.Info("chat_stream orchestration started",
              zap.String("session_id", sessionID),
              zap.Int("question_len", len([]rune(question))),
              zap.String("checkpoint_id", checkpointID))
      }
  
      // 调用编排图（批量模式，图内部自行路由）
      outputMsgs, err := c.orchGraph.Invoke(ctx, messages)
      if err != nil {
          // 检查是否为 Complex Agent 的 interrupt
          var intErr *dialogue.InterruptErr  // 注意：需要将 interruptErr 导出为 InterruptErr
          if errors.As(err, &intErr) {
              payload := buildInterruptPayload(checkpointID, intErr.Info())
              payloadBytes, _ := json.Marshal(payload)
              writeSSEData(r, string(payloadBytes))
              writeSSEData(r, "[DONE]")
              return &v1.ChatStreamRes{}, nil
          }
          writeSSEData(r, "[ERROR] "+err.Error())
          return nil, nil
      }
  
      // 从输出消息中提取最终助手回复
      answer := extractFinalAssistantContent(outputMsgs)
      
      // 推送答案到 SSE（按字符块推送，模拟流式效果）
      if answer != "" {
          writeSSEData(r, answer)
      }
      writeSSEData(r, "[DONE]")
  
      if answer != "" {
          c.sessionMemory.SaveTurnWithSource(context.Background(), sessionID, question, answer, nil, messages, "chat_stream_graph")
      }
  
      return &v1.ChatStreamRes{}, nil
  }
  
  // extractFinalAssistantContent 从编排图输出消息列表中提取最终助手回复。
  // 从后往前查找最后一条无 ToolCallID 的 assistant 消息。
  func extractFinalAssistantContent(msgs []*schema.Message) string {
      for i := len(msgs) - 1; i >= 0; i-- {
          msg := msgs[i]
          if msg.Role == schema.Assistant && strings.TrimSpace(msg.ToolCallID) == "" {
              content := sanitizeUserFacingContent(msg.Content)
              if content != "" {
                  return content
              }
          }
      }
      return ""
  }
  ```

- **MIRROR**: SSE_PUSH_PATTERN、ERROR_HANDLING
- **IMPORTS**: 新增 `"errors"`；`"go_agent/internal/logic/agent/dialogue"` 用于 `InterruptErr`
- **GOTCHA 1**: `ChatResumeStream` 仍使用 `complexRunner`（通过 `c.resumeAgent(ctx, c.complexRunner, ...)`），无需改动。确认 `Service.complexRunner` 字段不为 nil 时才调用。
- **GOTCHA 2**: `extractFinalAssistantContent` 需要过滤 `[RESOLVED]`/`[TO_COMPLEX]` 等路由标记，这些由 Gate Agent 输出，不应显示给用户。补充过滤逻辑：
  ```go
  // 过滤路由标记
  content = strings.ReplaceAll(content, "[RESOLVED]", "")
  content = strings.ReplaceAll(content, "[TO_COMPLEX]", "")
  content = strings.TrimSpace(content)
  ```
- **GOTCHA 3**: 原 `ChatStream` 中的 `c.chatStreamRunner == nil` 检查需改为 `c.orchGraph == nil`
- **VALIDATE**: 启动服务，发送一个简单问题，观察 SSE 输出是否正常

### Task 7: 修改 cmd.go — 更新 NewService 调用参数

- **ACTION**: 找到 `cmd.go` 中调用 `chat.NewService` 的位置，更新参数
- **IMPLEMENT**:
  ```go
  // 找到原来的调用（类似）：
  // chatService := chat.NewService(app.DialogueAgent, logger, redisClient, app.KnowledgeAgent)
  // 替换为：
  chatService := chat.NewService(
      app.OrchestrationGraph,
      app.ComplexAgent,
      logger,
      redisClient,
      app.KnowledgeAgent,
  )
  ```
- **MIRROR**: ERROR_HANDLING
- **GOTCHA**: 用 `grep -n "NewService\|DialogueAgent" Back_part/internal/cmd/cmd.go` 精确定位行号
- **VALIDATE**: `go build ./...` 全量编译通过

---

## Testing Strategy

### Unit Tests

由于项目当前无测试文件（`internal/logic/agent/dialogue/` 和 `internal/logic/chat/` 均无测试），本次以集成验证为主。

| Test | Input | Expected Output | Edge Case? |
|---|---|---|---|
| RAG 命中路由 | 包含内部文档相关问题 | answer_node 执行，SSE 推送整理后的知识库内容 | N |
| RAG 未命中路由 | 复杂技术问题（无内部文档） | complex_node 执行，使用全工具集 | N |
| 空 RAG 结果 | 完全无关的问题 | 路由到 complex_node | Y |
| Milvus 不可用 | 任意问题 | 降级到 complex_node（RAG 返回空） | Y |

### Edge Cases Checklist

- [ ] Milvus retriever 初始化失败时，Gate Agent 仍能正常工作（无 RAG 工具 or RAG 工具返回错误）
- [ ] Gate Agent 输出既不包含 `[RESOLVED]` 也不包含 `[TO_COMPLEX]` 时，路由回退逻辑正确工作
- [ ] outputMsgs 为空时，`extractFinalAssistantContent` 返回空字符串（不 panic）
- [ ] Complex Agent 因 BashApprovalTool 触发 interrupt 时，SSE 正确推送 interrupt 事件

---

## Validation Commands

### 静态检查

```bash
cd /home/lhq/project/My_oncall/Back_part
go build ./...
```
EXPECT: 零编译错误

```bash
go vet ./...
```
EXPECT: 零 vet 警告

### 运行测试

```bash
go test ./internal/logic/agent/dialogue/... -v
go test ./internal/logic/chat/... -v
```
EXPECT: 所有测试通过（或 no test files 提示）

### 手动验证（启动服务后）

```bash
# 启动服务
cd /home/lhq/project/My_oncall/Back_part && go run main.go
```

```bash
# 测试 1：普通问题（RAG 可能命中）
curl -N -X POST http://localhost:6872/api/v1/chat/stream \
  -H "Content-Type: application/json" \
  -d '{"id":"test-session","question":"有哪些内部文档可以参考？"}' 2>&1

# 期望：SSE 流式输出，包含 [DONE] 结束标记
```

```bash
# 测试 2：复杂技术问题（RAG 不应命中）
curl -N -X POST http://localhost:6872/api/v1/chat/stream \
  -H "Content-Type: application/json" \
  -d '{"id":"test-session","question":"Kubernetes pod 无法启动，OOMKilled，如何排查？"}' 2>&1

# 期望：SSE 输出包含技术性排查建议（由 complex_node 处理）
```

---

## Acceptance Criteria

- [ ] `go build ./...` 零错误
- [ ] `go vet ./...` 零警告
- [ ] `BuildOrchestrationGraph` 正确返回包含三个节点的 `compose.Runnable`
- [ ] RAG 命中时路由到 `answer_node`（日志可观测）
- [ ] RAG 未命中时路由到 `complex_node`（日志可观测）
- [ ] SSE `[DONE]` 正确发送
- [ ] 输出内容不包含 `[RESOLVED]` / `[TO_COMPLEX]` 路由标记
- [ ] `ChatResumeStream` 仍可通过 `complexRunner` 正常工作
- [ ] Milvus 不可用时服务正常启动（降级模式）

## Completion Checklist

- [ ] 代码注释使用中文（与项目规范一致）
- [ ] 所有 error 使用 `fmt.Errorf("context: %w", err)` 包装
- [ ] 新文件在包声明后包含适当的 import
- [ ] `interruptErr` / `InterruptErr` 正确导出（供 service.go 跨包使用）
- [ ] `OrchestrationConfig = Config`（别名或单独结构体，避免破坏现有调用）
- [ ] 删除 `Application.DialogueAgent` 字段引用（或兼容并存）
- [ ] `cmd.go` 中 `NewService` 调用参数更新

## Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| BashApproval interrupt 在图层丢失 | High | High | 使用方案 A（interruptErr 传播）并在 Service 层处理；或方案 B 保留 complexRunner |
| Gate Agent 未按预期输出 [RESOLVED]/[TO_COMPLEX] | Medium | Medium | 路由函数有 fallback（检查 schema.Tool 消息内容），降级到 complex_node |
| 图节点类型不对齐编译报错 | Medium | Low | 所有节点 `[]*schema.Message` → `[]*schema.Message`，编译时检查 |
| answer/complex 输出的 Gate 标记污染最终回复 | Medium | Medium | `extractFinalAssistantContent` 过滤标记字符串 |
| 初始版本无 token 级别流式（用户感知延迟） | High | Medium | 接受为已知限制；后续可改为 StreamableLambda + channel 推送 |

## Notes

1. **关于 `OrchestrationConfig = Config`**：直接使用类型别名 `type OrchestrationConfig = Config` 让 `BuildOrchestrationGraph` 接受与原 `NewDialogueAgent` 相同的配置，无需修改 `app.go` 的配置构建逻辑。

2. **关于 Milvus 初始化重复**：原 `NewDialogueAgent` 和新的 `BuildOrchestrationGraph` 都会初始化 Milvus retriever。如果同时运行两者，会创建两个连接。重构完成后应删除 `NewDialogueAgent` 调用，仅保留 `BuildOrchestrationGraph`。

3. **关于 collectAgentMessages 中的 GetMessage()**：由于 `EnableStreaming: false`，`MessageOutput` 会是 `Message`（非 stream）。`GetMessage()` 调用是安全的。若将来需要 streaming，改为 stream 处理。

4. **关于 complexRunner 与 interrupt/resume**：现有 `ChatResumeStream` 调用 `c.resumeAgent(ctx, c.chatStreamRunner, ...)` — 需要将 `c.chatStreamRunner` 替换为 `c.complexRunner`。确保 `complexRunner` 字段使用与原来 `chatStreamRunner` 相同的 `CheckPointStore` 实例（共享 Redis）。

5. **关于流式改进的路径**：未来优化时，`complex_node` 的 Lambda 可改为 `StreamableLambda`，通过 `schema.Pipe` 将 agent 事件转为消息流，配合 `graph.Stream()` 实现真正的 token 级别流式输出。
