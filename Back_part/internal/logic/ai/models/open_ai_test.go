package models

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type captureRoundTripper struct {
	header http.Header
}

func (c *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.header = req.Header.Clone()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
	}, nil
}

func TestRetryRoundTripperUsesConfiguredAPIKeyHeader(t *testing.T) {
	base := &captureRoundTripper{}
	transport := &retryRoundTripper{
		base:         base,
		apiKeyHeader: "x-litellm-api-key",
		apiKey:       "test-key",
	}
	req, err := http.NewRequest(http.MethodPost, "https://example.test/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-key")

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := base.header.Get("x-litellm-api-key"); got != "test-key" {
		t.Fatalf("x-litellm-api-key = %q, want test-key", got)
	}
	if got := base.header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization header = %q, want empty", got)
	}
}

func TestSanitizingToolCallingChatModelCleansOutgoingToolCallArguments(t *testing.T) {
	base := &capturingToolCallingChatModel{}
	model := newSanitizingToolCallingChatModel(base)

	dirtyArgs := `{"query":"GCash recharge"}{"query":"GCash recharge"}`
	input := []*schema.Message{{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{{
			Function: schema.FunctionCall{
				Name:      "web_search",
				Arguments: dirtyArgs,
			},
		}},
	}}

	if _, err := model.Generate(context.Background(), input); err != nil {
		t.Fatal(err)
	}

	got := base.generateInput[0].ToolCalls[0].Function.Arguments
	want := `{"query":"GCash recharge"}`
	if got != want {
		t.Fatalf("Generate arguments = %q, want %q", got, want)
	}
	if input[0].ToolCalls[0].Function.Arguments != dirtyArgs {
		t.Fatalf("original input was mutated")
	}
}

func TestSanitizingToolCallingChatModelPreservesCleaningAfterWithTools(t *testing.T) {
	base := &capturingToolCallingChatModel{}
	model := newSanitizingToolCallingChatModel(base)

	withTools, err := model.WithTools([]*schema.ToolInfo{{Name: "web_search"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := withTools.Stream(context.Background(), []*schema.Message{{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{{
			Function: schema.FunctionCall{
				Name:      "web_search",
				Arguments: `{"query":"a"}{"query":"a"}`,
			},
		}},
	}}); err != nil {
		t.Fatal(err)
	}

	got := base.streamInput[0].ToolCalls[0].Function.Arguments
	want := `{"query":"a"}`
	if got != want {
		t.Fatalf("Stream arguments = %q, want %q", got, want)
	}
}

type capturingToolCallingChatModel struct {
	generateInput []*schema.Message
	streamInput   []*schema.Message
}

func (m *capturingToolCallingChatModel) Generate(_ context.Context, input []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	m.generateInput = input
	return schema.AssistantMessage("", nil), nil
}

func (m *capturingToolCallingChatModel) Stream(_ context.Context, input []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	m.streamInput = input
	sr, sw := schema.Pipe[*schema.Message](1)
	go func() {
		defer sw.Close()
	}()
	return sr, nil
}

func (m *capturingToolCallingChatModel) WithTools(_ []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}
