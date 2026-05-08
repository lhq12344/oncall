package dialogue

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestRagResultRouterPrefersKnowledgeSpecialistJSONOverGateMarker(t *testing.T) {
	route, err := ragResultRouter(context.Background(), []*schema.Message{
		schema.UserMessage("活动奖励规则是什么"),
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call_ks",
			Function: schema.FunctionCall{
				Name: "knowledge_search_expert",
			},
		}}),
		schema.ToolMessage(
			`{"status":"success","solved_contexts":["【活动奖励规则】\n完成任务后可领取奖励"],"pending_questions":[]}`,
			"call_ks",
			schema.WithToolName("knowledge_search_expert"),
		),
		schema.AssistantMessage("[TO_COMPLEX] 模型误判为复杂问题", nil),
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
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call_ks",
			Function: schema.FunctionCall{
				Name: "knowledge_search_expert",
			},
		}}),
		schema.ToolMessage(
			`{"status":"partial","solved_contexts":["【活动奖励】\n完成任务后可领取奖励"],"pending_questions":["充值不到账怎么办"]}`,
			"call_ks",
			schema.WithToolName("knowledge_search_expert"),
		),
		schema.AssistantMessage("[RESOLVED] 模型误判为已解决", nil),
	})
	if err != nil {
		t.Fatalf("router returned error: %v", err)
	}
	if route != "complex_node" {
		t.Fatalf("route = %q, want complex_node", route)
	}
}
