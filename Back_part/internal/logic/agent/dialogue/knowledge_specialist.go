package dialogue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"go_agent/internal/logic/ai/models"

	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// KnowledgeSpecialistResult 子图输出结构，描述检索结果的解决情况。
type KnowledgeSpecialistResult struct {
	SolvedContexts   []string `json:"solved_contexts"`
	PendingQuestions []string `json:"pending_questions"`
	Status           string   `json:"status,omitempty"`
	ErrorSummary     string   `json:"error_summary,omitempty"`
}

const knowledgeSpecialistParallelRAGTimeout = 20 * time.Second
const knowledgeSpecialistMaxSubQuestions = 3

// buildKnowledgeSpecialistGraph 构建三节点子图：分解→并行RAG→评估。
// 输入类型：string（原始问题），输出类型：*KnowledgeSpecialistResult。
func buildKnowledgeSpecialistGraph(
	ctx context.Context,
	subgraphModel *models.ChatModel,
	retriever einoretriever.Retriever,
	logger *zap.Logger,
) (compose.Runnable[string, *KnowledgeSpecialistResult], error) {
	g := compose.NewGraph[string, *KnowledgeSpecialistResult]()

	// --- Decompose Node ---
	if err := g.AddLambdaNode("decompose_node", compose.InvokableLambda(
		buildDecomposeNode(ctx, subgraphModel, logger),
	)); err != nil {
		return nil, fmt.Errorf("failed to add decompose_node: %w", err)
	}

	// --- Parallel RAG Node ---
	if err := g.AddLambdaNode("parallel_rag_node", compose.InvokableLambda(
		buildParallelRAGNode(retriever, logger),
	)); err != nil {
		return nil, fmt.Errorf("failed to add parallel_rag_node: %w", err)
	}

	// --- Evaluate Node ---
	if err := g.AddLambdaNode("evaluate_node", compose.InvokableLambda(
		buildEvaluateNode(ctx, subgraphModel, logger),
	)); err != nil {
		return nil, fmt.Errorf("failed to add evaluate_node: %w", err)
	}

	for _, edge := range [][2]string{
		{compose.START, "decompose_node"},
		{"decompose_node", "parallel_rag_node"},
		{"parallel_rag_node", "evaluate_node"},
		{"evaluate_node", compose.END},
	} {
		if err := g.AddEdge(edge[0], edge[1]); err != nil {
			return nil, fmt.Errorf("failed to add edge %s→%s: %w", edge[0], edge[1], err)
		}
	}

	runnable, err := g.Compile(ctx,
		compose.WithGraphName("knowledge_specialist"),
		compose.WithNodeTriggerMode(compose.AllPredecessor),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to compile knowledge specialist graph: %w", err)
	}
	return runnable, nil
}

// buildDecomposeNode 返回问题分解节点函数。
// 调用 LLM 将原始问题拆解为若干原子子问题（最多 knowledgeSpecialistMaxSubQuestions 个）。
func buildDecomposeNode(
	ctx context.Context,
	llm *models.ChatModel,
	logger *zap.Logger,
) func(context.Context, string) ([]string, error) {
	return func(nodeCtx context.Context, question string) ([]string, error) {
		if llm == nil {
			logDecomposeDecision(logger, question, "unsplit", "llm unavailable", []string{question}, "llm_unavailable")
			return []string{question}, nil
		}

		sysMsg := schema.SystemMessage(
			`你是问题分解专家。你的任务是判断用户原问题是否包含多个可以独立回答的真实诉求。` +
				`只能对用户原句做忠实分解，不得补充、扩写、脑补用户没有问的前提、步骤、注意事项、失败处理、限制条件或背景问题。` +
				`如果边界模糊、不能确定有多个独立诉求，必须不分解。` +
				`如果分解，每个子问题必须可独立回答，合并后必须完整覆盖原问题，最多3个。` +
				`只输出 JSON，不要输出 Markdown 或解释文字。` +
				`输出格式只能是：{"mode":"unsplit","reason":"...","questions":["<原问题>"]} 或 {"mode":"split","reason":"...","questions":["q1","q2"]}。`,
		)
		userMsg := schema.UserMessage(question)

		output, err := llm.Client.Generate(nodeCtx, []*schema.Message{sysMsg, userMsg})
		if err != nil {
			if logger != nil {
				logger.Warn("knowledge_specialist decompose node failed, falling back to original question",
					zap.Error(err))
			}
			logDecomposeDecision(logger, question, "unsplit", "decompose model failed", []string{question}, "generate_error")
			return []string{question}, nil
		}

		subQuestions, decision, fallback := parseDecomposeDecision(question, output.Content)
		logDecomposeDecision(logger, question, decision.Mode, decision.Reason, subQuestions, fallback)
		return subQuestions, nil
	}
}

