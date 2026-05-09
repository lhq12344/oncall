package session

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTraceMetaFieldsIncludeUserLanguageTag(t *testing.T) {
	event := OrchestrationTraceEvent{
		SessionID: "s1",
		TurnID:    "t1",
		Source:    "graph",
		Tags: map[string]string{
			"user_language": "ja",
			"experiment":    "rag-v2",
		},
	}

	meta := traceMetaFields(event)
	if meta["user_language"] != "ja" {
		t.Fatalf("user_language meta = %#v, want ja", meta["user_language"])
	}
	if _, ok := meta["tags"]; !ok {
		t.Fatalf("trace meta missing tags: %#v", meta)
	}

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if got := string(payload); !strings.Contains(got, `"user_language":"ja"`) {
		t.Fatalf("event payload missing user_language tag: %s", got)
	}
}

func TestNormalizeTraceTagsDropsEmptyValues(t *testing.T) {
	tags := normalizeTraceTags(map[string]string{
		"user_language": " zh ",
		"empty":         "   ",
		"":              "noop",
	})
	if len(tags) != 1 || tags["user_language"] != "zh" {
		t.Fatalf("normalized tags = %#v, want only user_language=zh", tags)
	}
}

func TestShouldPersistTraceEventOnlyAllowsNormalVisibleTurns(t *testing.T) {
	tests := []struct {
		name  string
		event OrchestrationTraceEvent
		want  bool
	}{
		{
			name:  "normal visible turn",
			event: NewVisibleTurnTraceEvent("s1", "t1", "c1", "chat", "hello", "hi"),
			want:  true,
		},
		{
			name: "tool result is skipped",
			event: OrchestrationTraceEvent{
				SessionID:      "s1",
				TurnID:         "t1",
				EventType:      "tool_result",
				Status:         "success",
				CompactPayload: `{"result":"secret tool payload"}`,
			},
			want: false,
		},
		{
			name: "error reply is skipped",
			event: OrchestrationTraceEvent{
				SessionID:      "s1",
				TurnID:         "t1",
				EventType:      traceEventTypeVisibleTurn,
				Status:         "success",
				UserQuestion:   "hello",
				AssistantReply: "[ERROR] upstream failed",
				CompactPayload: "",
				ErrorSummary:   "",
			},
			want: false,
		},
		{
			name: "failed save event is skipped",
			event: OrchestrationTraceEvent{
				SessionID:    "s1",
				TurnID:       "t1",
				EventType:    traceEventTypeVisibleTurn,
				Status:       "error",
				UserQuestion: "hello",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldPersistTraceEvent(tt.event); got != tt.want {
				t.Fatalf("shouldPersistTraceEvent() = %v, want %v", got, tt.want)
			}
		})
	}
}
