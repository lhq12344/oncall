package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"go_agent/internal/logic/ai/models"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// IntentAnalysisTool 意图分析工具
type IntentAnalysisTool struct {
	chatModel *models.ChatModel
	embedder  embedding.Embedder
	llmEnable bool
	logger    *zap.Logger
}

type intentProfile struct {
	Type       string
	Label      string
	Prototype  string
	Keywords   []string
	Confidence float64
}

var gameIntentProfiles = []intentProfile{
	{
		Type:       "account_issue",
		Label:      "账号问题",
		Prototype:  "账号被盗、账号找回、绑定手机、实名认证、账号安全、角色丢失等账号相关问题",
		Keywords:   []string{"账号", "帐号", "被盗", "盗号", "找回", "登录账号", "换绑", "绑定", "实名", "实名认证", "角色没了", "角色丢失", "密码"},
		Confidence: 0.86,
	},
	{
		Type:       "login_server_issue",
		Label:      "登录或服务器问题",
		Prototype:  "无法登录、服务器进不去、排队、掉线、连接失败、区服异常等登录服务器问题",
		Keywords:   []string{"登录", "登陆", "进不去", "服务器", "区服", "掉线", "连接失败", "卡登录", "维护", "排队", "闪退", "加载不出来"},
		Confidence: 0.84,
	},
	{
		Type:       "payment_issue",
		Label:      "充值或支付问题",
		Prototype:  "充值不到账、扣费未到账、支付失败、月卡礼包未发放、订单异常等充值支付问题",
		Keywords:   []string{"充值", "支付", "扣费", "不到账", "未到账", "没到账", "订单", "月卡", "礼包", "点券", "钻石", "余额", "付款", "付了钱"},
		Confidence: 0.88,
	},
	{
		Type:       "refund_issue",
		Label:      "退款问题",
		Prototype:  "申请退款、误充值退款、未成年退款、重复扣款退款等退款相关诉求",
		Keywords:   []string{"退款", "退钱", "退费", "误充", "误充值", "未成年退款", "重复扣款", "撤销支付"},
		Confidence: 0.88,
	},
	{
		Type:       "ban_appeal",
		Label:      "封禁申诉",
		Prototype:  "账号封禁、禁言、处罚、外挂误封、申诉解封等处罚申诉问题",
		Keywords:   []string{"封号", "封禁", "禁言", "处罚", "误封", "解封", "申诉", "外挂", "开挂", "作弊", "违规"},
		Confidence: 0.9,
	},
	{
		Type:       "gameplay_question",
		Label:      "玩法问题",
		Prototype:  "副本打法、任务流程、装备培养、角色技能、阵容搭配等玩法咨询",
		Keywords:   []string{"怎么玩", "玩法", "副本", "任务", "装备", "技能", "角色", "阵容", "升级", "怎么打", "攻略", "关卡", "培养", "战力"},
		Confidence: 0.82,
	},
	{
		Type:       "event_reward_issue",
		Label:      "活动或奖励问题",
		Prototype:  "活动规则、奖励未到账、兑换码、签到、邮件奖励、排行榜奖励等活动奖励问题",
		Keywords:   []string{"活动", "奖励", "兑换码", "礼包码", "签到", "邮件", "排行榜", "赛季", "补偿", "领取", "未发放", "没发"},
		Confidence: 0.84,
	},
	{
		Type:       "bug_performance_issue",
		Label:      "BUG 或性能问题",
		Prototype:  "游戏 BUG、卡顿、闪退、黑屏、异常报错、穿模、数据错误等问题反馈",
		Keywords:   []string{"bug", "BUG", "卡顿", "闪退", "黑屏", "报错", "异常", "崩溃", "掉帧", "显示错误", "数据错误", "卡死", "不能点"},
		Confidence: 0.84,
	},
	{
		Type:       "complaint_feedback",
		Label:      "投诉或建议",
		Prototype:  "投诉客服、反馈体验、建议优化、举报玩家、表达强烈不满等投诉反馈",
		Keywords:   []string{"投诉", "反馈", "建议", "举报", "差评", "不满意", "客服", "处理态度", "垃圾游戏", "太离谱"},
		Confidence: 0.82,
	},
	{
		Type:       "human_service",
		Label:      "人工客服",
		Prototype:  "要求转人工、找真人客服、需要人工处理、联系人工客服等人工服务诉求",
		Keywords:   []string{"人工", "真人", "转人工", "人工客服", "客服介入", "找客服", "电话客服", "人工处理"},
		Confidence: 0.86,
	},
	{
		Type:       "general_chat",
		Label:      "一般对话",
		Prototype:  "问候、感谢、日常交流、非业务咨询等一般对话",
		Keywords:   []string{"你好", "您好", "谢谢", "感谢", "在吗", "辛苦了", "好的", "明白"},
		Confidence: 0.72,
	},
}

