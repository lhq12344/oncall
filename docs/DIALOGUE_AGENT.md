# Dialogue Agent 实现文档

## 概述

Dialogue Agent 是基于 Eino ADK 的对话代理，负责意图预测、候选问题生成、对话状态管理和实体提取。通过智能的对话引导，帮助用户更快地定位和解决问题。

## 核心功能

### 1. 意图分析（Intent Analysis）

#### 基础意图预测
```go
analysis, err := dialogueAgent.AnalyzeIntent(ctx, sessionID, "查看 pod 状态")
```

**支持的意图类型**:
- `monitor`: 监控查询（查看、监控、状态、指标）
- `diagnose`: 故障诊断（故障、问题、错误、异常）
- `execute`: 执行操作（重启、扩容、部署、回滚）
- `knowledge`: 知识检索（历史、案例、文档、经验）
- `general`: 通用对话

**返回信息**:
- 意图类型
- 置信度（0-1）
- 语义熵（0-1）
- 是否收敛

#### 意图收敛判断
```
Converged = Entropy < 0.3
```

当语义熵低于阈值时，表示用户意图已经明确，可以开始预测下一步问题。

### 2. 语义熵计算（Entropy Calculation）

#### 基于关键词重复度
```go
entropy, err := entropyCalculator.Calculate(ctx, session)
```

**计算方法**:
```
Entropy = 1 - RepeatRate
RepeatRate = RepeatedKeywords / TotalKeywords
```

**特点**:
- 对话越长，重复度越高，熵越低
- 熵低表示意图收敛
- 熵高表示意图模糊

#### 基于向量相似度（可选）
```go
entropy, err := entropyCalculator.CalculateWithVector(ctx, session)
```

**计算方法**:
```
Entropy = 1 - CosineSimilarity(Vector1, Vector2)
```

需要提供 Embedder（向量化器）。

### 3. 候选问题生成（Question Generation）

#### 预测下一步问题
```go
questions, err := dialogueAgent.PredictNextQuestions(ctx, sessionID, 3)
```

**生成策略**:
1. 根据意图类型选择问题模板
2. 过滤已问过的问题
3. 生成基于上下文的问题
4. 返回 Top-K 候选问题

**预定义模板**:

| 意图类型 | 候选问题示例 |
|---------|------------|
| monitor | "需要查看哪个服务的监控数据？" |
| monitor | "想了解哪个指标的情况？（CPU、内存、磁盘等）" |
| diagnose | "故障是从什么时候开始的？" |
| diagnose | "有看到具体的错误信息吗？" |
| execute | "确定要执行这个操作吗？" |
| execute | "需要在哪个环境执行？（生产/测试）" |
| knowledge | "想了解哪方面的历史案例？" |

#### 异步预测（不阻塞主流程）
```go
dialogueAgent.PredictNextQuestionsAsync(ctx, sessionID, 3)
```

在后台异步生成候选问题，不影响主流程响应速度。

#### 澄清性问题生成
```go
question, err := dialogueAgent.GenerateClarificationQuestion(ctx, sessionID)
```

当用户意图不明确时，生成澄清性问题引导用户。

### 4. 对话状态管理（Dialogue State Tracking）

#### 状态转移
```go
err := stateTracker.TransitionTo(ctx, sessionID, "monitoring", "user_query")
```

**状态类型**:
- `initial`: 初始状态
- `monitoring`: 监控查询中
- `diagnosing`: 故障诊断中
- `executing`: 执行操作中
- `completed`: 已完成

#### 状态历史
```go
history, err := stateTracker.GetStateHistory(ctx, sessionID)
```

记录所有状态转移，包括：
- 源状态
- 目标状态
- 触发条件
- 时间戳

#### 元数据管理
```go
// 设置元数据
err := stateTracker.SetMetadata(ctx, sessionID, "service_name", "nginx")

// 获取元数据
val, ok := stateTracker.GetMetadata(ctx, sessionID, "service_name")
```

### 5. 实体提取（Entity Extraction）

