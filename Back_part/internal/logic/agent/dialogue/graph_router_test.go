package dialogue

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func TestRagResultRouterSendsSolvedKnowledgeSpecialistResultToAnswer(t *testing.T) {
	route, err := ragResultRouter(context.Background(), []*schema.Message{
		schema.UserMessage("活动奖励规则是什么"),
		schema.ToolMessage(
			`{"status":"success","solved_contexts":["【活动奖励规则】\n完成任务后可领取奖励"],"pending_questions":[]}`,
			"call_ks",
			schema.WithToolName("knowledge_search_expert"),
		),
	})
	if err != nil {
		t.Fatalf("router returned error: %v", err)
	}
	if route != "answer_node" {
		t.Fatalf("route = %q, want answer_node", route)
	}
}

func TestRagResultRouterSendsKnowledgeSpecialistPendingToComplex(t *testing.T) {
	route, err := ragResultRouter(context.Background(), []*schema.Message{
		schema.UserMessage("活动奖励和充值不到账怎么办"),
		schema.ToolMessage(
			`{"status":"partial","solved_contexts":["【活动奖励】\n完成任务后可领取奖励"],"pending_questions":["充值不到账怎么办"]}`,
			"call_ks",
			schema.WithToolName("knowledge_search_expert"),
		),
	})
	if err != nil {
		t.Fatalf("router returned error: %v", err)
	}
	if route != "complex_node" {
		t.Fatalf("route = %q, want complex_node", route)
	}
}

func TestRagResultRouterSendsEmptyKnowledgeSpecialistResultToComplex(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "empty", content: `{"status":"empty","solved_contexts":[],"pending_questions":[]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			route, err := ragResultRouter(context.Background(), []*schema.Message{
				schema.UserMessage("活动奖励规则是什么"),
				schema.ToolMessage(tc.content, "call_ks", schema.WithToolName("knowledge_search_expert")),
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

func TestAppendKnowledgeSpecialistResultMessageAddsSyntheticToolOutput(t *testing.T) {
	msgs := []*schema.Message{schema.UserMessage("活动奖励规则是什么")}
	out := appendKnowledgeSpecialistResultMessage(context.Background(), &Config{}, msgs, &KnowledgeSpecialistResult{
		Status:           "success",
		SolvedContexts:   []string{"【活动奖励规则】\n完成任务后可领取奖励"},
		PendingQuestions: nil,
	})
	if len(out) != len(msgs)+2 {
		t.Fatalf("len(out) = %d, want %d", len(out), len(msgs)+2)
	}
	callMsg := out[len(out)-2]
	if callMsg.Role != schema.Assistant || len(callMsg.ToolCalls) != 1 {
		t.Fatalf("synthetic tool call message = %#v, want assistant with one tool call", callMsg)
	}
	if callMsg.ToolCalls[0].Function.Name != "knowledge_search_expert" {
		t.Fatalf("tool call name = %q, want knowledge_search_expert", callMsg.ToolCalls[0].Function.Name)
	}
	last := out[len(out)-1]
	if last.Role != schema.Tool {
		t.Fatalf("last role = %s, want tool", last.Role)
	}
	if last.ToolName != "knowledge_search_expert" {
		t.Fatalf("tool name = %q, want knowledge_search_expert", last.ToolName)
	}
	if last.ToolCallID != knowledgeSpecialistToolCallID {
		t.Fatalf("tool call id = %q, want %q", last.ToolCallID, knowledgeSpecialistToolCallID)
	}
	if !strings.Contains(last.Content, "solved_contexts") {
		t.Fatalf("synthetic tool payload missing solved_contexts: %s", last.Content)
	}
}

func TestAppendKnowledgeSpecialistResultReturnsInvokeError(t *testing.T) {
	msgs := []*schema.Message{schema.UserMessage("向量数据库连不上怎么办")}
	out, err := appendKnowledgeSpecialistResult(context.Background(), &Config{}, failingKnowledgeSpecialist{}, msgs)
	if err == nil {
		t.Fatalf("expected invoke error")
	}
	if out != nil {
		t.Fatalf("out = %#v, want nil on invoke error", out)
	}
	if !strings.Contains(err.Error(), "knowledge specialist failed") {
		t.Fatalf("error = %v, want knowledge specialist failure", err)
	}
}

type failingKnowledgeSpecialist struct{}

func (failingKnowledgeSpecialist) Invoke(context.Context, string, ...compose.Option) (*KnowledgeSpecialistResult, error) {
	return nil, errors.New("database retrieve failed")
}

func (failingKnowledgeSpecialist) Stream(context.Context, string, ...compose.Option) (*schema.StreamReader[*KnowledgeSpecialistResult], error) {
	return nil, errors.New("database retrieve failed")
}

func (failingKnowledgeSpecialist) Collect(context.Context, *schema.StreamReader[string], ...compose.Option) (*KnowledgeSpecialistResult, error) {
	return nil, errors.New("database retrieve failed")
}

func (failingKnowledgeSpecialist) Transform(context.Context, *schema.StreamReader[string], ...compose.Option) (*schema.StreamReader[*KnowledgeSpecialistResult], error) {
	r, w := schema.Pipe[*KnowledgeSpecialistResult](1)
	_ = w.Send(nil, errors.New("database retrieve failed"))
	w.Close()
	return r, nil
}
