package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// PlayerEmotionAnalysisTool 分析玩家请求中的情绪状态和升级风险。
type PlayerEmotionAnalysisTool struct {
	logger *zap.Logger
}

type emotionProfile struct {
	Type       string
	Label      string
	Keywords   []string
	Confidence float64
	Intensity  float64
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
func NewPlayerEmotionAnalysisTool(logger *zap.Logger) tool.BaseTool {
	return &PlayerEmotionAnalysisTool{logger: logger}
}

func (t *PlayerEmotionAnalysisTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "player_emotion_analysis",
		Desc: "分析玩家请求中的情绪状态、强度和是否需要客服升级处理。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"user_input": {
				Type:     schema.String,
				Desc:     "玩家输入文本",
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

	emotion, confidence, intensity, evidence := classifyPlayerEmotion(in.UserInput)
	escalationNeeded := shouldEscalateEmotion(in.UserInput, emotion, intensity)

	result := map[string]any{
		"emotion":           emotion,
		"emotion_label":     emotionLabel(emotion),
		"intensity":         intensity,
		"confidence":        confidence,
		"escalation_needed": escalationNeeded,
		"evidence_keywords": evidence,
	}
	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("player_emotion_analysis_result",
			zap.String("emotion", emotion),
			zap.String("emotion_label", emotionLabel(emotion)),
			zap.Float64("confidence", confidence),
			zap.Float64("intensity", intensity),
			zap.Bool("escalation_needed", escalationNeeded),
			zap.Strings("evidence_keywords", evidence),
			zap.String("result_json", string(out)),
			zap.String("input_preview", previewToolInput(in.UserInput)))
	}
	return string(out), nil
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

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
