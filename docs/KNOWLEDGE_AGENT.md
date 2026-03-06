# Knowledge Agent 实现文档

## 概述

Knowledge Agent 是基于 Eino ADK 的知识库代理，负责历史故障案例的检索、排序、反馈管理和知识进化。

## 核心功能

### 1. 知识检索（Knowledge Retrieval）

#### 基础检索
```go
cases, err := knowledgeAgent.Search(ctx, "pod 启动失败", 5)
```

- 使用 Milvus 向量检索
- 支持语义相似度搜索
- 返回 Top-K 相关案例

#### 上下文增强检索
```go
cases, err := knowledgeAgent.SearchWithContext(ctx, sessionID, "有什么异常", 5)
```

- 结合对话历史增强查询
- 自动拼接最近 3 轮对话
- 提高检索准确性

### 2. 案例排序（Case Ranking）

#### 综合评分算法
```
Score = w1·Similarity + w2·Quality + w3·Freshness + w4·Usage + KeywordBonus
```

**权重配置**:
- 相似度权重（w1）: 0.4
- 质量权重（w2）: 0.3
- 时效性权重（w3）: 0.2
- 使用频率权重（w4）: 0.1

#### 时效性评分
使用指数衰减函数：
```
FreshnessScore = e^(-λ · days)
```
- λ = 0.01（约 100 天后衰减到 37%）

#### 使用频率评分
使用对数归一化：
```
UsageScore = log(1 + usage_count) / log(1 + max_usage)
```

#### 关键词匹配加成
- 提取查询关键词
- 检查标题和内容匹配度
- 最多加成 0.1 分

### 3. 反馈管理（Feedback Management）

#### 添加反馈
```go
feedback := &Feedback{
    Helpful: true,
    Rating:  5,
    Comment: "很有帮助",
}
err := knowledgeAgent.AddFeedback(ctx, caseID, feedback)
```

#### 质量评分计算
```
QualityScore = HelpfulRate · 0.6 + AvgRating · 0.4
```

- HelpfulRate: 有用率（0-1）
- AvgRating: 平均评分（归一化到 0-1）

#### 反馈统计
```go
stats, err := feedbackManager.GetStatistics(ctx, caseID)
// stats.TotalCount: 总反馈数
// stats.HelpfulCount: 有用数
// stats.HelpfulRate: 有用率
// stats.AvgRating: 平均评分
```

### 4. 知识进化（Knowledge Evolution）

#### 成功路径提取
```go
doc, err := knowledgeAgent.ExtractSuccessPath(ctx, executionLog)
```

从执行日志中提取：
- 问题描述
- 解决方案
- 执行步骤
- 执行时长
- 成功率

#### 知识剪枝
自动识别低质量知识：

**剪枝条件**:
1. 质量评分 < 0.3
2. 90 天未使用
3. 成功率 < 0.2（且使用次数 > 5）

```go
shouldPrune, reason := pruneManager.ShouldPrune(ctx, kcase)
```

#### 重复检测
基于 Jaccard 相似度：
```
Similarity = |A ∩ B| / |A ∪ B|
```

```go
duplicates := pruneManager.FindDuplicates(ctx, cases, 0.5)
```

### 5. 工具封装（Tool Wrapping）

#### KnowledgeSearchTool
```json
{
  "name": "knowledge_search",
  "desc": "搜索历史故障案例和最佳实践",
  "params": {
    "query": "搜索关键词或故障描述",
    "top_k": "返回结果数量（默认 5）",
    "session_id": "会话 ID（可选）"
  }
}
```

#### KnowledgeIndexTool
```json
{
  "name": "knowledge_index",
  "desc": "将新的故障案例索引到知识库",
  "params": {
    "content": "文档内容",
    "title": "文档标题",
    "solution": "解决方案",
    "tags": "标签列表"
  }
}
```

#### KnowledgeFeedbackTool
```json
{
  "name": "knowledge_feedback",
  "desc": "提交对知识案例的反馈",
  "params": {
    "case_id": "案例 ID",
    "helpful": "是否有帮助",
    "rating": "评分（1-5）",
    "comment": "评论"
  }
}
```

## 数据结构

### KnowledgeCase
```go
type KnowledgeCase struct {
    ID           string                 // 案例 ID
    Title        string                 // 标题
    Content      string                 // 内容
    Solution     string                 // 解决方案
    Score        float64                // 相似度评分
    QualityScore float64                // 质量评分
    Tags         []string               // 标签
    Metadata     map[string]interface{} // 元数据
    RetrievedAt  time.Time              // 检索时间
    UsageCount   int                    // 使用次数
    SuccessRate  float64                // 成功率
}
```

### Feedback
```go
type Feedback struct {
    CaseID    string    // 案例 ID
    UserID    string    // 用户 ID
    Helpful   bool      // 是否有帮助
    Rating    int       // 评分（1-5）
    Comment   string    // 评论
    Timestamp time.Time // 时间戳
}
```

