package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"go_agent/internal/rag"

	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type OpsCaseRetrieveTool struct {
	retriever einoretriever.Retriever
	logger    *zap.Logger
}

type resultItem struct {
	ID            string         `json:"id,omitempty"`
	Content       string         `json:"content"`
	Score         float64        `json:"score"`
	Source        string         `json:"source,omitempty"`
	RetrievalPath []string       `json:"retrieval_path,omitempty"`
	Meta          map[string]any `json:"meta,omitempty"`
}

func NewOpsCaseRetrieveTool(rtr einoretriever.Retriever, logger *zap.Logger) tool.BaseTool {
	return &OpsCaseRetrieveTool{
		retriever: rtr,
		logger:    logger,
	}
}

func (t *OpsCaseRetrieveTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "ops_case_retrieve",
		Desc: "Retrieve historical ops incidents and final reports from the hybrid RAG index.",
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

func (t *OpsCaseRetrieveTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
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
	in.TopK = rag.DefaultConfig().CapFinalTopK(in.TopK)

	localReports := t.retrieveLocalFinalReports(in.Query, in.TopK)
	if t.retriever == nil {
		return marshalOpsContext(in.Query, "degraded", []string{"ops case retriever unavailable, fallback to local final reports"}, localReports)
	}

	if provider, ok := t.retriever.(retrievedContextProvider); ok {
		result, err := provider.RetrieveContext(ctx, in.Query, in.TopK)
		if err != nil {
			return "", err
		}
		items := mergeOpsCaseResults(localReports, opsItemsFromRAG(result.Results), in.TopK)
		result.Results = ragResultsFromOpsItems(items)
		result.Count = len(result.Results)
		truncateOpsNonFinalReports(result.Results, 500)
		return marshalRetrievedContext(result)
	}

	docs, err := t.retriever.Retrieve(ctx, in.Query, einoretriever.WithTopK(in.TopK))
	if err != nil {
		if strings.Contains(err.Error(), "extra output fields") {
			if t.logger != nil {
				t.logger.Warn("ops case retrieve schema mismatch, fallback to local final reports",
					zap.String("query", in.Query),
					zap.Int("top_k", in.TopK),
					zap.Error(err))
			}
			return marshalOpsContext(in.Query, "degraded", []string{"ops case collection schema mismatch, fallback to local final reports"}, localReports)
		}
		if t.logger != nil {
			t.logger.Error("ops case retrieve failed",
				zap.String("query", in.Query),
				zap.Int("top_k", in.TopK),
				zap.Error(err))
		}
		return marshalOpsContext(in.Query, "error", []string{err.Error()}, localReports)
	}

	items := make([]resultItem, 0, len(docs))
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		content := extractOpsCaseContent(doc)
		if content == "" {
			continue
		}
		meta := doc.MetaData
		if meta == nil {
			meta = map[string]any{}
		}
		if reportType := strings.TrimSpace(fmt.Sprintf("%v", meta["type"])); reportType != "ops_final_report" && len([]rune(content)) > 500 {
			content = string([]rune(content)[:500]) + "..."
		}
		items = append(items, resultItem{
			ID:            doc.ID,
			Content:       content,
			Score:         doc.Score(),
			Source:        "embedding_legacy",
			RetrievalPath: []string{"embedding"},
			Meta:          meta,
		})
	}

	items = mergeOpsCaseResults(localReports, items, in.TopK)
	return marshalOpsContext(in.Query, "success", nil, items)
}

func marshalOpsContext(query, status string, reasons []string, items []resultItem) (string, error) {
	result := &rag.RetrievedContext{
		Status:           status,
		Profile:          rag.ProfileOpsCase,
		Query:            query,
		RewrittenQueries: []string{query},
		DegradedReasons:  reasons,
		Results:          ragResultsFromOpsItems(items),
	}
	return marshalRetrievedContext(result)
}

func opsItemsFromRAG(results []rag.RetrievedResult) []resultItem {
	out := make([]resultItem, 0, len(results))
	for _, item := range results {
		out = append(out, resultItem{
			ID:            item.ID,
			Content:       item.Content,
			Score:         item.Score,
			Source:        item.Source,
			RetrievalPath: item.RetrievalPath,
			Meta:          item.Meta,
		})
	}
	return out
}

func ragResultsFromOpsItems(items []resultItem) []rag.RetrievedResult {
	out := make([]rag.RetrievedResult, 0, len(items))
	for _, item := range items {
		source := strings.TrimSpace(item.Source)
		if source == "" {
			source = strings.TrimSpace(fmt.Sprintf("%v", item.Meta["retrieval_source"]))
		}
		if source == "" {
			source = strings.TrimSpace(fmt.Sprintf("%v", item.Meta["source"]))
		}
		out = append(out, rag.RetrievedResult{
			ID:            item.ID,
			Content:       item.Content,
			Score:         item.Score,
			Source:        source,
			RetrievalPath: resolveOpsRetrievalPath(item, source),
			Meta:          item.Meta,
		})
	}
	return out
}

