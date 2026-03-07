package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino-ext/components/retriever/milvus"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// VectorSearchTool 向量检索工具
type VectorSearchTool struct {
	retriever *milvus.Retriever
	logger    *zap.Logger
}

func NewVectorSearchTool(r interface{}, logger *zap.Logger) tool.BaseTool {
	var retriever *milvus.Retriever
	if r != nil {
		if ret, ok := r.(*milvus.Retriever); ok {
			retriever = ret
		}
	}

	return &VectorSearchTool{
		retriever: retriever,
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

	// 如果 retriever 不可用，返回降级响应
	if t.retriever == nil {
		if t.logger != nil {
			t.logger.Warn("vector retriever not available, returning degraded response")
		}
		return `{"status":"degraded","results":[],"message":"向量检索功能暂时不可用（Milvus 未连接）"}`, nil
	}

	// 执行检索
	docs, err := t.retriever.Retrieve(ctx, params.Query)
	if err != nil {
		if t.logger != nil {
			t.logger.Error("failed to retrieve documents", zap.Error(err))
		}
		return fmt.Sprintf(`{"status":"error","results":[],"message":"检索失败: %s"}`, err.Error()), nil
	}

	// 过滤空文档，避免返回 id/content/metadata 全空的无效结果。
	results := make([]map[string]interface{}, 0, len(docs))
	for _, doc := range docs {
		content := strings.TrimSpace(doc.Content)
		if content == "" {
			continue
		}

		result := map[string]interface{}{
			"id":       doc.ID,
			"content":  content,
			"metadata": doc.MetaData,
		}
		results = append(results, result)
		if len(results) >= params.TopK {
			break
		}
	}

	if len(results) == 0 {
		resultJSON, _ := json.Marshal(map[string]interface{}{
			"status":  "success",
			"results": []map[string]interface{}{},
			"count":   0,
			"message": "知识库中暂无可用历史案例",
		})
		return string(resultJSON), nil
	}

	resultJSON, err := json.Marshal(map[string]interface{}{
		"status":  "success",
		"results": results,
		"count":   len(results),
		"message": fmt.Sprintf("找到 %d 个相关案例", len(results)),
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal results: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("vector search completed",
			zap.String("query", params.Query),
			zap.Int("results", len(results)))
	}

	return string(resultJSON), nil
}
