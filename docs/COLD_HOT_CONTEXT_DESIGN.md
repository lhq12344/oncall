# 上下文冷热数据分离设计方案

## 一、背景和目标

### 1.1 当前问题

**现有方案**（纯 Redis）:
- ✅ 优点：快速访问，实现简单
- ❌ 缺点：
  - 长对话场景下，历史信息被裁剪丢失
  - 无法智能召回相关历史经验
  - Token 预算固定，无法动态优化
  - 缺少语义理解，只能按时序裁剪

**目标**:
- 支持超长对话（100+ 轮）
- 智能召回相关历史
- 优化 Token 使用效率
- 保留完整对话记录

### 1.2 设计目标

1. **性能**: 召回延迟 < 100ms
2. **准确性**: 召回相关度 > 0.75
3. **可扩展**: 支持百万级对话存储
4. **成本**: Token 使用效率提升 30%+

## 二、架构设计

### 2.1 三层存储架构

```
┌─────────────────────────────────────────────────────────┐
│                    请求处理层                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐              │
│  │ 召回策略 │→│ 混合召回 │→│ Token优化│              │
│  └──────────┘  └──────────┘  └──────────┘              │
└─────────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────────┐
│                    存储层                                 │
│                                                          │
│  ┌──────────────────────────────────────────┐          │
│  │  热数据层（Redis）                        │          │
│  │  - 最近 5-10 Turn                        │          │
│  │  - 延迟: < 10ms                          │          │
│  │  - TTL: 2h（滑动）                       │          │
│  └──────────────────────────────────────────┘          │
│                        ↓                                 │
│  ┌──────────────────────────────────────────┐          │
│  │  冷数据层（Milvus）                       │          │
│  │  - 历史 Turn 向量                         │          │
│  │  - 延迟: 20-50ms                         │          │
│  │  - TTL: 30天                             │          │
│  └──────────────────────────────────────────┘          │
│                        ↓                                 │
│  ┌──────────────────────────────────────────┐          │
│  │  温数据层（MySQL）                        │          │
│  │  - 完整对话记录                           │          │
│  │  - 延迟: 10-30ms                         │          │
│  │  - 永久存储                               │          │
│  └──────────────────────────────────────────┘          │
└─────────────────────────────────────────────────────────┘
```

### 2.2 数据流转

```go
// 写入流程
func SaveTurn(turn *ConversationTurn) error {
    // 1. 写入 Redis（热数据）
    redis.RPush(hotKey, turn)

    // 2. 检查是否需要冷化
    if redis.LLen(hotKey) > HotThreshold {
        // 3. 移出最旧的 Turn
        oldTurn := redis.LPop(hotKey)

        // 4. 向量化
        embedding := embedder.Embed(oldTurn)

        // 5. 写入 Milvus（冷数据）
        milvus.Insert(collection, {
            turn_id: oldTurn.ID,
            embedding: embedding,
            ...
        })

        // 6. 写入 MySQL（温数据）
        mysql.Insert(table, oldTurn)
    }

    return nil
}

// 召回流程
func RecallContext(sessionID, question string) ([]*Message, error) {
    // 1. 召回热数据（全部）
    hotTurns := redis.LRange(hotKey, 0, -1)

    // 2. 判断是否需要召回冷数据
    if needColdRecall(question, hotTurns) {
        // 3. 向量化当前问题
        queryEmbedding := embedder.Embed(question)

        // 4. Milvus 向量检索
        coldTurns := milvus.Search(
            collection: "conversation_turns",
            vector: queryEmbedding,
            topK: 5,
            filter: fmt.Sprintf("session_id == '%s'", sessionID),
        )

        // 5. 过滤和去重
        relevantTurns := filterAndDeduplicate(coldTurns, hotTurns)

        // 6. 混合排序
        allTurns := merge(hotTurns, relevantTurns)
    } else {
        allTurns := hotTurns
    }

    // 7. Token 预算优化
    optimizedTurns := optimizeTokenBudget(allTurns, budget)

    return optimizedTurns, nil
}
```

## 三、详细设计

### 3.1 数据结构

