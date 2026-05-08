package dialogue

import (
	"context"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestRagResultRouterTreatsStructuredFailuresAsComplex(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "error status",
			content: `{"status":"error","results":[],"count":0,"message":"embedding failed"}`,
		},
		{
			name:    "degraded status",
			content: `{"status":"degraded","results":[],"count":0,"message":"retriever unavailable"}`,
		},
		{
			name:    "success with zero count",
			content: `{"status":"success","results":[],"count":0}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			route, err := ragResultRouter(context.Background(), []*schema.Message{
				schema.UserMessage("查一下认证信息"),
				{Role: schema.Tool, Content: tc.content},
			})
			if err != nil {
				t.Fatalf("router returned error: %v", err)
			}
			if route != "complex_node" {
				t.Fatalf("route = %q, want complex_node", route)
			}
		})
	}
}

func TestRagResultRouterTreatsStructuredHitsAsAnswer(t *testing.T) {
	route, err := ragResultRouter(context.Background(), []*schema.Message{
		schema.UserMessage("查一下认证信息"),
		{Role: schema.Tool, Content: `{"status":"success","results":[{"content":"认证信息说明"}],"count":1}`},
	})
	if err != nil {
		t.Fatalf("router returned error: %v", err)
	}
	if route != "answer_node" {
		t.Fatalf("route = %q, want answer_node", route)
	}
}

func TestPrepareLeafAgentMessagesAppendsUserHandoff(t *testing.T) {
	msgs := []*schema.Message{
		schema.UserMessage("Eino 是什么"),
		{Role: schema.Tool, Content: `{"status":"error","results":[],"count":0}`},
		schema.AssistantMessage("[TO_COMPLEX] 知识库检索异常", nil),
	}

	prepared := prepareComplexAgentMessages(context.Background(), msgs)
	if len(prepared) != len(msgs)+1 {
		t.Fatalf("len(prepared) = %d, want %d", len(prepared), len(msgs)+1)
	}
	last := prepared[len(prepared)-1]
	if last.Role != schema.User {
		t.Fatalf("last role = %s, want user", last.Role)
	}
	if !strings.Contains(last.Content, "Eino 是什么") {
		t.Fatalf("handoff content did not include original question: %q", last.Content)
	}
	if !strings.Contains(last.Content, "不要输出 [RESOLVED] 或 [TO_COMPLEX]") {
		t.Fatalf("handoff content did not suppress routing markers: %q", last.Content)
	}
}

func TestAppendAnalysisNodeMessageAppendsInternalSystemContext(t *testing.T) {
	msgs := []*schema.Message{
		schema.UserMessage("我充值扣费了但是钻石不到账，急死了"),
	}

	prepared, err := appendAnalysisNodeMessage(context.Background(), &Config{}, msgs)
	if err != nil {
		t.Fatalf("appendAnalysisNodeMessage returned error: %v", err)
	}
	if len(prepared) != len(msgs)+1 {
		t.Fatalf("len(prepared) = %d, want %d", len(prepared), len(msgs)+1)
	}

	last := prepared[len(prepared)-1]
	if last.Role != schema.System {
		t.Fatalf("last role = %s, want system", last.Role)
	}
	for _, want := range []string{"内部客服分析结果", "intent_analysis=", "player_emotion_analysis=", "payment_issue", "urgent", "analysis_degraded=false"} {
		if !strings.Contains(last.Content, want) {
			t.Fatalf("analysis system message missing %q: %s", want, last.Content)
		}
	}
}

func TestRunAnalysisToolFallsBackOnFailure(t *testing.T) {
	got, err := runAnalysisTool(context.Background(), failingAnalysisTool{}, "充值不到账", fallbackIntentAnalysis())
	if err == nil {
		t.Fatalf("expected fallback error")
	}
	if !strings.Contains(got, `"intent_type":"other"`) {
		t.Fatalf("fallback intent analysis not returned: %s", got)
	}
}

func TestGateInstructionDoesNotMandateIntentEmotionTools(t *testing.T) {
	for _, forbidden := range []string{"调用 intent_analysis", "调用 player_emotion_analysis"} {
		if strings.Contains(gateAgentInstruction, forbidden) {
			t.Fatalf("gate instruction still mandates %q: %s", forbidden, gateAgentInstruction)
		}
	}
}

func TestRagResultRouterIgnoresInternalAnalysisSystemMessage(t *testing.T) {
	route, err := ragResultRouter(context.Background(), []*schema.Message{
		schema.UserMessage("我充值不到账"),
		schema.SystemMessage(`内部客服分析结果：intent_analysis={"intent_type":"payment_issue","count":1}`),
	})
	if err != nil {
		t.Fatalf("router returned error: %v", err)
	}
	if route != "complex_node" {
		t.Fatalf("route = %q, want complex_node", route)
	}
}

type failingAnalysisTool struct{}

func (failingAnalysisTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "failing_analysis"}, nil
}

func (failingAnalysisTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	return "", context.Canceled
}