#### 提取关键实体
```go
entities, err := dialogueAgent.ExtractEntities(ctx, "查看 nginx 的 CPU 使用率")
```

**支持的实体类型**:
- `service`: 服务名（nginx、mysql、redis、kafka、etcd）
- `metric`: 指标名（cpu、memory、disk、latency、qps、error_rate）
- `time_range`: 时间范围（最近5分钟、最近1小时、今天）

**返回示例**:
```json
{
  "service": "nginx",
  "metric": "cpu",
  "time_range": "5m"
}
```

### 6. 对话总结（Conversation Summary）

#### 总结对话内容
```go
summary, err := dialogueAgent.SummarizeConversation(ctx, sessionID)
```

**总结内容**:
- 对话轮次
- 主要话题
- 关键信息

### 7. 工具封装（Tool Wrapping）

#### IntentAnalysisTool
```json
{
  "name": "intent_analysis",
  "desc": "分析用户意图",
  "params": {
    "session_id": "会话 ID",
    "user_input": "用户输入文本"
  }
}
```

#### QuestionPredictionTool
```json
{
  "name": "question_prediction",
  "desc": "预测用户下一步可能提出的问题",
  "params": {
    "session_id": "会话 ID",
    "count": "预测问题数量（默认 3）"
  }
}
```

#### ClarificationTool
```json
{
  "name": "clarification",
  "desc": "生成澄清性问题",
  "params": {
    "session_id": "会话 ID"
  }
}
```

#### EntityExtractionTool
```json
{
  "name": "entity_extraction",
  "desc": "提取关键实体",
  "params": {
    "user_input": "用户输入文本"
  }
}
```

#### ConversationSummaryTool
```json
{
  "name": "conversation_summary",
  "desc": "总结对话内容",
  "params": {
    "session_id": "会话 ID"
  }
}
```

## 数据结构

### IntentAnalysis
```go
type IntentAnalysis struct {
    Intent     *Intent   // 意图
    Entropy    float64   // 语义熵
    Converged  bool      // 是否收敛
    Confidence float64   // 置信度
    Timestamp  time.Time // 时间戳
}
```

### Intent
```go
type Intent struct {
    Type       string                 // 意图类型
    Confidence float64                // 置信度
    Entities   map[string]interface{} // 实体
    SubType    string                 // 子类型（可选）
}
```

### DialogueState
```go
type DialogueState struct {
    SessionID    string            // 会话 ID
    CurrentState string            // 当前状态
    PrevState    string            // 前一个状态
    Transitions  []StateTransition // 状态转移历史
    Metadata     map[string]interface{} // 元数据
    UpdatedAt    time.Time         // 更新时间
}
```

## 使用示例

### 1. 创建 Dialogue Agent
```go
import (
    "go_agent/internal/agent/dialogue"
)

// 创建 Dialogue Agent
dialogueAgent := dialogue.NewDialogueAgent(&dialogue.Config{
    ContextManager: contextManager,
    Embedder:       nil, // 可选
    Logger:         logger,
})
```

### 2. 分析意图
```go
// 分析用户意图
analysis, err := dialogueAgent.AnalyzeIntent(ctx, sessionID, "查看 pod 状态")
if err != nil {
    return err
}

fmt.Printf("意图类型: %s\n", analysis.Intent.Type)
fmt.Printf("置信度: %.2f\n", analysis.Confidence)
fmt.Printf("语义熵: %.2f\n", analysis.Entropy)
fmt.Printf("是否收敛: %v\n", analysis.Converged)
```

### 3. 预测候选问题
```go
// 预测下一步问题
questions, err := dialogueAgent.PredictNextQuestions(ctx, sessionID, 3)
if err != nil {
    return err
}

fmt.Println("您可能想问：")
for i, q := range questions {
    fmt.Printf("%d. %s\n", i+1, q)
}
```