#### 3.1.1 ConversationTurn

```go
type ConversationTurn struct {
    TurnID           string    `json:"turn_id"`           // UUID
    SessionID        string    `json:"session_id"`        // 会话 ID
    TurnIndex        int       `json:"turn_index"`        // 第几轮
    Timestamp        int64     `json:"timestamp"`         // Unix 时间戳
    UserMessage      string    `json:"user_message"`      // 用户消息
    AssistantMessage string    `json:"assistant_message"` // 助手消息
    Tokens           int       `json:"tokens"`            // Token 数
    Intent           string    `json:"intent"`            // 意图标签
    ToolsUsed        []string  `json:"tools_used"`        // 使用的工具
    Metadata         map[string]interface{} `json:"metadata"` // 元数据
}
```

#### 3.1.2 Milvus Schema

```go
type MilvusTurnSchema struct {
    TurnID           string    `milvus:"name:turn_id;primary_key"`
    SessionID        string    `milvus:"name:session_id"`
    TurnIndex        int64     `milvus:"name:turn_index"`
    Timestamp        int64     `milvus:"name:timestamp"`
    UserMessage      string    `milvus:"name:user_message"`
    AssistantMessage string    `milvus:"name:assistant_message"`
    Embedding        []float32 `milvus:"name:embedding;dim:2048"`
    Tokens           int32     `milvus:"name:tokens"`
    Intent           string    `milvus:"name:intent"`
    ToolsUsed        string    `milvus:"name:tools_used"` // JSON array
}
```

### 3.2 召回策略

#### 3.2.1 触发条件

```go
func needColdRecall(question string, hotTurns []*Turn) bool {
    // 1. 对话轮次超过阈值
    if len(hotTurns) >= 20 {
        return true
    }

    // 2. 用户明确提及历史
    historicalKeywords := []string{"之前", "上次", "刚才", "earlier", "before"}
    for _, keyword := range historicalKeywords {
        if strings.Contains(question, keyword) {
            return true
        }
    }

    // 3. 与最近对话相似度低（话题切换）
    if len(hotTurns) > 0 {
        recentSimilarity := calculateSimilarity(question, hotTurns[len(hotTurns)-1])
        if recentSimilarity < 0.7 {
            return true
        }
    }

    // 4. Agent 主动请求（通过特殊标记）
    if strings.Contains(question, "[RECALL_HISTORY]") {
        return true
    }

    return false
}
```

#### 3.2.2 相似度计算

```go
func calculateSimilarity(query string, turn *Turn) float64 {
    // 1. 向量化
    queryEmb := embedder.Embed(query)
    turnEmb := embedder.Embed(turn.UserMessage)

    // 2. 余弦相似度
    similarity := cosineSimilarity(queryEmb, turnEmb)

    return similarity
}

func cosineSimilarity(a, b []float32) float64 {
    var dotProduct, normA, normB float64
    for i := range a {
        dotProduct += float64(a[i] * b[i])
        normA += float64(a[i] * a[i])
        normB += float64(b[i] * b[i])
    }
    return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
```

#### 3.2.3 混合召回

```go
func hybridRecall(sessionID, question string, budget int) ([]*Turn, error) {
    // 1. 热数据召回（全部）
    hotTurns, err := recallHotData(sessionID)
    if err != nil {
        return nil, err
    }
    hotTokens := sumTokens(hotTurns)

    // 2. 计算冷数据预算
    coldBudget := budget - hotTokens - systemTokens - safetyTokens
    if coldBudget <= 0 {
        return hotTurns, nil
    }

    // 3. 冷数据召回（Top-K）
    coldTurns, err := recallColdData(sessionID, question, 5)
    if err != nil {
        log.Warn("cold recall failed", zap.Error(err))
        return hotTurns, nil
    }

    // 4. 去重
    coldTurns = deduplicateWithHot(coldTurns, hotTurns)

    // 5. Token 预算裁剪
    coldTurns = trimToBudget(coldTurns, coldBudget)

    // 6. 合并和排序
    allTurns := mergeTurns(hotTurns, coldTurns)

    return allTurns, nil
}
```

