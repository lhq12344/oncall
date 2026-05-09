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

func TestBuildParallelRAGNodeFailsOnRetrieverError(t *testing.T) {
	retriever := &fakeRetriever{err: errFakeRetriever}
	node := buildParallelRAGNode(retriever, nil)

	result, err := node(context.Background(), []string{"数据库召回"})
	if err == nil {
		t.Fatalf("expected retriever error")
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil on retriever error", result)
	}
	if !strings.Contains(err.Error(), "knowledge retrieve failed") {
		t.Fatalf("error = %v, want knowledge retrieve failure", err)
	}
}

func TestBuildParallelRAGNodeFailsWhenRetrieverUnavailable(t *testing.T) {
	node := buildParallelRAGNode(nil, nil)

	result, err := node(context.Background(), []string{"数据库召回"})
	if err == nil {
		t.Fatalf("expected unavailable retriever error")
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil on unavailable retriever", result)
	}
	if !strings.Contains(err.Error(), "knowledge retriever unavailable") {
		t.Fatalf("error = %v, want unavailable retriever", err)
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

func TestBuildEvaluateNodeMarksUncoveredQuestionsPending(t *testing.T) {
	model := &fakeToolCallingChatModel{
		generateResponse: schema.AssistantMessage("SOLVED:已覆盖问题", nil),
	}
	node := buildEvaluateNode(context.Background(), &models.ChatModel{Client: model}, nil)

	result, err := node(context.Background(), map[string][]schema.Document{
		"已覆盖问题": {
			{Content: "相关资料"},
		},
		"未覆盖问题": {
			{Content: "也有资料，但模型遗漏了这一行"},
		},
	})
	if err != nil {
		t.Fatalf("buildEvaluateNode returned error: %v", err)
	}
	if len(result.SolvedContexts) != 1 {
		t.Fatalf("solved contexts count = %d, want 1", len(result.SolvedContexts))
	}
	if len(result.PendingQuestions) != 1 || result.PendingQuestions[0] != "未覆盖问题" {
		t.Fatalf("pending questions = %#v, want uncovered question", result.PendingQuestions)
	}
	if result.Status != "partial" {
		t.Fatalf("status = %q, want partial", result.Status)
	}
}

func TestBuildEvaluateNodeDoesNotSolveQuestionsWithoutDocs(t *testing.T) {
	model := &fakeToolCallingChatModel{
		generateResponse: schema.AssistantMessage("SOLVED:无文档问题", nil),
	}
	node := buildEvaluateNode(context.Background(), &models.ChatModel{Client: model}, nil)

	result, err := node(context.Background(), map[string][]schema.Document{
		"无文档问题": nil,
	})
	if err != nil {
		t.Fatalf("buildEvaluateNode returned error: %v", err)
	}
	if len(result.SolvedContexts) != 0 {
		t.Fatalf("solved contexts = %#v, want none", result.SolvedContexts)
	}
	if len(result.PendingQuestions) != 1 || result.PendingQuestions[0] != "无文档问题" {
		t.Fatalf("pending questions = %#v, want no-doc question", result.PendingQuestions)
	}
}
