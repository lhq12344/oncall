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

// IntentAnalysisTool 意图分析工具
type IntentAnalysisTool struct {
	chatModel *models.ChatModel
	embedder  embedding.Embedder
	logger    *zap.Logger
}

func NewIntentAnalysisTool(chatModel *models.ChatModel, embedder embedding.Embedder, logger *zap.Logger) tool.BaseTool {
	return &IntentAnalysisTool{
		chatModel: chatModel,
		embedder:  embedder,
		logger:    logger,
	}
}

func (t *IntentAnalysisTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "intent_analysis",
		Desc: "分析用户输入的意图类型和明确程度。返回意图类型、置信度、语义熵等信息。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"user_input": {
				Type:     schema.String,
				Desc:     "用户输入文本",
				Required: true,
			},
		}),
	}, nil
}

func (t *IntentAnalysisTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		UserInput string `json:"user_input"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	in.UserInput = strings.TrimSpace(in.UserInput)
	if in.UserInput == "" {
		return "", fmt.Errorf("user_input is required")
	}

	// 1. 关键词匹配（快速初步分类）
	intentType, keywordConfidence := t.keywordMatching(in.UserInput)

	// 2. LLM 增强分类（提高准确性）
	llmIntent, llmConfidence, err := t.llmEnhancedClassification(ctx, in.UserInput)
	if err != nil {
		if t.logger != nil {
			t.logger.Warn("LLM classification failed, fallback to keyword matching",
				zap.Error(err))
		}
		// 降级到关键词匹配结果
	} else {
		// 如果 LLM 置信度更高，使用 LLM 结果
		if llmConfidence > keywordConfidence {
			intentType = llmIntent
			keywordConfidence = llmConfidence
		}
	}

	// 3. 语义熵计算（评估意图明确程度）
	entropy := t.calculateSemanticEntropy(in.UserInput, intentType, keywordConfidence)

	// 4. 置信度评估
	finalConfidence := t.evaluateConfidence(keywordConfidence, entropy, len(in.UserInput))

	// 5. 判断是否收敛
	converged := entropy < 0.6 && finalConfidence > 0.7

	// 6. 识别缺失信息
	missingInfo := t.identifyMissingInfo(in.UserInput, intentType)

	result := map[string]interface{}{
		"intent_type":  intentType,
		"confidence":   finalConfidence,
		"entropy":      entropy,
		"converged":    converged,
		"missing_info": missingInfo,
		"metadata": map[string]interface{}{
			"keyword_confidence": keywordConfidence,
			"input_length":       len(in.UserInput),
		},
	}

	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("intent analysis completed",
			zap.String("intent", intentType),
			zap.Float64("confidence", finalConfidence),
			zap.Float64("entropy", entropy),
			zap.Bool("converged", converged))
	}

	return string(out), nil
}

// keywordMatching 基于关键词的快速分类
func (t *IntentAnalysisTool) keywordMatching(input string) (string, float64) {
	lower := strings.ToLower(input)

	// 定义关键词模式
	patterns := map[string]struct {
		keywords   []string
		confidence float64
	}{
		"monitor": {
			keywords:   []string{"pod", "cpu", "内存", "监控", "指标", "资源", "使用率", "状态", "健康"},
			confidence: 0.8,
		},
		"diagnose": {
			keywords:   []string{"故障", "报错", "异常", "诊断", "问题", "错误", "失败", "超时", "崩溃"},
			confidence: 0.82,
		},
		"knowledge": {
			keywords:   []string{"案例", "最佳实践", "文档", "知识", "历史", "经验", "怎么", "如何"},
			confidence: 0.78,
		},
		"execute": {
			keywords:   []string{"重启", "执行", "修复", "变更", "部署", "回滚", "扩容", "缩容", "删除"},
			confidence: 0.8,
		},
	}

	// 计算每个意图的匹配分数
	scores := make(map[string]float64)
	for intent, pattern := range patterns {
		matchCount := 0
		for _, keyword := range pattern.keywords {
			if strings.Contains(lower, keyword) {
				matchCount++
			}
		}
		if matchCount > 0 {
			scores[intent] = pattern.confidence * (float64(matchCount) / float64(len(pattern.keywords)))
		}
	}

	// 找到最高分的意图
	maxScore := 0.0
	bestIntent := "general"
	for intent, score := range scores {
		if score > maxScore {
			maxScore = score
			bestIntent = intent
		}
	}

	// 如果没有匹配，返回 general
	if maxScore == 0 {
		return "general", 0.55
	}

	return bestIntent, maxScore
}

// llmEnhancedClassification 使用 LLM 进行增强分类
func (t *IntentAnalysisTool) llmEnhancedClassification(ctx context.Context, input string) (string, float64, error) {
	if t.chatModel == nil {
		return "", 0, fmt.Errorf("chat model not available")
	}

	prompt := fmt.Sprintf(`分析以下用户输入的意图类型，从以下类别中选择一个：
- monitor: 查看系统状态、指标、资源使用情况
- diagnose: 故障排查、问题分析、异常诊断
- knowledge: 查询历史案例、最佳实践、文档
- execute: 执行操作、修复问题、变更配置
- general: 通用对话、闲聊

用户输入："%s"

请只返回 JSON 格式：{"intent": "类型", "confidence": 0.0-1.0}`, input)

	resp, err := t.chatModel.Client.Generate(ctx, []*schema.Message{
		schema.UserMessage(prompt),
	})
	if err != nil {
		return "", 0, err
	}

	content := resp.Content
	if content == "" {
		return "", 0, fmt.Errorf("empty response from LLM")
	}

	// 解析 LLM 响应
	var result struct {
		Intent     string  `json:"intent"`
		Confidence float64 `json:"confidence"`
	}

	// 尝试提取 JSON
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		jsonStr := content[start : end+1]
		if err := json.Unmarshal([]byte(jsonStr), &result); err == nil {
			return result.Intent, result.Confidence, nil
		}
	}

	// 如果解析失败，降级到关键词匹配
	return "", 0, fmt.Errorf("failed to parse LLM response")
}

// calculateSemanticEntropy 计算语义熵（衡量意图的不确定性）
func (t *IntentAnalysisTool) calculateSemanticEntropy(input string, intent string, confidence float64) float64 {
	// 基础熵：基于置信度
	baseEntropy := -confidence * math.Log2(confidence)
	if confidence < 1.0 {
		baseEntropy -= (1 - confidence) * math.Log2(1-confidence)
	}

	// 长度惩罚：输入太短，熵增加
	lengthPenalty := 0.0
	if len(input) < 10 {
		lengthPenalty = 0.3
	} else if len(input) < 20 {
		lengthPenalty = 0.15
	}

	// 模糊词惩罚
	vagueWords := []string{"有问题", "不行", "不对", "怎么办", "看看", "查查"}
	vaguePenalty := 0.0
	lower := strings.ToLower(input)
	for _, word := range vagueWords {
		if strings.Contains(lower, word) {
			vaguePenalty += 0.2
		}
	}

	entropy := baseEntropy + lengthPenalty + vaguePenalty
	if entropy > 1.0 {
		entropy = 1.0
	}

	return entropy
}

// evaluateConfidence 综合评估置信度
func (t *IntentAnalysisTool) evaluateConfidence(baseConfidence, entropy float64, inputLength int) float64 {
	// 基础置信度
	confidence := baseConfidence

	// 熵惩罚：熵越高，置信度越低
	confidence *= (1.0 - entropy*0.3)

	// 长度奖励：输入越详细，置信度越高
	if inputLength > 50 {
		confidence *= 1.1
	} else if inputLength < 10 {
		confidence *= 0.8
	}

	// 限制在 [0, 1] 范围
	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < 0.0 {
		confidence = 0.0
	}

	return confidence
}

// identifyMissingInfo 识别缺失的关键信息
func (t *IntentAnalysisTool) identifyMissingInfo(input string, intent string) []string {
	lower := strings.ToLower(input)
	missing := []string{}

	switch intent {
	case "monitor":
		if !strings.Contains(lower, "pod") && !strings.Contains(lower, "服务") && !strings.Contains(lower, "节点") {
			missing = append(missing, "监控对象（Pod/服务/节点）")
		}
		if !strings.Contains(lower, "cpu") && !strings.Contains(lower, "内存") && !strings.Contains(lower, "指标") {
			missing = append(missing, "监控指标类型")
		}

	case "diagnose":
		if !strings.Contains(lower, "时间") && !strings.Contains(lower, "何时") {
			missing = append(missing, "故障发生时间")
		}
		if !strings.Contains(lower, "错误") && !strings.Contains(lower, "日志") {
			missing = append(missing, "错误信息或日志")
		}
		if !strings.Contains(lower, "影响") && !strings.Contains(lower, "范围") {
			missing = append(missing, "影响范围")
		}

	case "execute":
		if !strings.Contains(lower, "pod") && !strings.Contains(lower, "服务") {
			missing = append(missing, "操作目标")
		}
		if !strings.Contains(lower, "确认") && !strings.Contains(lower, "确定") {
			missing = append(missing, "操作确认")
		}
	}

	return missing
}