type decomposeDecision struct {
	Mode      string   `json:"mode"`
	Reason    string   `json:"reason"`
	Questions []string `json:"questions"`
}

func parseDecomposeDecision(original string, raw string) ([]string, decomposeDecision, string) {
	original = strings.TrimSpace(original)
	decision := decomposeDecision{
		Mode:      "unsplit",
		Reason:    "fallback to original question",
		Questions: []string{original},
	}

	payload, ok := extractJSONObject(raw)
	if !ok {
		decision.Reason = "decompose output is not valid JSON"
		return []string{original}, decision, "invalid_json"
	}
	if err := json.Unmarshal([]byte(payload), &decision); err != nil {
		decision.Mode = "unsplit"
		decision.Reason = "decompose JSON parse failed"
		decision.Questions = []string{original}
		return []string{original}, decision, "invalid_json"
	}

	decision.Mode = strings.ToLower(strings.TrimSpace(decision.Mode))
	decision.Reason = strings.TrimSpace(decision.Reason)
	if decision.Mode == "" {
		decision.Mode = "unsplit"
	}
	if decision.Mode != "split" && decision.Mode != "unsplit" {
		decision.Mode = "unsplit"
		decision.Questions = []string{original}
		return []string{original}, decision, "invalid_mode"
	}
	if decision.Mode == "unsplit" {
		decision.Questions = []string{original}
		return []string{original}, decision, ""
	}

	questions, fallback := validateSplitQuestions(original, decision.Questions)
	if fallback != "" {
		decision.Mode = "unsplit"
		decision.Questions = []string{original}
		if decision.Reason == "" {
			decision.Reason = "split failed quality gate"
		}
		return []string{original}, decision, fallback
	}
	decision.Questions = questions
	return questions, decision, ""
}

func extractJSONObject(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return "", false
	}
	return raw[start : end+1], true
}

func validateSplitQuestions(original string, questions []string) ([]string, string) {
	cleaned, duplicated := cleanQuestionList(questions)
	switch {
	case len(cleaned) == 0:
		return nil, "empty_questions"
	case duplicated:
		return nil, "duplicate_questions"
	case len(cleaned) == 1:
		return nil, "ambiguous"
	case len(cleaned) > knowledgeSpecialistMaxSubQuestions:
		return nil, "too_many_questions"
	case !splitQuestionsPassQualityGate(original, cleaned):
		return nil, "quality_gate_failed"
	default:
		return cleaned, ""
	}
}

func cleanQuestionList(questions []string) ([]string, bool) {
	seen := make(map[string]bool, len(questions))
	cleaned := make([]string, 0, len(questions))
	duplicated := false
	for _, question := range questions {
		question = strings.TrimSpace(question)
		if question == "" {
			continue
		}
		key := normalizeQuestionForMatch(question)
		if seen[key] {
			duplicated = true
			continue
		}
		seen[key] = true
		cleaned = append(cleaned, question)
	}
	return cleaned, duplicated
}

func splitQuestionsPassQualityGate(original string, questions []string) bool {
	originalTerms := significantTerms(original)
	if len(originalTerms) == 0 {
		return false
	}
	covered := make(map[string]bool, len(originalTerms))
	for _, question := range questions {
		terms := significantTerms(question)
		if len(terms) == 0 {
			return false
		}
		overlap := false
		for term := range terms {
			if originalTerms[term] {
				covered[term] = true
				overlap = true
			}
		}
		if !overlap {
			return false
		}
	}
	return len(covered) > 0
}

func significantTerms(text string) map[string]bool {
	terms := make(map[string]bool)
	var ascii strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			ascii.WriteRune(r)
		default:
			flushASCIITerm(terms, &ascii)
			if unicode.Is(unicode.Han, r) {
				terms[string(r)] = true
			}
		}
	}
	flushASCIITerm(terms, &ascii)
	return terms
}

