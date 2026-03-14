package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	ID      string         `json:"id,omitempty"`
	Content string         `json:"content"`
	Score   float64        `json:"score"`
	Meta    map[string]any `json:"meta,omitempty"`
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
		Desc: "从 ops 案例库检索历史故障处理记录（与通用知识库隔离）。",
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
	if in.TopK <= 0 {
		in.TopK = 3
	}
	if in.TopK > 10 {
		in.TopK = 10
	}

	if t.retriever == nil {
		items := t.retrieveLocalFinalReports(in.Query, in.TopK)
		out, err := json.Marshal(map[string]any{
			"status":  "degraded",
			"query":   in.Query,
			"count":   len(items),
			"results": items,
			"message": "ops case retriever unavailable, fallback to local final reports",
		})
		if err != nil {
			return "", fmt.Errorf("failed to marshal degraded result: %w", err)
		}
		return string(out), nil
	}

	localReports := t.retrieveLocalFinalReports(in.Query, in.TopK)
	docs, err := t.retriever.Retrieve(ctx, in.Query, einoretriever.WithTopK(in.TopK))
	if err != nil {
		if strings.Contains(err.Error(), "extra output fields") {
			if t.logger != nil {
				t.logger.Warn("ops case retrieve schema mismatch, fallback to empty result",
					zap.String("query", in.Query),
					zap.Int("top_k", in.TopK),
					zap.Error(err))
			}
			out, marshalErr := json.Marshal(map[string]any{
				"status":  "degraded",
				"query":   in.Query,
				"count":   len(localReports),
				"results": localReports,
				"message": "ops case collection schema mismatch, fallback to local final reports",
			})
			if marshalErr != nil {
				return "", fmt.Errorf("failed to marshal degraded result: %w", marshalErr)
			}
			return string(out), nil
		}
		if t.logger != nil {
			t.logger.Error("ops case retrieve failed",
				zap.String("query", in.Query),
				zap.Int("top_k", in.TopK),
				zap.Error(err))
		}
		out, marshalErr := json.Marshal(map[string]any{
			"status":  "error",
			"query":   in.Query,
			"count":   len(localReports),
			"results": localReports,
			"message": err.Error(),
		})
		if marshalErr != nil {
			return "", fmt.Errorf("failed to marshal error result: %w", marshalErr)
		}
		return string(out), nil
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
			ID:      doc.ID,
			Content: content,
			Score:   doc.Score(),
			Meta:    meta,
		})
	}

	items = mergeOpsCaseResults(localReports, items, in.TopK)

	out, err := json.Marshal(map[string]any{
		"status":  "success",
		"query":   in.Query,
		"count":   len(items),
		"results": items,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}
	return string(out), nil
}

// retrieveLocalFinalReports 从本地完整技术报告目录中检索与 query 相关的报告。
// 输入：query、topK。
// 输出：按相关度排序的完整技术报告结果。
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
			Meta: map[string]any{
				"type":   "ops_final_report",
				"source": "local_file",
				"path":   path,
				"title":  entry.Name(),
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

// mergeOpsCaseResults 合并本地完整技术报告与向量检索结果，并优先返回 ops_final_report。
// 输入：localReports、retrieved、topK。
// 输出：去重后的结果列表。
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
		leftType := strings.TrimSpace(fmt.Sprintf("%v", out[i].Meta["type"]))
		rightType := strings.TrimSpace(fmt.Sprintf("%v", out[j].Meta["type"]))
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

// splitQueryKeywords 将查询文本拆成关键词。
// 输入：query。
// 输出：关键词列表。
func splitQueryKeywords(query string) []string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(query)), func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', ',', '，', '。', ':', '：', ';', '；', '/', '\\', '-', '_':
			return true
		default:
			return false
		}
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

// scoreLocalReport 计算本地技术报告与查询的粗略相关度。
// 输入：query、keywords、filename、content。
// 输出：相关度分数。
func scoreLocalReport(query string, keywords []string, filename, content string) float64 {
	filenameLower := strings.ToLower(filename)
	contentLower := strings.ToLower(content)
	score := 0.0

	if query != "" && strings.Contains(contentLower, strings.ToLower(query)) {
		score += 5
	}
	if query != "" && strings.Contains(filenameLower, strings.ToLower(query)) {
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
	if strings.Contains(content, "## 运维技术报告") {
		score += 1
	}
	return score
}

// stripMarkdownFrontMatter 去掉 markdown 文件头部 front matter。
// 输入：markdown 文本。
// 输出：去掉 front matter 后的正文。
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

// extractOpsCaseContent 提取运维案例文本。
// 输入：Milvus 检索返回的 Document。
// 输出：优先 doc.Content，其次从 metadata 的常见文本字段回退提取。
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
