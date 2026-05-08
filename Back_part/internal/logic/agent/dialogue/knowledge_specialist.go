package dialogue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
}

const knowledgeSpecialistParallelRAGTimeout = 20 * time.Second
const knowledgeSpecialistMaxSubQuestions = 5

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
			return []string{question}, nil
		}

		sysMsg := schema.SystemMessage(
			`你是问题分解专家。将用户问题分解为若干原子子问题（最多5个），每行一个，不加序号前缀。` +
				`仅输出子问题列表，不输出其他任何内容。若问题本身已是原子问题，直接输出原问题即可。`,
		)
		userMsg := schema.UserMessage(question)

		output, err := llm.Client.Generate(nodeCtx, []*schema.Message{sysMsg, userMsg})
		if err != nil {
			if logger != nil {
				logger.Warn("knowledge_specialist decompose node failed, falling back to original question",
					zap.Error(err))
			}
			return []string{question}, nil
		}

		lines := strings.Split(strings.TrimSpace(output.Content), "\n")
		subQuestions := make([]string, 0, len(lines))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				subQuestions = append(subQuestions, line)
			}
		}

		if len(subQuestions) == 0 {
			return []string{question}, nil
		}
		if len(subQuestions) > knowledgeSpecialistMaxSubQuestions {
			subQuestions = subQuestions[:knowledgeSpecialistMaxSubQuestions]
		}

		if logger != nil {
			logger.Info("knowledge_specialist decomposed question",
				zap.String("original", question),
				zap.Int("sub_questions", len(subQuestions)))
		}
		return subQuestions, nil
	}
}

// buildParallelRAGNode 返回并行检索节点函数。
// 对每个子问题并发调用 retriever，使用 errgroup 控制并发和超时。
func buildParallelRAGNode(
	retriever einoretriever.Retriever,
	logger *zap.Logger,
) func(context.Context, []string) (map[string][]schema.Document, error) {
	return func(nodeCtx context.Context, subQuestions []string) (map[string][]schema.Document, error) {
		result := make(map[string][]schema.Document, len(subQuestions))

		if retriever == nil || len(subQuestions) == 0 {
			for _, q := range subQuestions {
				result[q] = nil
			}
			return result, nil
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
						logger.Warn("knowledge_specialist RAG failed for sub-question",
							zap.String("question", q),
							zap.Error(err))
					}
					results <- entry{question: q, docs: nil}
					return nil
				}
				docsVal := make([]schema.Document, len(docs))
				for i, d := range docs {
					docsVal[i] = *d
				}
				results <- entry{question: q, docs: docsVal}
				return nil
			})
		}

		if err := eg.Wait(); err != nil {
			return result, fmt.Errorf("parallel RAG errgroup: %w", err)
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
			return result, nil
		}

		lines := strings.Split(strings.TrimSpace(output.Content), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "SOLVED:") {
				q := strings.TrimPrefix(line, "SOLVED:")
				q = strings.TrimSpace(q)
				if docs, ok := ragResults[q]; ok && len(docs) > 0 {
					result.SolvedContexts = append(result.SolvedContexts, formatDocsContext(q, docs))
				} else {
					result.SolvedContexts = append(result.SolvedContexts, q)
				}
			} else if strings.HasPrefix(line, "PENDING:") {
				q := strings.TrimPrefix(line, "PENDING:")
				result.PendingQuestions = append(result.PendingQuestions, strings.TrimSpace(q))
			}
		}

		// 对 LLM 未覆盖的问题做回退处理
		if len(result.SolvedContexts)+len(result.PendingQuestions) == 0 {
			for q, docs := range ragResults {
				if len(docs) > 0 {
					result.SolvedContexts = append(result.SolvedContexts, formatDocsContext(q, docs))
				} else {
					result.PendingQuestions = append(result.PendingQuestions, q)
				}
			}
		}

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
			t.logger.Warn("knowledge_search_expert degraded", zap.Error(err))
		}
		fallback, _ := json.Marshal(&KnowledgeSpecialistResult{
			PendingQuestions: []string{args.Question},
		})
		return string(fallback), nil
	}

	out, _ := json.Marshal(result)
	return string(out), nil
}