func NewIntentAnalysisTool(chatModel *models.ChatModel, embedder embedding.Embedder, logger *zap.Logger, enableLLM ...bool) tool.BaseTool {
	llmEnable := false
	if len(enableLLM) > 0 {
		llmEnable = enableLLM[0]
	}

	return &IntentAnalysisTool{
		chatModel: chatModel,
		embedder:  embedder,
		llmEnable: llmEnable,
		logger:    logger,
	}
}

func (t *IntentAnalysisTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "intent_analysis",
		Desc: "分析玩家输入的主要客服诉求。返回游戏客服意图类型、置信度、语义熵、缺失信息和内部路由建议。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"user_input": {
				Type:     schema.String,
				Desc:     "玩家输入文本",
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

	// 2. Embedding 语义分类（提高准确性）
	semanticIntent, semanticConfidence, err := t.semanticEmbeddingClassification(ctx, in.UserInput)
	if err == nil && semanticConfidence > keywordConfidence {
		intentType = semanticIntent
		keywordConfidence = semanticConfidence
	}

	// 3. 可选 LLM 增强分类（默认关闭，避免二次模型调用开销）
	if t.llmEnable {
		llmIntent, llmConfidence, llmErr := t.llmEnhancedClassification(ctx, in.UserInput)
		if llmErr != nil {
			if t.logger != nil {
				t.logger.Warn("LLM classification failed, fallback to non-LLM classifiers",
					zap.Error(llmErr))
			}
		} else if llmConfidence > keywordConfidence {
			intentType = llmIntent
			keywordConfidence = llmConfidence
		}
	}

	// 4. 语义熵计算（评估意图明确程度）
	entropy := t.calculateSemanticEntropy(in.UserInput, intentType, keywordConfidence)

	// 5. 置信度评估
	finalConfidence := t.evaluateConfidence(keywordConfidence, entropy, len(in.UserInput))

	// 6. 判断是否收敛
	converged := entropy < 0.6 && finalConfidence > 0.7

	// 7. 识别缺失信息和内部路由建议
	missingInfo := t.identifyMissingInfo(in.UserInput, intentType)
	intentLabel := intentLabel(intentType)
	routingHint := routingHint(intentType, converged, missingInfo)

	result := map[string]interface{}{
		"intent_type":  intentType,
		"intent_label": intentLabel,
		"confidence":   finalConfidence,
		"entropy":      entropy,
		"converged":    converged,
		"missing_info": missingInfo,
		"routing_hint": routingHint,
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
		t.logger.Info("customer_intent_analysis_result",
			zap.String("intent_type", intentType),
			zap.String("intent_label", intentLabel),
			zap.Float64("confidence", finalConfidence),
			zap.Float64("entropy", entropy),
			zap.Bool("converged", converged),
			zap.String("routing_hint", routingHint),
			zap.Strings("missing_info", missingInfo),
			zap.Float64("keyword_confidence", keywordConfidence),
			zap.String("result_json", string(out)),
			zap.String("input_preview", previewToolInput(in.UserInput)))
	}

	return string(out), nil
}

// semanticEmbeddingClassification 基于 embedding 的语义分类
func (t *IntentAnalysisTool) semanticEmbeddingClassification(ctx context.Context, input string) (string, float64, error) {
	if t.embedder == nil {
		return "", 0, fmt.Errorf("embedder not available")
	}

	texts := make([]string, 0, len(gameIntentProfiles)+1)
	texts = append(texts, input)
	for _, profile := range gameIntentProfiles {
		texts = append(texts, profile.Prototype)
	}

	vectors, err := t.embedder.EmbedStrings(ctx, texts)
	if err != nil {
		return "", 0, err
	}
	if len(vectors) != len(texts) {
		return "", 0, fmt.Errorf("embedding result size mismatch")
	}

	inputVec := vectors[0]
	bestIntent := "other"
	bestScore := 0.0
	for idx, profile := range gameIntentProfiles {
		score := cosineSimilarity(inputVec, vectors[idx+1])
		if score > bestScore {
			bestScore = score
			bestIntent = profile.Type
		}
	}

	if bestScore <= 0 {
		return "other", 0.5, nil
	}

	return bestIntent, bestScore, nil
}

