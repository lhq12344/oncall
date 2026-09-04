package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go_agent/internal/rag"
	"go_agent/internal/telemetry"

	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type retrievedContextProvider interface {
	RetrieveContext(ctx context.Context, query string, topK int) (*rag.RetrievedContext, error)
}

// KnowledgeRetrieveTool retrieves business knowledge chunks.
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
		Desc: "Retrieve relevant business knowledge chunks from the hybrid RAG index.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "retrieval query",
				Required: true,
			},
			"top_k": {
				Type:     schema.Integer,
				Desc:     "final result count, default 3, max 10",
				Required: false,
			},
		}),
	}, nil
}

func (t *KnowledgeRetrieveTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	finish := func(error) {}
	if info := telemetry.ContextFrom(ctx); info.Recorder != nil {
		finish = info.Recorder.StartContext(ctx, "rag.retrieve", nil)
	}
	var operationErr error
	defer func() { finish(operationErr) }()
	type args struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		operationErr = err
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	in.Query = strings.TrimSpace(in.Query)
	if in.Query == "" {
		operationErr = fmt.Errorf("query is required")
		return "", operationErr
	}
	in.TopK = rag.DefaultConfig().CapFinalTopK(in.TopK)

	if t.retriever == nil {
		return marshalAndLogRetrievedContext(t.logger, "knowledge_retrieve", &rag.RetrievedContext{
			Status:           "degraded",
			Profile:          rag.ProfileKnowledge,
			Query:            in.Query,
			RewrittenQueries: []string{in.Query},
			DegradedReasons:  []string{"knowledge retriever unavailable"},
			Results:          []rag.RetrievedResult{},
		})
	}

	if provider, ok := t.retriever.(retrievedContextProvider); ok {
		result, err := provider.RetrieveContext(ctx, in.Query, in.TopK)
		if err != nil {
			operationErr = err
			return "", err
		}
		truncateRetrievedContent(result.Results, 500)
		return marshalAndLogRetrievedContext(t.logger, "knowledge_retrieve", result)
	}

	docs, err := t.retriever.Retrieve(ctx, in.Query, einoretriever.WithTopK(in.TopK))
	if err != nil {
		if strings.Contains(err.Error(), "extra output fields") {
			if t.logger != nil {
				t.logger.Warn("knowledge retrieve schema mismatch, fallback to empty result",
					zap.String("query", in.Query),
					zap.Int("top_k", in.TopK),
					zap.Error(err))
			}
			return marshalAndLogRetrievedContext(t.logger, "knowledge_retrieve", &rag.RetrievedContext{
				Status:           "degraded",
				Profile:          rag.ProfileKnowledge,
				Query:            in.Query,
				RewrittenQueries: []string{in.Query},
				DegradedReasons:  []string{"knowledge collection schema mismatch"},
				Results:          []rag.RetrievedResult{},
			})
		}
		if t.logger != nil {
			t.logger.Error("knowledge retrieve failed",
				zap.String("query", in.Query),
				zap.Int("top_k", in.TopK),
				zap.Error(err))
		}
		return marshalAndLogRetrievedContext(t.logger, "knowledge_retrieve", &rag.RetrievedContext{
			Status:           "error",
			Profile:          rag.ProfileKnowledge,
			Query:            in.Query,
			RewrittenQueries: []string{in.Query},
			DegradedReasons:  []string{err.Error()},
			Results:          []rag.RetrievedResult{},
		})
	}

	results := make([]rag.RetrievedResult, 0, len(docs))
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		content := extractKnowledgeContent(doc)
		if content == "" {
			continue
		}
		if len([]rune(content)) > 500 {
			content = string([]rune(content)[:500]) + "..."
		}
		results = append(results, rag.RetrievedResult{
			ID:            doc.ID,
			Content:       content,
			Score:         doc.Score(),
			Source:        "embedding",
			RetrievalPath: []string{"embedding"},
			Meta:          doc.MetaData,
		})
	}

	result := &rag.RetrievedContext{
		Status:           "success",
		Profile:          rag.ProfileKnowledge,
		Query:            in.Query,
		RewrittenQueries: []string{in.Query},
		Count:            len(results),
		Results:          results,
	}
	return marshalAndLogRetrievedContext(t.logger, "knowledge_retrieve", result)
}

func marshalRetrievedContext(result *rag.RetrievedContext) (string, error) {
	if result == nil {
		result = &rag.RetrievedContext{Status: "degraded", Results: []rag.RetrievedResult{}}
	}
	result.Count = len(result.Results)
	if result.Results == nil {
		result.Results = []rag.RetrievedResult{}
	}
	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}
	return string(output), nil
}

func marshalAndLogRetrievedContext(logger *zap.Logger, toolName string, result *rag.RetrievedContext) (string, error) {
	if result != nil && result.CandidateCounts == nil {
		result.CandidateCounts = map[string]int{rag.CandidateCountStageFinalDocs: len(result.Results)}
	}
	logRetrievedContext(logger, toolName, result)
	return marshalRetrievedContext(result)
}

func logRetrievedContext(logger *zap.Logger, toolName string, result *rag.RetrievedContext) {
	if logger == nil || result == nil {
		return
	}
	logger.Info("rag retrieve completed",
		zap.String("tool", toolName),
		zap.String("profile", string(result.Profile)),
		zap.String("status", result.Status),
		zap.Int("final_count", len(result.Results)),
		zap.Float64("latency_ms", result.LatencyMS),
		zap.Any("candidate_counts", result.CandidateCounts),
		zap.Int("degraded_count", len(result.DegradedReasons)))
}

func truncateRetrievedContent(results []rag.RetrievedResult, maxRunes int) {
	if maxRunes <= 0 {
		return
	}
	for i := range results {
		if len([]rune(results[i].Content)) > maxRunes {
			results[i].Content = string([]rune(results[i].Content)[:maxRunes]) + "..."
		}
	}
}

// extractKnowledgeContent extracts text from an Eino document.
func extractKnowledgeContent(doc *schema.Document) string {
	if doc == nil {
		return ""
	}
	if content := strings.TrimSpace(doc.Content); content != "" {
		return content
	}
	if doc.MetaData == nil {
		return ""
	}

	candidateKeys := []string{"content", "text", "chunk", "summary", "description"}
	for _, key := range candidateKeys {
		raw, ok := doc.MetaData[key]
		if !ok {
			continue
		}
		text := strings.TrimSpace(fmt.Sprintf("%v", raw))
		if text != "" {
			return text
		}
	}
	return ""
}

func escapeJSONString(s string) string {
	b, _ := json.Marshal(s)
	if len(b) >= 2 {
		return string(b[1 : len(b)-1])
	}
	return s
}