### 4. 异步预测（推荐）
```go
// 异步预测，不阻塞主流程
dialogueAgent.PredictNextQuestionsAsync(ctx, sessionID, 3)

// 主流程继续处理其他逻辑
// ...

// 稍后从会话中获取预测的问题
session, _ := contextManager.GetSession(ctx, sessionID)
questions := session.PredictedQuestions
```

### 5. 提取实体
```go
entities, err := dialogueAgent.ExtractEntities(ctx, "查看 nginx 的 CPU 使用率")
if err != nil {
    return err
}

if service, ok := entities["service"]; ok {
    fmt.Printf("服务: %v\n", service)
}
if metric, ok := entities["metric"]; ok {
    fmt.Printf("指标: %v\n", metric)
}
```

### 6. 作为工具使用
```go
// 创建工具
intentTool := dialogue.NewIntentAnalysisTool(dialogueAgent)
questionTool := dialogue.NewQuestionPredictionTool(dialogueAgent)

// 注册到 Supervisor
supervisorAgent.RegisterTool(intentTool)
supervisorAgent.RegisterTool(questionTool)

// 工具会被 LLM 自动调用
```

## 工作流程

### 完整的对话流程
```
1. 用户输入 → Dialogue Agent
2. 分析意图 → 识别类型（monitor/diagnose/execute/knowledge）
3. 计算语义熵 → 判断是否收敛
4. 如果收敛 → 预测候选问题（异步）
5. 如果不收敛 → 生成澄清性问题
6. 提取实体 → 识别服务名、指标名、时间范围
7. 更新对话状态 → 记录状态转移
8. 返回分析结果 → 供 Supervisor 使用
```

### 意图收敛过程
```
轮次 1: "有问题" → 熵=1.0（意图模糊）→ 澄清："能否详细描述？"
轮次 2: "pod 启动失败" → 熵=0.6（意图逐渐明确）→ 继续收集信息
轮次 3: "pod 一直 Pending" → 熵=0.2（意图收敛）→ 预测候选问题
```

## 测试

### 运行测试
```bash
go test ./internal/agent/dialogue/... -v
```

### 测试覆盖
- ✅ IntentPredictor 意图预测
- ✅ EntropyCalculator 熵计算
- ✅ QuestionGenerator 问题生成
- ✅ DialogueStateTracker 状态跟踪
- ✅ DialogueAgent 完整流程
- ✅ 实体提取
- ✅ 余弦相似度计算

## 性能优化

### 1. 异步预测
- 使用 goroutine 异步预测候选问题
- 不阻塞主流程
- 提高响应速度

### 2. 缓存机制
- 缓存意图预测结果
- 缓存问题模板
- 减少重复计算

### 3. 批量处理
- 批量向量化（如果使用 Embedder）
- 批量状态更新

## 未来改进

### 1. LLM 增强
- [ ] 使用 LLM 进行意图分类（更准确）
- [ ] 使用 LLM 生成候选问题（更智能）
- [ ] 使用 LLM 进行实体提取（更全面）

### 2. 向量化增强
- [ ] 使用向量计算语义熵（更精确）
- [ ] 使用向量检索历史相似对话
- [ ] 基于向量的意图分类

### 3. 个性化
- [ ] 基于用户历史的个性化问题推荐
- [ ] 学习用户偏好
- [ ] 自适应问题生成策略

### 4. 多轮对话管理
- [ ] 对话树结构
- [ ] 多分支对话流
- [ ] 对话回溯

## 文件清单

```
internal/agent/dialogue/
├── dialogue.go            # Dialogue Agent 主逻辑
├── types.go              # 数据结构定义
├── predictor.go          # 意图预测器 + 熵计算器
├── question_generator.go # 问题生成器
├── state_tracker.go      # 对话状态跟踪器
├── tool.go               # 工具封装（5 个工具）
└── dialogue_test.go      # 测试
```

## 依赖项

```go
require (
    go_agent/internal/context
    github.com/cloudwego/eino/compose
    go.uber.org/zap v1.27.0
)
```

---

**文档版本**: v1.0
**最后更新**: 2026-03-06
**作者**: Oncall Team
