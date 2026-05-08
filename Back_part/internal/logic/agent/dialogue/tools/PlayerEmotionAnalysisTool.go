package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go_agent/internal/logic/ai/models"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// PlayerEmotionAnalysisTool 使用 LLM 分析玩家请求中的情绪状态和升级风险。
type PlayerEmotionAnalysisTool struct {
	chatModel *models.ChatModel
	logger    *zap.Logger
}

type emotionProfile struct {
	Type       string
	Label      string
	Keywords   []string
	Confidence float64
	Intensity  float64
}

type emotionAnalysisResult struct {
	Emotion          string   `json:"emotion"`
	EmotionLabel     string   `json:"emotion_label"`
	Label            string   `json:"label"`
	Intensity        float64  `json:"intensity"`
	Confidence       float64  `json:"confidence"`
	EscalationNeeded bool     `json:"escalation_needed"`
	Reasoning        string   `json:"reasoning,omitempty"`
	EvidenceKeywords []string `json:"evidence_keywords"`
	Keywords         []string `json:"keywords"`
	Source           string   `json:"source"`
}

type emotionLLMResult struct {
	Emotion          string   `json:"emotion"`
	Label            string   `json:"label"`
	EmotionLabel     string   `json:"emotion_label"`
	Intensity        float64  `json:"intensity"`
	Confidence       float64  `json:"confidence"`
	EscalationNeeded bool     `json:"escalation_needed"`
	Reasoning        string   `json:"reasoning"`
	Keywords         []string `json:"keywords"`
	EvidenceKeywords []string `json:"evidence_keywords"`
}

var playerEmotionProfiles = []emotionProfile{
	{
		Type:       "angry",
		Label:      "愤怒",
		Keywords:   []string{"生气", "愤怒", "气死", "垃圾", "离谱", "投诉", "差评", "骗子", "坑钱", "不处理", "太过分", "退钱"},
		Confidence: 0.88,
		Intensity:  0.86,
	},
	{
		Type:       "urgent",
		Label:      "急迫",
		Keywords:   []string{"急", "马上", "立刻", "赶紧", "快点", "现在", "急死", "尽快", "等不了", "火速"},
		Confidence: 0.86,
		Intensity:  0.82,
	},
	{
		Type:       "anxious",
		Label:      "焦虑",
		Keywords:   []string{"担心", "害怕", "慌", "怎么办", "完了", "丢了", "找不回", "不会没了吧", "着急"},
		Confidence: 0.82,
		Intensity:  0.72,
	},
	{
		Type:       "disappointed",
		Label:      "失望",
		Keywords:   []string{"失望", "心寒", "算了", "不想玩", "退游", "不好玩", "体验差", "没意思"},
		Confidence: 0.8,
		Intensity:  0.68,
	},
	{
		Type:       "confused",
		Label:      "困惑",
		Keywords:   []string{"不懂", "不明白", "为什么", "怎么回事", "什么意思", "看不懂", "不知道", "不会"},
		Confidence: 0.78,
		Intensity:  0.5,
	},
	{
		Type:       "happy",
		Label:      "快乐",
		Keywords:   []string{"开心", "高兴", "太好了", "谢谢", "感谢", "好耶", "舒服了", "满意", "赞"},
		Confidence: 0.8,
		Intensity:  0.45,
	},
}

// NewPlayerEmotionAnalysisTool 创建玩家情绪识别工具。
// chatModel 由应用启动时从 .env/config.yaml 构造；nil 时自动降级为关键词规则。
func NewPlayerEmotionAnalysisTool(chatModel *models.ChatModel, logger *zap.Logger) tool.BaseTool {
	return &PlayerEmotionAnalysisTool{
		chatModel: chatModel,
		logger:    logger,
	}
}

func (t *PlayerEmotionAnalysisTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "player_emotion_analysis",
		Desc: "深度分析玩家输入的情绪、不满程度、语境压力，判断是否需要高级客服介入。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"user_input": {
				Type:     schema.String,
				Desc:     "玩家原始输入文本",
				Required: true,
			},
		}),
	}, nil
}

func (t *PlayerEmotionAnalysisTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
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

	result, err := t.llmAnalyzeEmotion(ctx, in.UserInput)
	if err != nil {
		if t.logger != nil {
			t.logger.Warn("player emotion LLM analysis failed, fallback to keyword analysis",
				zap.Error(err),
				zap.String("input_preview", previewToolInput(in.UserInput)))
		}
		result = keywordEmotionAnalysis(in.UserInput)
	}

	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("player_emotion_analysis_result",
			zap.String("emotion", result.Emotion),
			zap.String("emotion_label", result.EmotionLabel),
			zap.Float64("confidence", result.Confidence),
			zap.Float64("intensity", result.Intensity),
			zap.Bool("escalation_needed", result.EscalationNeeded),
			zap.Strings("evidence_keywords", result.EvidenceKeywords),
			zap.String("source", result.Source),
			zap.String("reasoning", result.Reasoning),
			zap.String("result_json", string(out)),
			zap.String("input_preview", previewToolInput(in.UserInput)))
	}
	return string(out), nil
}

func (t *PlayerEmotionAnalysisTool) llmAnalyzeEmotion(ctx context.Context, input string) (emotionAnalysisResult, error) {
	if t.chatModel == nil || t.chatModel.Client == nil {
		return emotionAnalysisResult{}, fmt.Errorf("chat model not available")
	}

	resp, err := t.chatModel.Client.Generate(ctx, []*schema.Message{
		schema.SystemMessage(playerEmotionAnalysisPrompt()),
		schema.UserMessage(fmt.Sprintf("请分析以下玩家输入的内容：\n%q", input)),
	}, einomodel.WithTemperature(0), einomodel.WithMaxTokens(600))
	if err != nil {
		return emotionAnalysisResult{}, fmt.Errorf("generate emotion analysis: %w", err)
	}
	if resp == nil || strings.TrimSpace(resp.Content) == "" {
		return emotionAnalysisResult{}, fmt.Errorf("empty response from LLM")
	}

	analysis, err := parseEmotionLLMResult(resp.Content)
	if err != nil {
		return emotionAnalysisResult{}, err
	}
	return normalizeEmotionLLMResult(input, analysis)
}

func playerEmotionAnalysisPrompt() string {
	return `你是资深游戏客服专家和心理分析师。你的任务是分析玩家输入的情绪状态，并给出结构化评估。

情绪分类定义:
- angry: 愤怒，包含辱骂、强烈不满、威胁退款或投诉。
- urgent: 急迫，玩家非常焦急，要求立刻解决，如账号丢失、充值未到账。
- anxious: 焦虑，担心数据丢失或反馈等待时间长。
- disappointed: 失望，表达对游戏品质、平衡性的不满，有退游倾向。
- confused: 困惑，单纯疑问，没有明显负面情绪。
- happy: 快乐，表达感谢、赞美或满意。
- stable: 平稳，正常陈述或反馈。

输出要求:
1. 只返回合法 JSON，不要 Markdown，不要解释性前后缀。
2. 识别讽刺语气，例如“真有你的”“真是好游戏”，归类为 angry 或 disappointed。
3. escalation_needed 判定标准：若情绪为愤怒且强度 > 0.7，或涉及法律投诉、大规模退坑、账号被盗、充值不到账等核心利益受损，设为 true。
4. JSON 字段必须是:
{"emotion":"angry|urgent|anxious|disappointed|confused|happy|stable","label":"中文情绪标签","intensity":0.0,"confidence":0.0,"escalation_needed":false,"reasoning":"简短理由","keywords":["关键词"]}`
}

func parseEmotionLLMResult(content string) (emotionLLMResult, error) {
	var result emotionLLMResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &result); err == nil {
		return result, nil
	}

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return result, fmt.Errorf("failed to locate JSON object in LLM response")
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &result); err != nil {
		return result, fmt.Errorf("parse LLM emotion response: %w", err)
	}
	return result, nil
}

