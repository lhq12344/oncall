package dialogue

import (
	"context"
	"errors"
	"strings"
	"testing"

	einoModel "github.com/cloudwego/eino/components/model"
	einoretriever "github.com/cloudwego/eino/components/retriever"
	"go_agent/internal/logic/ai/models"

	"github.com/cloudwego/eino/schema"
)

func TestBuildDecomposeNodeKeepsAtomicProcedureQuestion(t *testing.T) {
	model := &decomposeFakeChatModel{
		generateResponse: schema.AssistantMessage(`{"mode":"unsplit","reason":"single procedure request","questions":["如何使用 GCash 充值"]}`, nil),
	}
	node := buildDecomposeNode(context.Background(), &models.ChatModel{Client: model}, nil)

	got, err := node(context.Background(), "如何使用 GCash 充值")
	if err != nil {
		t.Fatalf("buildDecomposeNode returned error: %v", err)
	}
	if len(got) != 1 || got[0] != "如何使用 GCash 充值" {
		t.Fatalf("sub questions = %#v, want original atomic procedure question", got)
	}
	if model.generateCalls != 1 {
		t.Fatalf("Generate calls = %d, want 1 for structured decompose decision", model.generateCalls)
	}
}

func TestBuildDecomposeNodeAllowsMultipleQuestionIntents(t *testing.T) {
	model := &decomposeFakeChatModel{
		generateResponse: schema.AssistantMessage(`{"mode":"split","reason":"two independent intents","questions":["GCash充值方式步骤","游客ID账号能否充值"]}`, nil),
	}
	node := buildDecomposeNode(context.Background(), &models.ChatModel{Client: model}, nil)

	got, err := node(context.Background(), "GCash充值方式步骤 游客ID账号能否充值")
	if err != nil {
		t.Fatalf("buildDecomposeNode returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("sub questions = %#v, want 2 decomposed questions", got)
	}
	if model.generateCalls != 1 {
		t.Fatalf("Generate calls = %d, want 1 for multiple question intents", model.generateCalls)
	}
}

func TestBuildDecomposeNodeFallsBackToUnsplitOnInvalidJSON(t *testing.T) {
	model := &decomposeFakeChatModel{
		generateResponse: schema.AssistantMessage("不是 JSON", nil),
	}
	node := buildDecomposeNode(context.Background(), &models.ChatModel{Client: model}, nil)

	original := "如何使用 GCash 充值"
	got, err := node(context.Background(), original)
	if err != nil {
		t.Fatalf("buildDecomposeNode returned error: %v", err)
	}
	if len(got) != 1 || got[0] != original {
		t.Fatalf("sub questions = %#v, want fallback to original", got)
	}
}

func TestBuildDecomposeNodeFallsBackToUnsplitOnQualityGateFailure(t *testing.T) {
	model := &decomposeFakeChatModel{
		generateResponse: schema.AssistantMessage(`{"mode":"split","reason":"bad split","questions":["天气怎么样","今天星期几"]}`, nil),
	}
	node := buildDecomposeNode(context.Background(), &models.ChatModel{Client: model}, nil)

	original := "GCash充值方式步骤 游客ID账号能否充值"
	got, err := node(context.Background(), original)
	if err != nil {
		t.Fatalf("buildDecomposeNode returned error: %v", err)
	}
	if len(got) != 1 || got[0] != original {
		t.Fatalf("sub questions = %#v, want fallback to original when quality gate fails", got)
	}
}

func TestBuildDecomposeNodeFallsBackToUnsplitOnTooManyQuestions(t *testing.T) {
	model := &decomposeFakeChatModel{
		generateResponse: schema.AssistantMessage(`{"mode":"split","reason":"over split","questions":["q1","q2","q3","q4"]}`, nil),
	}
	node := buildDecomposeNode(context.Background(), &models.ChatModel{Client: model}, nil)

	original := "q1 q2 q3 q4"
	got, err := node(context.Background(), original)
	if err != nil {
		t.Fatalf("buildDecomposeNode returned error: %v", err)
	}
	if len(got) != 1 || got[0] != original {
		t.Fatalf("sub questions = %#v, want fallback to original when split exceeds limit", got)
	}
}

func TestBuildParallelRAGNodeFailsOnRetrieverError(t *testing.T) {
	node := buildParallelRAGNode(&decomposeErrorRetriever{}, nil)

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

func TestBuildEvaluateNodeKeepsLLMPendingEvenWithHighScoreRAG(t *testing.T) {
	model := &decomposeFakeChatModel{
		generateResponse: schema.AssistantMessage("PENDING:如何删除角色", nil),
	}
	node := buildEvaluateNode(context.Background(), &models.ChatModel{Client: model}, nil)

	doc := schema.Document{Content: "# 该怎么删除角色？\n您可以通过角色选择界面中的删除按钮来删除角色。"}
	doc.WithScore(0.9469)
	result, err := node(context.Background(), map[string][]schema.Document{
		"如何删除角色": {doc},
	})
	if err != nil {
		t.Fatalf("buildEvaluateNode returned error: %v", err)
	}
	if len(result.SolvedContexts) != 0 {
		t.Fatalf("solved contexts count = %d, want 0 when evaluator marks pending", len(result.SolvedContexts))
	}
	if len(result.PendingQuestions) != 1 || result.PendingQuestions[0] != "如何删除角色" {
		t.Fatalf("pending questions = %#v, want evaluator pending question", result.PendingQuestions)
	}
	if result.Status != "partial" {
		t.Fatalf("status = %q, want partial", result.Status)
	}
}

type decomposeFakeChatModel struct {
	generateResponse *schema.Message
	generateCalls    int
}

type decomposeErrorRetriever struct{}

func (decomposeErrorRetriever) Retrieve(context.Context, string, ...einoretriever.Option) ([]*schema.Document, error) {
	return nil, errors.New("database retrieve failed")
}

func (f *decomposeFakeChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...einoModel.Option) (*schema.Message, error) {
	f.generateCalls++
	if f.generateResponse != nil {
		return f.generateResponse, nil
	}
	return schema.AssistantMessage("", nil), nil
}

func (f *decomposeFakeChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...einoModel.Option) (*schema.StreamReader[*schema.Message], error) {
	sr, sw := schema.Pipe[*schema.Message](1)
	go func() {
		defer sw.Close()
	}()
	return sr, nil
}

func (f *decomposeFakeChatModel) WithTools(_ []*schema.ToolInfo) (einoModel.ToolCallingChatModel, error) {
	return f, nil
}
