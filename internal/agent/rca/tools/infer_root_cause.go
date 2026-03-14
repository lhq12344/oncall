package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// InferRootCauseTool 根因推理工具
type InferRootCauseTool struct {
	chatModel *models.ChatModel
	logger    *zap.Logger
}

// RootCauseHypothesis 根因假设
type RootCauseHypothesis struct {
	Service      string   `json:"service"`
	Component    string   `json:"component"`
	Cause        string   `json:"cause"`
	Evidence     []string `json:"evidence"`
	Confidence   float64  `json:"confidence"`
	Reasoning    string   `json:"reasoning"`
	Verification string   `json:"verification"` // 验证建议
}

// RootCauseInference 根因推理结果
type RootCauseInference struct {
	FaultNode       string                `json:"fault_node"`  // 故障节点
	Hypotheses      []RootCauseHypothesis `json:"hypotheses"`  // 根因假设列表
	TopCause        *RootCauseHypothesis  `json:"top_cause"`   // 最可能的根因
	SearchPath      []string              `json:"search_path"` // 搜索路径
	TotalHypotheses int                   `json:"total_hypotheses"`
}

func NewInferRootCauseTool(chatModel *models.ChatModel, logger *zap.Logger) tool.BaseTool {
	return &InferRootCauseTool{
		chatModel: chatModel,
		logger:    logger,
	}
}

func (t *InferRootCauseTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "infer_root_cause",
		Desc: "推理故障的根本原因。基于依赖图和信号关联，沿调用链反向搜索，生成根因假设。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"fault_node": {
				Type:     schema.String,
				Desc:     "故障节点（服务名称）",
				Required: true,
			},
			"dependency_graph": {
				Type:     schema.Object,
				Desc:     "依赖图（JSON 对象）",
				Required: true,
			},
			"correlations": {
				Type:     schema.Array,
				Desc:     "信号关联结果（JSON 数组）",
				Required: true,
			},
		}),
	}, nil
}

func (t *InferRootCauseTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		FaultNode       string                 `json:"fault_node"`
		DependencyGraph map[string]interface{} `json:"dependency_graph"`
		Correlations    []interface{}          `json:"correlations"`
	}

	var in args
	if err := unmarshalRCAArgsLenient(argumentsInJSON, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if in.FaultNode == "" {
		return "", fmt.Errorf("fault_node is required")
	}

	// 1. 反向搜索依赖链
	searchPath := t.backwardSearch(in.FaultNode, in.DependencyGraph)

	// 2. 生成根因假设
	hypotheses := t.generateHypotheses(in.FaultNode, searchPath, in.Correlations)

	// 3. 使用 LLM 增强推理
	if t.chatModel != nil {
		enhancedHypotheses, err := t.enhanceWithLLM(ctx, in.FaultNode, hypotheses, in.Correlations)
		if err == nil {
			hypotheses = enhancedHypotheses
		} else if t.logger != nil {
			t.logger.Warn("LLM enhancement failed", zap.Error(err))
		}
	}

	// 4. 按置信度排序
	sort.Slice(hypotheses, func(i, j int) bool {
		return hypotheses[i].Confidence > hypotheses[j].Confidence
	})

	// 5. 选择最可能的根因
	var topCause *RootCauseHypothesis
	if len(hypotheses) > 0 {
		topCause = &hypotheses[0]
	}

	result := &RootCauseInference{
		FaultNode:       in.FaultNode,
		Hypotheses:      hypotheses,
		TopCause:        topCause,
		SearchPath:      searchPath,
		TotalHypotheses: len(hypotheses),
	}

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		topCauseService := ""
		if topCause != nil {
			topCauseService = topCause.Service
		}

		t.logger.Info("root cause inference completed",
			zap.String("fault_node", in.FaultNode),
			zap.Int("hypotheses", len(hypotheses)),
			zap.String("top_cause", topCauseService))
	}

	return string(output), nil
}

// backwardSearch 反向搜索依赖链
func (t *InferRootCauseTool) backwardSearch(faultNode string, graph map[string]interface{}) []string {
	searchPath := []string{faultNode}

	// 提取 nodes
	nodesData, ok := graph["nodes"].(map[string]interface{})
	if !ok {
		return searchPath
	}

	// BFS 反向搜索
	visited := make(map[string]bool)
	queue := []string{faultNode}
	visited[faultNode] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// 获取当前节点
		nodeData, ok := nodesData[current].(map[string]interface{})
		if !ok {
			continue
		}

		// 获取依赖（上游服务）
		deps, ok := nodeData["dependencies"].([]interface{})
		if !ok {
			continue
		}

		for _, dep := range deps {
			depName, ok := dep.(string)
			if !ok {
				continue
			}

			if !visited[depName] {
				visited[depName] = true
				queue = append(queue, depName)
				searchPath = append(searchPath, depName)
			}
		}
	}

	return searchPath
}