func resolveOpsRetrievalPath(item resultItem, source string) []string {
	if len(item.RetrievalPath) > 0 {
		return append([]string(nil), item.RetrievalPath...)
	}
	if source != "" {
		if strings.Contains(source, "local") {
			return []string{"local"}
		}
		if strings.Contains(source, "bm25") {
			return []string{"bm25"}
		}
		if strings.Contains(source, "embedding") {
			return []string{"embedding"}
		}
	}
	if strings.TrimSpace(fmt.Sprintf("%v", item.Meta["path"])) != "" {
		return []string{"local"}
	}
	return nil
}

func truncateOpsNonFinalReports(results []rag.RetrievedResult, maxRunes int) {
	for i := range results {
		meta := results[i].Meta
		sourceType := strings.TrimSpace(fmt.Sprintf("%v", meta["source_type"]))
		if sourceType == "" {
			sourceType = strings.TrimSpace(fmt.Sprintf("%v", meta["type"]))
		}
		if sourceType == "ops_final_report" {
			continue
		}
		if len([]rune(results[i].Content)) > maxRunes {
			results[i].Content = string([]rune(results[i].Content)[:maxRunes]) + "..."
		}
	}
}

// retrieveLocalFinalReports searches locally archived final reports.
func (t *OpsCaseRetrieveTool) retrieveLocalFinalReports(query string, topK int) []resultItem {
	dir := filepath.Join("logs", "ops_reports")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	query = strings.TrimSpace(query)
	keywords := splitQueryKeywords(query)
	results := make([]resultItem, 0, len(entries))

	for _, entry := range entries {
		if entry == nil || entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(stripMarkdownFrontMatter(string(body)))
		if content == "" {
			continue
		}
		score := scoreLocalReport(query, keywords, entry.Name(), content)
		if score <= 0 {
			continue
		}
		results = append(results, resultItem{
			ID:      "file:" + entry.Name(),
			Content: content,
			Score:   score,
			Source:  "local_file",
			Meta: map[string]any{
				"type":        "ops_final_report",
				"source_type": "ops_final_report",
				"source":      "local_file",
				"path":        path,
				"title":       entry.Name(),
			},
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ID > results[j].ID
		}
		return results[i].Score > results[j].Score
	})

	if topK > 0 && len(results) > topK {
		return results[:topK]
	}
	return results
}

func mergeOpsCaseResults(localReports, retrieved []resultItem, topK int) []resultItem {
	out := make([]resultItem, 0, len(localReports)+len(retrieved))
	seen := make(map[string]struct{}, len(localReports)+len(retrieved))

	appendItem := func(item resultItem) {
		key := strings.TrimSpace(item.ID)
		if key == "" {
			key = strings.TrimSpace(item.Content)
		}
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}

	for _, item := range localReports {
		appendItem(item)
	}
	for _, item := range retrieved {
		appendItem(item)
	}

	sort.SliceStable(out, func(i, j int) bool {
		leftType := strings.TrimSpace(fmt.Sprintf("%v", firstOpsMeta(out[i].Meta, "source_type", "type")))
		rightType := strings.TrimSpace(fmt.Sprintf("%v", firstOpsMeta(out[j].Meta, "source_type", "type")))
		if leftType != rightType {
			if leftType == "ops_final_report" {
				return true
			}
			if rightType == "ops_final_report" {
				return false
			}
		}
		return out[i].Score > out[j].Score
	})

	if topK > 0 && len(out) > topK {
		return out[:topK]
	}
	return out
}

func firstOpsMeta(meta map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := meta[key]; ok {
			return value
		}
	}
	return ""
}

func splitQueryKeywords(query string) []string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(query)), func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",，。:：;；/\\-_()[]{}<>|", r)
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func scoreLocalReport(query string, keywords []string, filename, content string) float64 {
	filenameLower := strings.ToLower(filename)
	contentLower := strings.ToLower(content)
	queryLower := strings.ToLower(strings.TrimSpace(query))
	score := 0.0

	if queryLower != "" && strings.Contains(contentLower, queryLower) {
		score += 5
	}
	if queryLower != "" && strings.Contains(filenameLower, queryLower) {
		score += 3
	}
	for _, keyword := range keywords {
		if strings.Contains(contentLower, keyword) {
			score += 1
		}
		if strings.Contains(filenameLower, keyword) {
			score += 0.5
		}
	}
	if strings.Contains(content, "运维技术报告") || strings.Contains(strings.ToLower(content), "final report") {
		score += 1
	}
	return score
}

func stripMarkdownFrontMatter(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---\n") {
		return content
	}
	parts := strings.SplitN(content, "\n---\n", 2)
	if len(parts) != 2 {
		return content
	}
	return strings.TrimSpace(parts[1])
}

func extractOpsCaseContent(doc *schema.Document) string {
	if doc == nil {
		return ""
	}
	if content := strings.TrimSpace(doc.Content); content != "" {
		return content
	}
	if doc.MetaData == nil {
		return ""
	}

	candidateKeys := []string{"content", "text", "case", "summary", "description"}
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
