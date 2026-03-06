package knowledge

import (
	"context"
	"fmt"
	"sync"
	"time"

	appcontext "go_agent/internal/context"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// KnowledgeAgent 知识库代理
type KnowledgeAgent struct {
	contextManager *appcontext.ContextManager
	retriever      compose.Retriever // Milvus 检索器
	indexer        compose.Indexer   // 知识索引器
	ranker         *CaseRanker       // 案例排序器
	feedback       *FeedbackManager  // 反馈管理器
	logger         *zap.Logger
	mu             sync.RWMutex
}

// Config Knowledge Agent 配置
type Config struct {
	ContextManager *appcontext.ContextManager
	Retriever      compose.Retriever
	Indexer        compose.Indexer
	Logger         *zap.Logger
}

// NewKnowledgeAgent 创建 Knowledge Agent
func NewKnowledgeAgent(cfg *Config) *KnowledgeAgent {
	return &KnowledgeAgent{
		contextManager: cfg.ContextManager,
		retriever:      cfg.Retriever,
		indexer:        cfg.Indexer,
		ranker:         NewCaseRanker(),
		feedback:       NewFeedbackManager(),
		logger:         cfg.Logger,
	}
}

// Search 搜索历史故障案例
func (k *KnowledgeAgent) Search(ctx context.Context, query string, topK int) ([]*KnowledgeCase, error) {
	k.logger.Info("searching knowledge base",
		zap.String("query", query),
		zap.Int("top_k", topK),
	)

	// 1. 使用 Milvus 检索器进行向量搜索
	docs, err := k.retriever.Retrieve(ctx, query, compose.WithRetrieverTopK(topK))
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve documents: %w", err)
	}

	k.logger.Info("retrieved documents",
		zap.Int("count", len(docs)),
	)

	// 2. 转换为 KnowledgeCase
	cases := make([]*KnowledgeCase, 0, len(docs))
	for i, doc := range docs {
		kcase := &KnowledgeCase{
			ID:          fmt.Sprintf("case_%d", i),
			Content:     doc.Content,
			Score:       doc.Score,
			Metadata:    doc.MetaData,
			RetrievedAt: time.Now(),
		}

		// 从 metadata 中提取额外信息
		if title, ok := doc.MetaData["title"].(string); ok {
			kcase.Title = title
		}
		if solution, ok := doc.MetaData["solution"].(string); ok {
			kcase.Solution = solution
		}
		if tags, ok := doc.MetaData["tags"].([]string); ok {
			kcase.Tags = tags
		}

		cases = append(cases, kcase)
	}

	// 3. 使用 Ranker 排序
	rankedCases := k.ranker.Rank(ctx, query, cases)

	k.logger.Info("ranked cases",
		zap.Int("count", len(rankedCases)),
	)

	return rankedCases, nil
}

// SearchWithContext 带上下文的搜索（考虑对话历史）
func (k *KnowledgeAgent) SearchWithContext(ctx context.Context, sessionID, query string, topK int) ([]*KnowledgeCase, error) {
	// 获取会话上下文
	session, err := k.contextManager.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	// 构建增强查询（结合对话历史）
	enhancedQuery := k.buildEnhancedQuery(session, query)

	k.logger.Info("searching with context",
		zap.String("session_id", sessionID),
		zap.String("original_query", query),
		zap.String("enhanced_query", enhancedQuery),
	)

	// 执行搜索
	return k.Search(ctx, enhancedQuery, topK)
}

// buildEnhancedQuery 构建增强查询
func (k *KnowledgeAgent) buildEnhancedQuery(session *appcontext.SessionContext, query string) string {
	// 简单实现：拼接最近的对话历史
	session.mu.RLock()
	defer session.mu.RUnlock()

	if len(session.History) == 0 {
		return query
	}

	// 取最近 3 轮对话
	recentMessages := session.History
	if len(recentMessages) > 6 { // 3 轮 = 6 条消息（user + assistant）
		recentMessages = recentMessages[len(recentMessages)-6:]
	}

	// 拼接上下文
	contextStr := ""
	for _, msg := range recentMessages {
		contextStr += msg.Content + " "
	}

	return contextStr + query
}

// Index 索引新知识
func (k *KnowledgeAgent) Index(ctx context.Context, docs []*schema.Document) error {
	k.logger.Info("indexing documents",
		zap.Int("count", len(docs)),
	)

	if k.indexer == nil {
		return fmt.Errorf("indexer not configured")
	}

	// 使用 Eino 的 Indexer 进行索引
	_, err := k.indexer.Index(ctx, docs)
	if err != nil {
		return fmt.Errorf("failed to index documents: %w", err)
	}

	k.logger.Info("documents indexed successfully")

	return nil
}

// AddFeedback 添加用户反馈
func (k *KnowledgeAgent) AddFeedback(ctx context.Context, caseID string, feedback *Feedback) error {
	k.logger.Info("adding feedback",
		zap.String("case_id", caseID),
		zap.Bool("helpful", feedback.Helpful),
	)

	return k.feedback.AddFeedback(ctx, caseID, feedback)
}

// GetCaseQuality 获取案例质量评分
func (k *KnowledgeAgent) GetCaseQuality(ctx context.Context, caseID string) (float64, error) {
	return k.feedback.GetQualityScore(ctx, caseID)
}

// ExtractSuccessPath 从执行日志中提取成功路径
func (k *KnowledgeAgent) ExtractSuccessPath(ctx context.Context, executionLog *ExecutionLog) (*schema.Document, error) {
	if !executionLog.Success {
		return nil, fmt.Errorf("execution failed, cannot extract success path")
	}

	k.logger.Info("extracting success path",
		zap.String("execution_id", executionLog.ExecutionID),
	)

	// 构建文档内容
	content := fmt.Sprintf(`
问题描述: %s

解决方案: %s

执行步骤:
%s

执行时长: %s
成功率: %.2f
`,
		executionLog.Problem,
		executionLog.Solution,
		k.formatSteps(executionLog.Steps),
		executionLog.Duration,
		executionLog.SuccessRate,
	)

	// 创建文档
	doc := &schema.Document{
		Content: content,
		MetaData: map[string]any{
			"title":         executionLog.Problem,
			"solution":      executionLog.Solution,
			"execution_id":  executionLog.ExecutionID,
			"duration":      executionLog.Duration.Seconds(),
			"success_rate":  executionLog.SuccessRate,
			"created_at":    time.Now().Unix(),
			"tags":          executionLog.Tags,
		},
	}

	// 索引到知识库
	if err := k.Index(ctx, []*schema.Document{doc}); err != nil {
		return nil, fmt.Errorf("failed to index success path: %w", err)
	}

	k.logger.Info("success path extracted and indexed",
		zap.String("execution_id", executionLog.ExecutionID),
	)

	return doc, nil
}

// formatSteps 格式化执行步骤
func (k *KnowledgeAgent) formatSteps(steps []string) string {
	result := ""
	for i, step := range steps {
		result += fmt.Sprintf("%d. %s\n", i+1, step)
	}
	return result
}
