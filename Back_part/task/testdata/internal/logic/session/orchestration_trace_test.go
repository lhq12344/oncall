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
