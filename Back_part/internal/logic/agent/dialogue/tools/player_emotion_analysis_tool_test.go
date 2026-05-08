package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestPlayerEmotionAnalysisToolClassifiesEmotion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		input          string
		wantEmotion    string
		wantEscalation bool
	}{
		{
			name:        "stable question",
			input:       "我想问一下这个任务在哪里接",
			wantEmotion: "stable",
		},
		{
			name:           "urgent request",
			input:          "急死了，马上帮我处理一下",
			wantEmotion:    "urgent",
			wantEscalation: true,
		},
		{
			name:           "angry complaint",
			input:          "太离谱了，我要投诉你们这个处理结果",
			wantEmotion:    "angry",
			wantEscalation: true,
		},
		{
			name:        "happy thanks",
			input:       "谢谢，太好了，问题解决了",
			wantEmotion: "happy",
		},
	}

	tool := NewPlayerEmotionAnalysisTool(nil).(*PlayerEmotionAnalysisTool)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runEmotionAnalysisForTest(t, tool, tt.input)
			if got, _ := result["emotion"].(string); got != tt.wantEmotion {
				t.Fatalf("emotion = %q, want %q; result=%v", got, tt.wantEmotion, result)
			}
			if got, _ := result["escalation_needed"].(bool); got != tt.wantEscalation {
				t.Fatalf("escalation_needed = %v, want %v; result=%v", got, tt.wantEscalation, result)
			}
		})
	}
}

func runEmotionAnalysisForTest(t *testing.T, tool *PlayerEmotionAnalysisTool, input string) map[string]any {
	t.Helper()

	out, err := tool.InvokableRun(context.Background(), `{"user_input":"`+input+`"}`)
	if err != nil {
		t.Fatalf("InvokableRun returned error: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse result %q: %v", out, err)
	}
	return result
}
