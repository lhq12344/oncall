package tools

import (
	"context"
	"encoding/json"
	"testing"

	"go_agent/internal/logic/ai/models"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestPlayerEmotionTreatsPureInfoStatementAsStable(t *testing.T) {
	tool := NewPlayerEmotionAnalysisTool(&models.ChatModel{
		Client: &fakeEmotionChatModel{
			content: `{"emotion":"confused","label":"困惑","intensity":0.2,"confidence":0.95,"escalation_needed":false,"reasoning":"LLM误判","keywords":["游戏ID","lhq"]}`,
		},
	}, nil).(*PlayerEmotionAnalysisTool)

	result := runEmotionAnalysisForTest(t, tool, "我的游戏ID是lhq")
	if got, _ := result["emotion"].(string); got != "stable" {
		t.Fatalf("emotion = %q, want stable; result=%v", got, result)
	}
	if got, _ := result["source"].(string); got != "rule" {
		t.Fatalf("source = %q, want rule; result=%v", got, result)
	}
	if got, _ := result["escalation_needed"].(bool); got {
		t.Fatalf("escalation_needed = true, want false; result=%v", result)
	}
}

func TestPlayerEmotionKeepsQuestionEmotionClassification(t *testing.T) {
	tool := NewPlayerEmotionAnalysisTool(nil, nil).(*PlayerEmotionAnalysisTool)
	result := runEmotionAnalysisForTest(t, tool, "为什么我的账号登录不了")
	if got, _ := result["emotion"].(string); got != "confused" {
		t.Fatalf("emotion = %q, want confused; result=%v", got, result)
	}
}

func runEmotionAnalysisForTest(t *testing.T, tool *PlayerEmotionAnalysisTool, input string) map[string]any {
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

type fakeEmotionChatModel struct {
	content string
}

func (m *fakeEmotionChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	return schema.AssistantMessage(m.content, nil), nil
}

func (m *fakeEmotionChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func (m *fakeEmotionChatModel) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}