// keywordMatching 基于关键词的快速分类
func (t *IntentAnalysisTool) keywordMatching(input string) (string, float64) {
	lower := strings.ToLower(input)

	scores := make(map[string]float64)
	for _, pattern := range gameIntentProfiles {
		matchCount := 0
		for _, keyword := range pattern.Keywords {
			if strings.Contains(lower, strings.ToLower(keyword)) {
				matchCount++
			}
		}
		if matchCount > 0 {
			score := pattern.Confidence * 0.88
			if matchCount == 2 {
				score = pattern.Confidence * 0.98
			} else if matchCount >= 3 {
				score = pattern.Confidence + 0.06
			}
			if score > 1 {
				score = 1
			}
			scores[pattern.Type] = score
		}
	}

	maxScore := 0.0
	bestIntent := "other"
	for intent, score := range scores {
		if score > maxScore {
			maxScore = score
			bestIntent = intent
		}
	}

	if maxScore == 0 {
		return "other", 0.5
	}

	return bestIntent, maxScore
}

// llmEnhancedClassification 使用 LLM 进行增强分类
func (t *IntentAnalysisTool) llmEnhancedClassification(ctx context.Context, input string) (string, float64, error) {
	if t.chatModel == nil {
		return "", 0, fmt.Errorf("chat model not available")
	}

	prompt := fmt.Sprintf(`分析以下玩家输入的游戏客服主要诉求类型，从以下类别中选择一个：
- account_issue: 账号问题
- login_server_issue: 登录或服务器问题
- payment_issue: 充值或支付问题
- refund_issue: 退款问题
- ban_appeal: 封禁申诉
- gameplay_question: 玩法问题
- event_reward_issue: 活动或奖励问题
- bug_performance_issue: BUG 或性能问题
- complaint_feedback: 投诉或建议
- human_service: 人工客服
- general_chat: 一般对话
- other: 其他

玩家输入："%s"

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
	vagueWords := []string{"有问题", "不行", "不对", "怎么办", "看看", "查查", "处理一下", "帮我看下"}
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

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}

	dot := 0.0
	normA := 0.0
	normB := 0.0
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	score := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

// identifyMissingInfo 识别缺失的关键信息
func (t *IntentAnalysisTool) identifyMissingInfo(input string, intent string) []string {
	lower := strings.ToLower(input)
	missing := []string{}

	switch intent {
	case "account_issue":
		if !containsAny(lower, []string{"账号", "帐号", "角色", "uid", "id"}) {
			missing = append(missing, "账号或角色标识")
		}
	case "login_server_issue":
		if !containsAny(lower, []string{"服务器", "区服", "ios", "安卓", "android"}) {
			missing = append(missing, "区服和设备平台")
		}
	case "payment_issue":
		if !containsAny(lower, []string{"订单", "充值", "支付", "扣费"}) {
			missing = append(missing, "订单号或支付渠道")
		}
	case "refund_issue":
		if !containsAny(lower, []string{"订单", "退款", "支付", "充值"}) {
			missing = append(missing, "退款订单信息")
		}
	case "ban_appeal":
		if !containsAny(lower, []string{"封号", "封禁", "禁言", "处罚"}) {
			missing = append(missing, "处罚类型或提示截图")
		}
	case "bug_performance_issue":
		if !containsAny(lower, []string{"机型", "设备", "版本", "报错", "截图"}) {
			missing = append(missing, "设备、版本或异常截图")
		}
	case "event_reward_issue":
		if !containsAny(lower, []string{"活动", "奖励", "兑换码", "礼包", "邮件"}) {
			missing = append(missing, "活动或奖励名称")
		}
	case "other":
		if len([]rune(strings.TrimSpace(input))) < 12 {
			missing = append(missing, "更完整的问题背景")
		}
	}

	return missing
}

func containsAny(input string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(input, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func intentLabel(intentType string) string {
	for _, profile := range gameIntentProfiles {
		if profile.Type == intentType {
			return profile.Label
		}
	}
	return "其他"
}

func routingHint(intentType string, converged bool, missingInfo []string) string {
	if !converged || len(missingInfo) > 0 {
		return "clarify_or_collect_details"
	}
	switch intentType {
	case "gameplay_question", "event_reward_issue":
		return "prefer_knowledge_retrieve"
	case "payment_issue", "refund_issue", "ban_appeal", "account_issue", "login_server_issue", "bug_performance_issue":
		return "customer_support_resolution"
	case "complaint_feedback", "human_service":
		return "escalate_or_human_service"
	case "general_chat":
		return "direct_answer"
	default:
		return "general_handling"
	}
}

func previewToolInput(input string) string {
	runes := []rune(strings.TrimSpace(input))
	if len(runes) > 120 {
		return string(runes[:120])
	}
	return string(runes)
}
