package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// VectorSearchTool 向量检索工具
type VectorSearchTool struct {
	retriever einoRetriever.Retriever
	logger    *zap.Logger
}

func NewVectorSearchTool(r einoRetriever.Retriever, logger *zap.Logger) tool.BaseTool {
	return &VectorSearchTool{
		retriever: r,
		logger:    logger,
	}
}

func (t *VectorSearchTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "vector_search",
		Desc: "基于语义相似度检索历史故障案例。输入查询文本，返回最相关的案例列表。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "搜索查询文本（故障描述或关键词）",
				Required: true,
			},
			"top_k": {
				Type:     schema.Integer,
				Desc:     "返回结果数量（默认 3）",
				Required: false,
			},
		}),
	}, nil
}

func (t *VectorSearchTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	// 解析参数
	var params struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}

	if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
		return "", fmt.Errorf("invalid parameters: %w", err)
	}

	// 参数验证
	if params.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if params.TopK == 0 {
		params.TopK = 3 // 默认值
	}

	t.logger.Info("vector search invoked",
		zap.String("query", params.Query),
		zap.Int("top_k", params.TopK))

	// 检查 retriever 是否可用
	if t.retriever == nil {
		return `{"error": "向量检索功能尚未配置（需要 Milvus）", "results": []}`, nil
	}

	// 调用 Milvus Retriever
	docs, err := t.retriever.Retrieve(ctx, params.Query, einoRetriever.WithTopK(params.TopK))
	if err != nil {
		t.logger.Error("retrieval failed", zap.Error(err))
		return "", fmt.Errorf("retrieval failed: %w", err)
	}

	// 使用 CaseRanker 进行多维度排序
	ranker := NewCaseRanker()
	rankedResults := ranker.Rank(docs)

	// 格式化结果
	type SearchResult struct {
		ID              string                 `json:"id"`
		Content         string                 `json:"content"`
		Score           float64                `json:"score"`           // 原始相似度分数
		SimilarityScore float64                `json:"similarity_score"` // 相似度分数
		RecencyScore    float64                `json:"recency_score"`    // 时效性分数
		SuccessScore    float64                `json:"success_score"`    // 成功率分数
		CompositeScore  float64                `json:"composite_score"`  // 综合分数
		Metadata        map[string]interface{} `json:"metadata,omitempty"`
	}

	results := make([]SearchResult, 0, len(rankedResults))
	for _, ranked := range rankedResults {
		results = append(results, SearchResult{
			ID:              ranked.Doc.ID,
			Content:         ranked.Doc.Content,
			Score:           ranked.Doc.Score(),
			SimilarityScore: ranked.SimilarityScore,
			RecencyScore:    ranked.RecencyScore,
			SuccessScore:    ranked.SuccessScore,
			CompositeScore:  ranked.CompositeScore,
			Metadata:        ranked.Doc.MetaData,
		})
	}

	output := map[string]interface{}{
		"query":        params.Query,
		"result_count": len(results),
		"results":      results,
	}

	outputJSON, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("failed to marshal output: %w", err)
	}

	t.logger.Info("vector search completed",
		zap.Int("result_count", len(results)))

	return string(outputJSON), nil
}