package dialogue

import (
	"context"
	"strings"
	"testing"

	"go_agent/internal/logic/ai/models"

	"github.com/cloudwego/eino/schema"
)

func TestBuildParallelRAGNodeKeepsOriginalQuery(t *testing.T) {
	retriever := &fakeRetriever{
		docs: map[string][]*schema.Document{
			"こんにちは": {
				{Content: "中文知识"},
			},
		},
	}

	node := buildParallelRAGNode(retriever, nil)
	result, err := node(context.Background(), []string{"こんにちは"})
	if err != nil {
		t.Fatalf("buildParallelRAGNode returned error: %v", err)
	}
	if len(retriever.queries) != 1 || retriever.queries[0] != "こんにちは" {
		t.Fatalf("retriever queries = %#v, want original query", retriever.queries)
	}
	if _, ok := result["こんにちは"]; !ok {
		t.Fatalf("result missing original key: %#v", result)
	}
}

func TestBuildEvaluateNodeUsesUserLanguage(t *testing.T) {
	model := &fakeToolCallingChatModel{
		generateResponse: schema.AssistantMessage("SOLVED:How to reset password?", nil),
	}
	node := buildEvaluateNode(context.Background(), &models.ChatModel{Client: model}, nil)

	ctx := withDialogueRuntimeContext(context.Background(), dialogueRuntimeContext{UserLanguage: "en"})
	result, err := node(ctx, map[string][]schema.Document{
		"How to reset password?": {
			{Content: "这是中文资料。"},
		},
	})
	if err != nil {
		t.Fatalf("buildEvaluateNode returned error: %v", err)
	}
	if len(result.SolvedContexts) != 1 {
		t.Fatalf("solved contexts count = %d, want 1", len(result.SolvedContexts))
	}
	if len(model.lastGenerate) != 2 {
		t.Fatalf("model input count = %d, want 2", len(model.lastGenerate))
	}
	if got := model.lastGenerate[0].Content; got == "" || !strings.Contains(got, "用户的 en 问题") {
		t.Fatalf("system prompt missing user language: %s", got)
	}
}
