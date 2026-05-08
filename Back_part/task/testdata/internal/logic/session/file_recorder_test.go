package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestFileSessionRecorderPreservesOriginalLanguageContent(t *testing.T) {
	dir := t.TempDir()
	recorder := NewFileSessionRecorder(dir)

	userMsg := schema.UserMessage("こんにちは、アカウントを復旧したいです")
	assistantMsg := schema.AssistantMessage("请提供绑定信息。", nil)

	err := recorder.AppendTurn(context.Background(), "session-1", "chat_stream_graph", userMsg, assistantMsg)
	if err != nil {
		t.Fatalf("AppendTurn returned error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "session-1.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if got := string(content); !strings.Contains(got, userMsg.Content) || !strings.Contains(got, assistantMsg.Content) {
		t.Fatalf("recorded content does not preserve original messages: %s", got)
	}
}

func TestFileSessionRecorderRecordsPromptSnapshot(t *testing.T) {
	dir := t.TempDir()
	recorder := NewFileSessionRecorder(dir)

	promptMessages := []*schema.Message{
		schema.SystemMessage("历史会话摘要：用户叫零零七"),
		schema.UserMessage("上一轮问题"),
		schema.AssistantMessage("上一轮回答", []schema.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "knowledge_search",
				Arguments: `{"query":"GCash"}`,
			},
		}}),
	}
	userMsg := schema.UserMessage("如何使用 GCash 充值？")
	assistantMsg := schema.AssistantMessage("可以通过充值中心选择 GCash。", nil)

	err := recorder.AppendTurnWithPrompt(context.Background(), "session-1", "chat_stream_graph", promptMessages, userMsg, assistantMsg)
	if err != nil {
		t.Fatalf("AppendTurnWithPrompt returned error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "session-1.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 6 {
		t.Fatalf("line count = %d, want header + 3 prompt messages + 2 visible messages\n%s", len(lines), string(content))
	}

	var firstPrompt map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &firstPrompt); err != nil {
		t.Fatalf("unmarshal first prompt line: %v", err)
	}
	if firstPrompt["type"] != "prompt_message" || firstPrompt["role"] != string(schema.System) || numberValue(firstPrompt["index"]) != 1 {
		t.Fatalf("first prompt record = %#v", firstPrompt)
	}
	if firstPrompt["content"] != promptMessages[0].Content {
		t.Fatalf("first prompt content = %#v, want %#v", firstPrompt["content"], promptMessages[0].Content)
	}

	var promptWithToolCalls map[string]any
	if err := json.Unmarshal([]byte(lines[3]), &promptWithToolCalls); err != nil {
		t.Fatalf("unmarshal prompt tool line: %v", err)
	}
	toolCalls, _ := json.Marshal(promptWithToolCalls["tool_calls"])
	if !strings.Contains(string(toolCalls), "knowledge_search") {
		t.Fatalf("prompt tool calls not recorded: %#v", promptWithToolCalls)
	}

	var visibleUser map[string]any
	if err := json.Unmarshal([]byte(lines[4]), &visibleUser); err != nil {
		t.Fatalf("unmarshal visible user line: %v", err)
	}
	if visibleUser["type"] != "message" || visibleUser["content"] != userMsg.Content {
		t.Fatalf("visible user record = %#v", visibleUser)
	}
}

func numberValue(value any) float64 {
	n, _ := value.(float64)
	return n
}