### ExecutionLog
```go
type ExecutionLog struct {
    ExecutionID string        // 执行 ID
    Problem     string        // 问题描述
    Solution    string        // 解决方案
    Steps       []string      // 执行步骤
    Duration    time.Duration // 执行时长
    Success     bool          // 是否成功
    SuccessRate float64       // 成功率
    Tags        []string      // 标签
    Timestamp   time.Time     // 时间戳
}
```

## 使用示例

### 1. 创建 Knowledge Agent
```go
import (
    "go_agent/internal/agent/knowledge"
    "go_agent/internal/ai/retriever"
)

// 创建 Milvus 检索器
milvusRetriever, err := retriever.NewMilvusRetriever(ctx)
if err != nil {
    return err
}

// 创建 Knowledge Agent
knowledgeAgent := knowledge.NewKnowledgeAgent(&knowledge.Config{
    ContextManager: contextManager,
    Retriever:      milvusRetriever,
    Indexer:        nil, // 可选
    Logger:         logger,
})
```

### 2. 搜索知识
```go
// 基础搜索
cases, err := knowledgeAgent.Search(ctx, "pod 启动失败", 5)
if err != nil {
    return err
}

for _, kcase := range cases {
    fmt.Printf("案例: %s\n", kcase.Title)
    fmt.Printf("相似度: %.2f\n", kcase.Score)
    fmt.Printf("质量评分: %.2f\n", kcase.QualityScore)
    fmt.Printf("解决方案: %s\n", kcase.Solution)
}
```

### 3. 添加反馈
```go
feedback := &knowledge.Feedback{
    UserID:  "user123",
    Helpful: true,
    Rating:  5,
    Comment: "解决了我的问题",
}

err := knowledgeAgent.AddFeedback(ctx, caseID, feedback)
```

### 4. 提取成功路径
```go
execLog := &knowledge.ExecutionLog{
    ExecutionID: "exec_001",
    Problem:     "Nginx 服务无法启动",
    Solution:    "修复配置文件语法错误",
    Steps: []string{
        "检查 nginx 配置文件",
        "发现语法错误",
        "修复配置文件",
        "重启 nginx 服务",
    },
    Duration:    5 * time.Minute,
    Success:     true,
    SuccessRate: 1.0,
    Tags:        []string{"nginx", "配置错误"},
}

doc, err := knowledgeAgent.ExtractSuccessPath(ctx, execLog)
```

### 5. 作为工具使用
```go
// 创建工具
searchTool := knowledge.NewKnowledgeSearchTool(knowledgeAgent)

// 注册到 Supervisor
supervisorAgent.RegisterTool(searchTool)

// 工具会被 LLM 自动调用
```

## 测试

### 运行测试
```bash
go test ./internal/agent/knowledge/... -v
```

### 测试覆盖
- ✅ CaseRanker 排序算法
- ✅ FeedbackManager 反馈管理
- ✅ PruneManager 剪枝逻辑
- ✅ 关键词提取
- ✅ 重复检测
- ✅ 数据结构验证

## 性能优化

### 1. 缓存策略
- 热门案例缓存（LRU）
- 查询结果缓存（5 分钟 TTL）
- 反馈统计缓存

### 2. 批量操作
- 批量索引文档
- 批量更新反馈
- 批量剪枝

### 3. 异步处理
- 异步索引（不阻塞主流程）
- 异步统计计算
- 异步剪枝任务

## 未来改进

### 1. 增强检索
- [ ] 混合检索（向量 + 关键词 + 标量过滤）
- [ ] 多模态检索（文本 + 图片 + 日志）
- [ ] 个性化推荐（基于用户历史）

### 2. 智能排序
- [ ] 学习排序（Learning to Rank）
- [ ] 上下文感知排序
- [ ] A/B 测试框架

### 3. 知识图谱
- [ ] 构建故障关联图
- [ ] 根因推理增强
- [ ] 知识演化追踪

### 4. 自动化
- [ ] 自动标签生成
- [ ] 自动摘要提取
- [ ] 自动质量评估

## 文件清单

```
internal/agent/knowledge/
├── knowledge.go       # Knowledge Agent 主逻辑
├── types.go          # 数据结构定义
├── ranker.go         # 案例排序器
├── feedback.go       # 反馈管理器
├── tool.go           # 工具封装
├── knowledge_test.go # 完整测试
└── basic_test.go     # 基础测试（不依赖外部库）
```

## 依赖项

```go
require (
    github.com/cloudwego/eino v0.7.14
    github.com/cloudwego/eino/compose
    github.com/cloudwego/eino/schema
    go.uber.org/zap v1.27.0
)
```

---

**文档版本**: v1.0
**最后更新**: 2026-03-06
**作者**: Oncall Team