func flushASCIITerm(terms map[string]bool, b *strings.Builder) {
	if b.Len() == 0 {
		return
	}
	term := b.String()
	if len(term) > 1 {
		terms[term] = true
	}
	b.Reset()
}

func logDecomposeDecision(logger *zap.Logger, original string, mode string, reason string, subQuestions []string, fallback string) {
	if logger == nil {
		return
	}
	fields := []zap.Field{
		zap.String("original", original),
		zap.String("decompose_mode", mode),
		zap.String("decompose_reason", reason),
		zap.Strings("sub_questions", subQuestions),
		zap.Int("sub_questions_count", len(subQuestions)),
	}
	if fallback != "" {
		fields = append(fields, zap.String("decompose_fallback", fallback))
	}
	logger.Info("knowledge_specialist decomposed question", fields...)
}

// buildParallelRAGNode 返回并行检索节点函数。
// 对每个子问题并发调用 retriever，使用 errgroup 控制并发和超时。
func buildParallelRAGNode(
	retriever einoretriever.Retriever,
	logger *zap.Logger,
) func(context.Context, []string) (map[string][]schema.Document, error) {
	return func(nodeCtx context.Context, subQuestions []string) (map[string][]schema.Document, error) {
		result := make(map[string][]schema.Document, len(subQuestions))

		if len(subQuestions) == 0 {
			return result, nil
		}
		if retriever == nil {
			return nil, fmt.Errorf("knowledge retriever unavailable")
		}

		type entry struct {
			question string
			docs     []schema.Document
		}

		ragCtx, cancel := context.WithTimeout(nodeCtx, knowledgeSpecialistParallelRAGTimeout)
		defer cancel()

		eg, egCtx := errgroup.WithContext(ragCtx)
		results := make(chan entry, len(subQuestions))

		for _, q := range subQuestions {
			q := q
			eg.Go(func() error {
				docs, err := retriever.Retrieve(egCtx, q)
				if err != nil {
					if logger != nil {
						logger.Error("knowledge_specialist RAG failed for sub-question",
							zap.String("question", q),
							zap.Error(err))
					}
					return fmt.Errorf("knowledge retrieve failed for %q: %w", q, err)
				}
				docsVal := make([]schema.Document, len(docs))
				for i, d := range docs {
					docsVal[i] = *d
				}
				if logger != nil {
					logRAGMatches(logger, q, docsVal)
				}
				results <- entry{question: q, docs: docsVal}
				return nil
			})
		}

		if err := eg.Wait(); err != nil {
			return nil, fmt.Errorf("parallel RAG errgroup: %w", err)
		}
		close(results)

		for e := range results {
			result[e.question] = e.docs
		}

		if logger != nil {
			logger.Info("knowledge_specialist parallel RAG complete",
				zap.Int("sub_questions", len(subQuestions)),
				zap.Int("results", len(result)))
		}
		return result, nil
	}
}

func logRAGMatches(logger *zap.Logger, question string, docs []schema.Document) {
	if logger == nil {
		return
	}
	if len(docs) == 0 {
		logger.Info("knowledge_specialist rag matches",
			zap.String("question", question),
			zap.Int("match_count", 0))
		return
	}

	topScore := docs[0].Score()
	topIndex := 0
	for i := 1; i < len(docs); i++ {
		if score := docs[i].Score(); score > topScore {
			topScore = score
			topIndex = i
		}
	}

	matchSummaries := make([]string, 0, len(docs))
	for i, doc := range docs {
		content := strings.TrimSpace(doc.Content)
		if len([]rune(content)) > 80 {
			content = string([]rune(content)[:80]) + "…"
		}
		matchSummaries = append(matchSummaries, fmt.Sprintf(
			"#%d score=%.6f id=%s content=%q",
			i+1, doc.Score(), doc.ID, content,
		))
	}

	logger.Info("knowledge_specialist rag matches",
		zap.String("question", question),
		zap.Int("match_count", len(docs)),
		zap.Int("top_match_index", topIndex+1),
		zap.Float64("top_match_score", topScore),
		zap.Strings("matches", matchSummaries))
}

