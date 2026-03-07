package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// VectorSearchTool 向量检索工具
type VectorSearchTool struct {
	// retriever einoRetriever.Retriever // TODO: implement
	logger *zap.Logger
}

func NewVectorSearchTool(r interface{}, logger *zap.Logger) tool.BaseTool {
	return &VectorSearchTool{
		// retriever: r,
		logger: logger,
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

	if t.logger != nil {
		t.logger.Info("vector search invoked (placeholder)",
			zap.String("query", params.Query),
			zap.Int("top_k", params.TopK))
	}

	// TODO: Implement Milvus retrieval
	return `{"status":"degraded","implemented":false,"backend":"milvus","error_code":"milvus_not_integrated","message":"向量检索功能尚未完全实现（需要 Milvus 集成）","results":[]}`, nil
}
