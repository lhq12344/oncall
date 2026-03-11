package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// KnowledgeRetrieveTool 知识检索工具
type KnowledgeRetrieveTool struct {
	retriever einoretriever.Retriever
	logger    *zap.Logger
}

func NewKnowledgeRetrieveTool(rtr einoretriever.Retriever, logger *zap.Logger) tool.BaseTool {
	return &KnowledgeRetrieveTool{
		retriever: rtr,
		logger:    logger,
	}
}

func (t *KnowledgeRetrieveTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "knowledge_retrieve",
		Desc: "从 Milvus 向量库检索与查询最相近的知识文本片段，可用于补充对话上下文。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "检索查询文本",
				Required: true,
			},
			"top_k": {
				Type:     schema.Integer,
				Desc:     "返回结果数量，默认 3，最大 10",
				Required: false,
			},
		}),
	}, nil
}

func (t *KnowledgeRetrieveTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	in.Query = strings.TrimSpace(in.Query)
	if in.Query == "" {
		return "", fmt.Errorf("query is required")
	}

	if in.TopK <= 0 {
		in.TopK = 3
	}
	if in.TopK > 10 {
		in.TopK = 10
	}

	if t.retriever == nil {
		return `{"status":"degraded","results":[],"count":0,"message":"knowledge retriever unavailable"}`, nil
	}

	docs, err := t.retriever.Retrieve(ctx, in.Query, einoretriever.WithTopK(in.TopK))
	if err != nil {
		if t.logger != nil {
			t.logger.Error("knowledge retrieve failed",
				zap.String("query", in.Query),
				zap.Int("top_k", in.TopK),
				zap.Error(err))
		}
		return fmt.Sprintf(`{"status":"error","results":[],"count":0,"message":"%s"}`, escapeJSONString(err.Error())), nil
	}

	type resultItem struct {
		ID      string         `json:"id,omitempty"`
		Content string         `json:"content"`
		Score   float64        `json:"score"`
		Meta    map[string]any `json:"meta,omitempty"`
	}

	items := make([]resultItem, 0, len(docs))
	for _, doc := range docs {
		if doc == nil {
			continue
		}

		content := strings.TrimSpace(doc.Content)
		if len([]rune(content)) > 500 {
			content = string([]rune(content)[:500]) + "..."
		}

		items = append(items, resultItem{
			ID:      doc.ID,
			Content: content,
			Score:   doc.Score(),
			Meta:    doc.MetaData,
		})
	}

	result := map[string]any{
		"status":  "success",
		"query":   in.Query,
		"count":   len(items),
		"results": items,
	}

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("knowledge retrieve completed",
			zap.String("query", in.Query),
			zap.Int("top_k", in.TopK),
			zap.Int("result_count", len(items)))
	}

	return string(output), nil
}

func escapeJSONString(s string) string {
	b, _ := json.Marshal(s)
	if len(b) >= 2 {
		return string(b[1 : len(b)-1])
	}
	return s
}