// buildEvaluateNode 返回评估节点函数。
// 调用 LLM 逐个判断每个子问题的文档是否充分，分类为 solved/pending。
func buildEvaluateNode(
	ctx context.Context,
	llm *models.ChatModel,
	logger *zap.Logger,
) func(context.Context, map[string][]schema.Document) (*KnowledgeSpecialistResult, error) {
	return func(nodeCtx context.Context, ragResults map[string][]schema.Document) (*KnowledgeSpecialistResult, error) {
		result := &KnowledgeSpecialistResult{
			SolvedContexts:   []string{},
			PendingQuestions: []string{},
		}
		userLanguage := userLanguageFromState(nodeCtx)

		if len(ragResults) == 0 {
			result.Status = "empty"
			return result, nil
		}

		if llm == nil {
			for q, docs := range ragResults {
				if len(docs) > 0 {
					result.SolvedContexts = append(result.SolvedContexts, formatDocsContext(q, docs))
				} else {
					result.PendingQuestions = append(result.PendingQuestions, q)
				}
			}
			result.Status = statusFromKnowledgeResult(result)
			return result, nil
		}

		// 构建评估 prompt
		var evalLines []string
		for q, docs := range ragResults {
			docTexts := make([]string, 0, len(docs))
			for _, d := range docs {
				if strings.TrimSpace(d.Content) != "" {
					docTexts = append(docTexts, d.Content)
				}
			}
			if len(docTexts) == 0 {
				evalLines = append(evalLines, fmt.Sprintf("问题：%s\n文档：（无检索结果）", q))
			} else {
				evalLines = append(evalLines, fmt.Sprintf("问题：%s\n文档：\n%s", q, strings.Join(docTexts, "\n---\n")))
			}
		}

		sysMsg := schema.SystemMessage(
			fmt.Sprintf(`判断这段中文 RAG 资料是否能准确回答用户的 %s 问题。请忽略语言差异，仅关注语义匹配度。`, userLanguage) +
				`对每个子问题，判断提供的文档是否能充分回答该问题。` +
				`若能充分回答，输出：SOLVED:<子问题文本>` +
				`若不能充分回答或无文档，输出：PENDING:<子问题文本>` +
				`每行一个结果，不要输出其他内容。`,
		)
		userMsg := schema.UserMessage(strings.Join(evalLines, "\n\n"))

		output, err := llm.Client.Generate(nodeCtx, []*schema.Message{sysMsg, userMsg})
		if err != nil {
			if logger != nil {
				logger.Warn("knowledge_specialist evaluate node failed, falling back to doc-presence heuristic",
					zap.Error(err))
			}
			for q, docs := range ragResults {
				if len(docs) > 0 {
					result.SolvedContexts = append(result.SolvedContexts, formatDocsContext(q, docs))
				} else {
					result.PendingQuestions = append(result.PendingQuestions, q)
				}
			}
			result.Status = statusFromKnowledgeResult(result)
			result.ErrorSummary = err.Error()
			return result, nil
		}

		covered := make(map[string]bool, len(ragResults))
		lines := strings.Split(strings.TrimSpace(output.Content), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "SOLVED:") {
				q := strings.TrimPrefix(line, "SOLVED:")
				q = strings.TrimSpace(q)
				matchedQ, docs, ok := findRAGResultForQuestion(ragResults, q)
				if ok {
					covered[matchedQ] = true
				}
				if ok && hasDocumentContent(docs) {
					result.SolvedContexts = append(result.SolvedContexts, formatDocsContext(matchedQ, docs))
				} else {
					result.PendingQuestions = append(result.PendingQuestions, fallbackPendingQuestion(q, matchedQ))
				}
			} else if strings.HasPrefix(line, "PENDING:") {
				q := strings.TrimPrefix(line, "PENDING:")
				q = strings.TrimSpace(q)
				matchedQ, _, ok := findRAGResultForQuestion(ragResults, q)
				if ok {
					covered[matchedQ] = true
					result.PendingQuestions = append(result.PendingQuestions, matchedQ)
				} else if q != "" {
					result.PendingQuestions = append(result.PendingQuestions, q)
				}
			}
		}

		// 对 LLM 未覆盖的问题做保守回退：未被明确覆盖的子问题一律 pending，
		// 避免部分评估输出误导路由进入 answer_node。
		for q := range ragResults {
			if !covered[q] {
				result.PendingQuestions = append(result.PendingQuestions, q)
			}
		}

		if len(result.SolvedContexts)+len(result.PendingQuestions) == 0 {
			for q, docs := range ragResults {
				if len(docs) > 0 {
					result.SolvedContexts = append(result.SolvedContexts, formatDocsContext(q, docs))
				} else {
					result.PendingQuestions = append(result.PendingQuestions, q)
				}
			}
		}
		result.PendingQuestions = uniqueNonEmptyStrings(result.PendingQuestions)
		result.Status = statusFromKnowledgeResult(result)

		if logger != nil {
			logger.Info("knowledge_specialist evaluation complete",
				zap.Int("solved", len(result.SolvedContexts)),
				zap.Int("pending", len(result.PendingQuestions)))
		}
		return result, nil
	}
}

