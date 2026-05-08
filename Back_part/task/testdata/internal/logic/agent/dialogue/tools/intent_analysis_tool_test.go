package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestIntentAnalysisToolClassifiesGameSupportRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantAnyType map[string]bool
	}{
		{
			name:        "account stolen or login failure",
			input:       "我的账号被盗了，现在无法登录",
			wantAnyType: map[string]bool{"account_issue": true, "login_server_issue": true},
		},
		{
			name:        "payment not received",
			input:       "我充值扣费了但是钻石不到账",
			wantAnyType: map[string]bool{"payment_issue": true},
		},
		{
			name:        "gameplay question",
			input:       "这个副本怎么打，装备要怎么升级",
			wantAnyType: map[string]bool{"gameplay_question": true},
		},
		{
			name:        "ban appeal",
			input:       "我的号被封号了，我要申诉解封",
			wantAnyType: map[string]bool{"ban_appeal": true},
		},
	}

	tool := NewIntentAnalysisTool(nil, nil, nil).(*IntentAnalysisTool)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runIntentAnalysisForTest(t, tool, tt.input)
			got, _ := result["intent_type"].(string)
			if !tt.wantAnyType[got] {
				t.Fatalf("intent_type = %q, want one of %v; result=%v", got, tt.wantAnyType, result)
			}
			if _, ok := result["intent_label"].(string); !ok {
				t.Fatalf("intent_label missing or not string: %v", result)
			}
			if _, ok := result["routing_hint"].(string); !ok {
				t.Fatalf("routing_hint missing or not string: %v", result)
			}
		})
	}
}

func runIntentAnalysisForTest(t *testing.T, tool *IntentAnalysisTool, input string) map[string]any {
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
