package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	einoretriever "github.com/cloudwego/eino/components/retriever"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
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

func TestKnowledgeRetrieveToolFailsWhenRetrieverUnavailable(t *testing.T) {
	tool := NewKnowledgeRetrieveTool(nil, nil).(*KnowledgeRetrieveTool)

	out, err := tool.InvokableRun(context.Background(), `{"query":"数据库召回"}`)
	if err == nil {
		t.Fatalf("expected unavailable retriever error")
	}
	if out != "" {
		t.Fatalf("out = %q, want empty output on error", out)
	}
	if !strings.Contains(err.Error(), "knowledge retriever unavailable") {
		t.Fatalf("error = %v, want unavailable retriever", err)
	}
}

func TestKnowledgeRetrieveToolFailsOnRetrieverError(t *testing.T) {
	tool := NewKnowledgeRetrieveTool(intentErrorRetriever{}, nil).(*KnowledgeRetrieveTool)

	out, err := tool.InvokableRun(context.Background(), `{"query":"数据库召回"}`)
	if err == nil {
		t.Fatalf("expected retriever error")
	}
	if out != "" {
		t.Fatalf("out = %q, want empty output on error", out)
	}
	if !strings.Contains(err.Error(), "knowledge retrieve failed") {
		t.Fatalf("error = %v, want knowledge retrieve failure", err)
	}
}

func TestSafeToolMiddlewarePropagatesInvokableErrors(t *testing.T) {
	middleware := &SafeToolMiddleware{}
	wrapped, err := middleware.WrapInvokableToolCall(
		context.Background(),
		func(context.Context, string, ...einotool.Option) (string, error) {
			return "", errors.New("tool failed")
		},
		&adk.ToolContext{Name: "failing_tool"},
	)
	if err != nil {
		t.Fatalf("WrapInvokableToolCall returned error: %v", err)
	}

	out, err := wrapped(context.Background(), `{}`)
	if err == nil {
		t.Fatalf("expected tool error")
	}
	if out != "" {
		t.Fatalf("out = %q, want empty output on error", out)
	}
	if !strings.Contains(err.Error(), "tool failed") {
		t.Fatalf("error = %v, want original tool failure", err)
	}
}

func TestSafeToolMiddlewarePreservesInterruptErrors(t *testing.T) {
	middleware := &SafeToolMiddleware{}
	wrapped, err := middleware.WrapInvokableToolCall(
		context.Background(),
		func(ctx context.Context, _ string, _ ...einotool.Option) (string, error) {
			return "", einotool.StatefulInterrupt(ctx, "approval required", "state")
		},
		&adk.ToolContext{Name: "interrupting_tool"},
	)
	if err != nil {
		t.Fatalf("WrapInvokableToolCall returned error: %v", err)
	}

	_, err = wrapped(context.Background(), `{}`)
	if _, ok := compose.IsInterruptRerunError(err); !ok {
		t.Fatalf("error = %v, want interrupt rerun error", err)
	}
}

func TestSafeToolMiddlewarePropagatesStreamReaderErrors(t *testing.T) {
	middleware := &SafeToolMiddleware{}
	wrapped, err := middleware.WrapStreamableToolCall(
		context.Background(),
		func(context.Context, string, ...einotool.Option) (*schema.StreamReader[string], error) {
			r, w := schema.Pipe[string](1)
			_ = w.Send("", errors.New("stream failed"))
			w.Close()
			return r, nil
		},
		&adk.ToolContext{Name: "streaming_tool"},
	)
	if err != nil {
		t.Fatalf("WrapStreamableToolCall returned error: %v", err)
	}

	reader, err := wrapped(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("wrapped stream returned setup error: %v", err)
	}
	defer reader.Close()

	_, err = reader.Recv()
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("Recv error = %v, want stream failure", err)
	}
	if !strings.Contains(err.Error(), "stream failed") {
		t.Fatalf("error = %v, want original stream failure", err)
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

type intentErrorRetriever struct{}

func (intentErrorRetriever) Retrieve(context.Context, string, ...einoretriever.Option) ([]*schema.Document, error) {
	return nil, errors.New("database retrieve failed")
}