func formatDocsContext(question string, docs []schema.Document) string {
	texts := make([]string, 0, len(docs))
	for _, d := range docs {
		if strings.TrimSpace(d.Content) != "" {
			texts = append(texts, d.Content)
		}
	}
	return fmt.Sprintf("【%s】\n%s", question, strings.Join(texts, "\n"))
}

func findRAGResultForQuestion(ragResults map[string][]schema.Document, question string) (string, []schema.Document, bool) {
	question = strings.TrimSpace(question)
	if question == "" {
		return "", nil, false
	}
	if docs, ok := ragResults[question]; ok {
		return question, docs, true
	}
	normalized := normalizeQuestionForMatch(question)
	for q, docs := range ragResults {
		if normalizeQuestionForMatch(q) == normalized {
			return q, docs, true
		}
	}
	return "", nil, false
}

func normalizeQuestionForMatch(question string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(question)), " ")
}

func hasDocumentContent(docs []schema.Document) bool {
	for _, doc := range docs {
		if strings.TrimSpace(doc.Content) != "" {
			return true
		}
	}
	return false
}

func fallbackPendingQuestion(rawQuestion string, matchedQuestion string) string {
	if strings.TrimSpace(matchedQuestion) != "" {
		return strings.TrimSpace(matchedQuestion)
	}
	return strings.TrimSpace(rawQuestion)
}

func uniqueNonEmptyStrings(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func statusFromKnowledgeResult(result *KnowledgeSpecialistResult) string {
	if result == nil {
		return "empty"
	}
	switch {
	case len(result.PendingQuestions) > 0:
		return "partial"
	case len(result.SolvedContexts) > 0:
		return "success"
	default:
		return "empty"
	}
}

// ---- Tool 封装 ----

type knowledgeSearchExpertTool struct {
	runnable compose.Runnable[string, *KnowledgeSpecialistResult]
	logger   *zap.Logger
}

// NewKnowledgeSearchExpertTool 将 KnowledgeSpecialist 子图编译并封装为 Tool。
func NewKnowledgeSearchExpertTool(
	ctx context.Context,
	subgraphModel *models.ChatModel,
	retriever einoretriever.Retriever,
	logger *zap.Logger,
) (tool.BaseTool, error) {
	runnable, err := buildKnowledgeSpecialistGraph(ctx, subgraphModel, retriever, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to build knowledge specialist graph: %w", err)
	}
	return &knowledgeSearchExpertTool{runnable: runnable, logger: logger}, nil
}

func (t *knowledgeSearchExpertTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "knowledge_search_expert",
		Desc: "该工具能深度拆解复杂问题，通过并行 RAG 检索并自评质量，返回高可信度的知识背景。" +
			"返回 JSON：{\"solved_contexts\":[...],\"pending_questions\":[...]}",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"question": {
				Type:     schema.String,
				Desc:     "需要检索的问题或复合问题",
				Required: true,
			},
		}),
	}, nil
}

func (t *knowledgeSearchExpertTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	if t.logger != nil {
		t.logger.Info("knowledge_search_expert tool arguments",
			zap.String("arguments_in_json", argumentsInJSON))
	}
	var args struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("knowledge_search_expert: invalid args: %w", err)
	}
	if strings.TrimSpace(args.Question) == "" {
		return "", fmt.Errorf("knowledge_search_expert: question is required")
	}

	result, err := t.runnable.Invoke(ctx, args.Question)
	if err != nil {
		if t.logger != nil {
			t.logger.Error("knowledge_search_expert failed", zap.Error(err))
		}
		return "", fmt.Errorf("knowledge_search_expert failed: %w", err)
	}

	out, _ := json.Marshal(result)
	return string(out), nil
}
