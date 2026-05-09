package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestIntentAnalysisTreatsPureInfoStatementAsGeneralChat(t *testing.T) {
	tool := NewIntentAnalysisTool(nil, nil, nil).(*IntentAnalysisTool)
	result := runIntentAnalysisForTest(t, tool, "我的游戏ID是lhq")

	if got, _ := result["intent_type"].(string); got != "general_chat" {
		t.Fatalf("intent_type = %q, want general_chat; result=%v", got, result)
	}
	if got, _ := result["routing_hint"].(string); got != "direct_answer" {
		t.Fatalf("routing_hint = %q, want direct_answer; result=%v", got, result)
	}
	if missing, _ := result["missing_info"].([]any); len(missing) != 0 {
		t.Fatalf("missing_info = %v, want empty", missing)
	}
}

func TestIntentAnalysisKeepsProblemStatementAsSupportIntent(t *testing.T) {
	tool := NewIntentAnalysisTool(nil, nil, nil).(*IntentAnalysisTool)
	result := runIntentAnalysisForTest(t, tool, "我的游戏ID是lhq，登录失败")

	if got, _ := result["intent_type"].(string); got == "general_chat" {
		t.Fatalf("intent_type = general_chat, want support intent; result=%v", result)
	}
}

func runIntentAnalysisForTest(t *testing.T, tool *IntentAnalysisTool, input string) map[string]any {
	t.Helper()

	args, err := json.Marshal(map[string]string{"user_input": input})
	if err != nil {
		t.Fatalf("failed to marshal args: %v", err)
	}
	out, err := tool.InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatalf("InvokableRun returned error: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse result %q: %v", out, err)
	}
	return result
}
