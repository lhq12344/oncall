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

// KnowledgeIndexTool 知识索引工具
type KnowledgeIndexTool struct {
	indexer *milvusIndexer.Indexer
	logger  *zap.Logger
}

func NewKnowledgeIndexTool(idx *milvusIndexer.Indexer, logger *zap.Logger) tool.BaseTool {
	return &KnowledgeIndexTool{
		indexer: idx,
		logger:  logger,
	}
}

func (t *KnowledgeIndexTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "knowledge_index",
		Desc: "将新的故障案例索引到知识库。输入案例内容，自动分块、向量化并存储。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"content": {
				Type:     schema.String,
				Desc:     "案例内容（包含问题描述、解决方案、结果）",
				Required: true,
			},
			"metadata": {
				Type:     schema.Object,
				Desc:     "案例元数据（标签、时间、成功率等）",
				Required: false,
			},
		}),
	}, nil
}

func (t *KnowledgeIndexTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	// 解析参数
	var params struct {
		Content  string                 `json:"content"`
		Metadata map[string]interface{} `json:"metadata"`
	}

	if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
		return "", fmt.Errorf("invalid parameters: %w", err)
	}

	// 参数验证
	if params.Content == "" {
		return "", fmt.Errorf("content is required")
	}

	t.logger.Info("knowledge index invoked",
		zap.Int("content_length", len(params.Content)))

	// 检查 indexer 是否可用
	if t.indexer == nil {
		return `{"error": "知识索引功能尚未配置（需要 Milvus）", "indexed": false}`, nil
	}

	// 创建文档
	doc := &schema.Document{
		ID:       fmt.Sprintf("doc_%d", len(params.Content)), // 简单的 ID 生成
		Content:  params.Content,
		MetaData: params.Metadata,
	}

	// 调用 Milvus Indexer
	ids, err := t.indexer.Store(ctx, []*schema.Document{doc})
	if err != nil {
		t.logger.Error("indexing failed", zap.Error(err))
		return "", fmt.Errorf("indexing failed: %w", err)
	}

	output := map[string]interface{}{
		"indexed":    true,
		"document_id": ids[0],
		"message":    "案例已成功索引到知识库",
	}

	outputJSON, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("failed to marshal output: %w", err)
	}

	t.logger.Info("knowledge index completed",
		zap.String("document_id", ids[0]))

	return string(outputJSON), nil
}