func normalizeEmotionLLMResult(input string, analysis emotionLLMResult) (emotionAnalysisResult, error) {
	emotion := strings.ToLower(strings.TrimSpace(analysis.Emotion))
	if !isKnownEmotion(emotion) {
		return emotionAnalysisResult{}, fmt.Errorf("unknown emotion from LLM: %q", analysis.Emotion)
	}

	label := strings.TrimSpace(analysis.Label)
	if label == "" {
		label = strings.TrimSpace(analysis.EmotionLabel)
	}
	if label == "" {
		label = emotionLabel(emotion)
	}

	keywords := compactEmotionKeywords(append(analysis.Keywords, analysis.EvidenceKeywords...))
	intensity := clamp01(analysis.Intensity)
	confidence := clamp01(analysis.Confidence)
	if confidence == 0 {
		confidence = 0.65
	}
	escalationNeeded := analysis.EscalationNeeded || shouldEscalateEmotion(input, emotion, intensity)

	return emotionAnalysisResult{
		Emotion:          emotion,
		EmotionLabel:     label,
		Label:            label,
		Intensity:        intensity,
		Confidence:       confidence,
		EscalationNeeded: escalationNeeded,
		Reasoning:        strings.TrimSpace(analysis.Reasoning),
		EvidenceKeywords: keywords,
		Keywords:         keywords,
		Source:           "llm",
	}, nil
}

func keywordEmotionAnalysis(input string) emotionAnalysisResult {
	emotion, confidence, intensity, evidence := classifyPlayerEmotion(input)
	escalationNeeded := shouldEscalateEmotion(input, emotion, intensity)

	label := emotionLabel(emotion)
	return emotionAnalysisResult{
		Emotion:          emotion,
		EmotionLabel:     label,
		Label:            label,
		Intensity:        intensity,
		Confidence:       confidence,
		EscalationNeeded: escalationNeeded,
		EvidenceKeywords: evidence,
		Keywords:         evidence,
		Source:           "keyword",
	}
}

func classifyPlayerEmotion(input string) (string, float64, float64, []string) {
	lower := strings.ToLower(input)
	bestEmotion := "stable"
	bestScore := 0.55
	bestIntensity := 0.28
	bestEvidence := []string{}

	for _, profile := range playerEmotionProfiles {
		evidence := matchedKeywords(lower, profile.Keywords)
		if len(evidence) == 0 {
			continue
		}
		ratio := float64(len(evidence)) / float64(len(profile.Keywords))
		score := profile.Confidence * (0.7 + ratio*0.3)
		if score > bestScore {
			bestScore = score
			bestEmotion = profile.Type
			bestIntensity = profile.Intensity + minFloat(0.12, ratio*0.2)
			bestEvidence = evidence
		}
	}

	if bestIntensity > 1 {
		bestIntensity = 1
	}
	return bestEmotion, bestScore, bestIntensity, bestEvidence
}

func matchedKeywords(input string, keywords []string) []string {
	matches := make([]string, 0)
	for _, keyword := range keywords {
		if strings.Contains(input, strings.ToLower(keyword)) {
			matches = append(matches, keyword)
		}
	}
	return matches
}

func shouldEscalateEmotion(input, emotion string, intensity float64) bool {
	lower := strings.ToLower(input)
	if (emotion == "angry" || emotion == "urgent" || emotion == "anxious") && intensity >= 0.78 {
		return true
	}
	return containsAny(lower, []string{"投诉", "退款", "退钱", "封号", "封禁", "误封", "申诉", "人工", "差评"})
}

func emotionLabel(emotion string) string {
	switch emotion {
	case "stable":
		return "稳定"
	case "happy":
		return "快乐"
	case "urgent":
		return "急迫"
	case "angry":
		return "愤怒"
	case "anxious":
		return "焦虑"
	case "confused":
		return "困惑"
	case "disappointed":
		return "失望"
	default:
		return "其他"
	}
}

func isKnownEmotion(emotion string) bool {
	switch emotion {
	case "stable", "happy", "urgent", "angry", "anxious", "confused", "disappointed":
		return true
	default:
		return false
	}
}

func compactEmotionKeywords(keywords []string) []string {
	seen := make(map[string]struct{}, len(keywords))
	out := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		if _, ok := seen[keyword]; ok {
			continue
		}
		seen[keyword] = struct{}{}
		out = append(out, keyword)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