### 3.3 Token 优化

#### 3.3.1 动态预算分配

```go
type TokenBudget struct {
    Total      int
    System     int
    Hot        int
    Cold       int
    Summary    int
    Knowledge  int
    Safety     int
}

func allocateBudget(total int, context *RecallContext) *TokenBudget {
    budget := &TokenBudget{Total: total}

    // 1. 固定预算
    budget.System = 10000  // System prompt
    budget.Safety = 2000   // 安全缓冲

    remaining := total - budget.System - budget.Safety

    // 2. 动态分配
    if context.HasColdData {
        // 有冷数据：热 40%，冷 20%，摘要 5%，知识 10%
        budget.Hot = int(float64(remaining) * 0.40)
        budget.Cold = int(float64(remaining) * 0.20)
        budget.Summary = int(float64(remaining) * 0.05)
        budget.Knowledge = int(float64(remaining) * 0.10)
    } else {
        // 无冷数据：热 60%，知识 15%
        budget.Hot = int(float64(remaining) * 0.60)
        budget.Knowledge = int(float64(remaining) * 0.15)
    }

    return budget
}
```

#### 3.3.2 智能裁剪

```go
func smartTrim(turns []*Turn, budget int) []*Turn {
    if sumTokens(turns) <= budget {
        return turns
    }

    // 1. 计算每个 Turn 的重要性分数
    scores := make([]float64, len(turns))
    for i, turn := range turns {
        scores[i] = calculateImportance(turn, i, len(turns))
    }

    // 2. 按分数排序
    sorted := sortByScore(turns, scores)

    // 3. 贪心选择
    selected := []*Turn{}
    currentTokens := 0
    for _, turn := range sorted {
        if currentTokens + turn.Tokens <= budget {
            selected = append(selected, turn)
            currentTokens += turn.Tokens
        }
    }

    // 4. 按时间排序（恢复时序）
    sort.Slice(selected, func(i, j int) bool {
        return selected[i].Timestamp < selected[j].Timestamp
    })

    return selected
}

func calculateImportance(turn *Turn, index, total int) float64 {
    score := 0.0

    // 1. 时间衰减（最近的更重要）
    recency := float64(index) / float64(total)
    score += recency * 0.4

    // 2. 工具使用（使用工具的更重要）
    if len(turn.ToolsUsed) > 0 {
        score += 0.3
    }

    // 3. 消息长度（信息量大的更重要）
    lengthScore := math.Min(float64(len(turn.UserMessage))/1000.0, 1.0)
    score += lengthScore * 0.2

    // 4. 意图类型（某些意图更重要）
    if turn.Intent == "diagnose" || turn.Intent == "execute" {
        score += 0.1
    }

    return score
}
```

## 四、实现计划

### Phase 1: 基础架构（3-4 天）

**Day 1-2: 数据结构和接口**
```go
// 1. 定义数据结构
type ConversationTurn struct { ... }
type ColdHotManager interface {
    SaveTurn(turn *ConversationTurn) error
    RecallContext(sessionID, question string, budget int) ([]*Message, error)
    MigrateToCold(sessionID string, count int) error
}

// 2. 实现 Milvus 集成
func InitMilvusCollection() error { ... }
func InsertTurn(turn *ConversationTurn) error { ... }
func SearchSimilarTurns(query string, topK int) ([]*ConversationTurn, error) { ... }

// 3. 实现 MySQL 集成
func CreateTurnsTable() error { ... }
func InsertTurnRecord(turn *ConversationTurn) error { ... }
func QueryTurnsBySession(sessionID string) ([]*ConversationTurn, error) { ... }
```

**Day 3-4: 数据流转**
```go
// 1. 实现热数据管理
func SaveToHot(turn *ConversationTurn) error { ... }
func GetHotTurns(sessionID string) ([]*ConversationTurn, error) { ... }

// 2. 实现冷化逻辑
func CheckAndMigrate(sessionID string) error { ... }
func MigrateTurnToCold(turn *ConversationTurn) error { ... }

// 3. 实现向量化
func EmbedTurn(turn *ConversationTurn) ([]float32, error) { ... }
```