// generateHypotheses 生成根因假设
func (t *InferRootCauseTool) generateHypotheses(faultNode string, searchPath []string, correlations []interface{}) []RootCauseHypothesis {
	hypotheses := []RootCauseHypothesis{}

	// 为搜索路径中的每个节点生成假设
	for _, service := range searchPath {
		// 收集该服务的证据
		evidence := t.collectEvidence(service, correlations)

		if len(evidence) == 0 {
			continue
		}

		// 生成假设
		hypothesis := RootCauseHypothesis{
			Service:      service,
			Component:    "unknown",
			Cause:        t.inferCauseType(evidence),
			Evidence:     evidence,
			Confidence:   t.calculateHypothesisConfidence(service, faultNode, evidence, searchPath),
			Reasoning:    fmt.Sprintf("Service %s shows abnormal signals that may have caused the fault in %s", service, faultNode),
			Verification: fmt.Sprintf("Check logs and metrics of %s, verify if the issue started here", service),
		}

		hypotheses = append(hypotheses, hypothesis)
	}

	return hypotheses
}

// collectEvidence 收集证据
func (t *InferRootCauseTool) collectEvidence(service string, correlations []interface{}) []string {
	evidence := []string{}

	for _, corr := range correlations {
		corrMap, ok := corr.(map[string]interface{})
		if !ok {
			continue
		}

		// 检查 signal1
		if sig1, ok := corrMap["signal1"].(map[string]interface{}); ok {
			if svc, ok := sig1["service"].(string); ok && svc == service {
				if msg, ok := sig1["message"].(string); ok {
					evidence = append(evidence, msg)
				}
			}
		}

		// 检查 signal2
		if sig2, ok := corrMap["signal2"].(map[string]interface{}); ok {
			if svc, ok := sig2["service"].(string); ok && svc == service {
				if msg, ok := sig2["message"].(string); ok {
					evidence = append(evidence, msg)
				}
			}
		}
	}

	return evidence
}

// inferCauseType 推断原因类型
func (t *InferRootCauseTool) inferCauseType(evidence []string) string {
	allEvidence := strings.Join(evidence, " ")
	lower := strings.ToLower(allEvidence)

	if strings.Contains(lower, "cpu") || strings.Contains(lower, "memory") {
		return "resource_exhaustion"
	}
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "connection") {
		return "network_issue"
	}
	if strings.Contains(lower, "error") || strings.Contains(lower, "exception") {
		return "application_error"
	}
	if strings.Contains(lower, "database") || strings.Contains(lower, "db") {
		return "database_issue"
	}

	return "unknown"
}

// calculateHypothesisConfidence 计算假设置信度
func (t *InferRootCauseTool) calculateHypothesisConfidence(service, faultNode string, evidence []string, searchPath []string) float64 {
	confidence := 0.5

	// 证据越多，置信度越高
	confidence += float64(len(evidence)) * 0.1
	if confidence > 1.0 {
		confidence = 1.0
	}

	// 如果是故障节点本身，置信度降低
	if service == faultNode {
		confidence *= 0.7
	}

	// 如果是直接依赖，置信度提高
	if len(searchPath) > 1 && searchPath[1] == service {
		confidence *= 1.2
	}

	// 限制在 [0, 1]
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// enhanceWithLLM 使用 LLM 增强推理
func (t *InferRootCauseTool) enhanceWithLLM(ctx context.Context, faultNode string, hypotheses []RootCauseHypothesis, correlations []interface{}) ([]RootCauseHypothesis, error) {
	if len(hypotheses) == 0 {
		return hypotheses, nil
	}

	// 构建 prompt
	prompt := fmt.Sprintf(`分析以下故障根因假设，评估每个假设的合理性：

故障节点：%s

假设列表：
`, faultNode)

	for i, hyp := range hypotheses {
		prompt += fmt.Sprintf("\n%d. 服务：%s\n   原因：%s\n   证据：%v\n", i+1, hyp.Service, hyp.Cause, hyp.Evidence)
	}

	prompt += `
请分析：
1. 哪个假设最可能是根因？
2. 为什么？
3. 如何验证？

返回 JSON 格式：
{
  "top_hypothesis_index": 0,
  "reasoning": "分析原因",
  "verification_steps": ["步骤1", "步骤2"]
}`

	resp, err := t.chatModel.Client.Generate(ctx, []*schema.Message{
		schema.UserMessage(prompt),
	})
	if err != nil {
		return hypotheses, err
	}

	content := resp.Content
	if content == "" {
		return hypotheses, fmt.Errorf("empty response from LLM")
	}

	// 尝试解析 LLM 响应
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		jsonStr := content[start : end+1]

		var llmResult struct {
			TopHypothesisIndex int      `json:"top_hypothesis_index"`
			Reasoning          string   `json:"reasoning"`
			VerificationSteps  []string `json:"verification_steps"`
		}

		if err := json.Unmarshal([]byte(jsonStr), &llmResult); err == nil {
			// 更新最可能的假设
			if llmResult.TopHypothesisIndex >= 0 && llmResult.TopHypothesisIndex < len(hypotheses) {
				hypotheses[llmResult.TopHypothesisIndex].Confidence = 0.95
				hypotheses[llmResult.TopHypothesisIndex].Reasoning = llmResult.Reasoning
				if len(llmResult.VerificationSteps) > 0 {
					hypotheses[llmResult.TopHypothesisIndex].Verification = strings.Join(llmResult.VerificationSteps, "; ")
				}
			}
		}
	}

	return hypotheses, nil
}