### Phase 2: 召回策略（2-3 天）

**Day 5-6: 召回实现**
```go
// 1. 实现触发条件判断
func NeedColdRecall(question string, hotTurns []*Turn) bool { ... }

// 2. 实现相似度计算
func CalculateSimilarity(query string, turn *Turn) float64 { ... }

// 3. 实现混合召回
func HybridRecall(sessionID, question string, budget int) ([]*Turn, error) { ... }

// 4. 实现去重和排序
func DeduplicateAndSort(hot, cold []*Turn) []*Turn { ... }
```

**Day 7: Token 优化**
```go
// 1. 实现动态预算分配
func AllocateBudget(total int, context *RecallContext) *TokenBudget { ... }

// 2. 实现智能裁剪
func SmartTrim(turns []*Turn, budget int) []*Turn { ... }

// 3. 实现重要性评分
func CalculateImportance(turn *Turn, index, total int) float64 { ... }
```

### Phase 3: 测试和优化（2-3 天）

**Day 8-9: 测试**
- 单元测试（覆盖率 > 80%）
- 集成测试（端到端流程）
- 性能测试（召回延迟、吞吐量）
- 准确性测试（召回相关度）

**Day 10: 优化**
- 性能优化（缓存、批处理）
- 参数调优（阈值、权重）
- 文档编写

## 五、监控和评估

### 5.1 关键指标

```go
type RecallMetrics struct {
    // 性能指标
    HotRecallLatency   time.Duration // 热数据召回延迟
    ColdRecallLatency  time.Duration // 冷数据召回延迟
    TotalLatency       time.Duration // 总延迟

    // 质量指标
    HotTurnCount       int     // 热数据 Turn 数
    ColdTurnCount      int     // 冷数据 Turn 数
    AvgSimilarity      float64 // 平均相似度

    // Token 指标
    HotTokens          int     // 热数据 Token 数
    ColdTokens         int     // 冷数据 Token 数
    TotalTokens        int     // 总 Token 数
    TokenUtilization   float64 // Token 利用率
}
```

### 5.2 评估方法

**召回质量评估**:
```
1. 人工标注：随机抽样 100 个查询，人工评估召回相关性
2. A/B 测试：对比冷热分离 vs 纯 Redis 的效果
3. 用户反馈：收集用户对回答质量的评分
```

**性能评估**:
```
1. 延迟：P50/P95/P99 延迟
2. 吞吐量：QPS
3. 资源使用：CPU、内存、存储
```

## 六、风险和应对

### 6.1 风险

1. **向量化延迟**: Embedding API 调用可能较慢
   - 应对：异步向量化，不阻塞主流程

2. **Milvus 查询延迟**: 向量检索可能超时
   - 应对：设置超时，失败时降级到纯热数据

3. **存储成本**: Milvus 和 MySQL 存储成本增加
   - 应对：设置 TTL，定期清理旧数据

4. **召回质量**: 可能召回不相关的历史
   - 应对：调整相似度阈值，增加过滤条件

### 6.2 降级策略

```go
func RecallWithFallback(sessionID, question string, budget int) ([]*Turn, error) {
    // 1. 尝试混合召回
    turns, err := hybridRecall(sessionID, question, budget)
    if err == nil {
        return turns, nil
    }

    // 2. 降级：只召回热数据
    log.Warn("hybrid recall failed, fallback to hot only", zap.Error(err))
    hotTurns, err := recallHotData(sessionID)
    if err == nil {
        return hotTurns, nil
    }

    // 3. 最终降级：返回空历史
    log.Error("all recall failed", zap.Error(err))
    return []*Turn{}, nil
}
```

## 七、总结

### 7.1 预期收益

- **Token 效率**: 提升 30-50%
- **召回质量**: 相关度 > 0.75
- **支持长度**: 支持 100+ 轮对话
- **延迟**: < 100ms

### 7.2 后续优化

1. **分层摘要**: 自动生成会话摘要
2. **知识提取**: 提取关键实体和关系
3. **主题聚类**: 按主题组织历史对话
4. **多模态**: 支持图片、日志等多模态内容
