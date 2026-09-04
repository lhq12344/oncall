package sse

import (
	"strings"
	"testing"

	"go_agent/internal/events"
)

func TestEncoderWritesVersionedRunEventFrame(t *testing.T) {
	event := events.New("run-1", 1, events.EventRunStarted, map[string]any{"route": "dialogue"})
	event.TraceID = "trace-1"
	frame, err := (Encoder{}).Encode(event)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	text := string(frame)
	for _, want := range []string{"id: run-1-000001", "event: run.started", "data:", "oncall.event/v1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("frame missing %q: %s", want, text)
		}
	}
}

func TestReplayAfterReturnsOnlyMissingEvents(t *testing.T) {
	eventsList := []events.RunEvent{
		events.New("run-1", 1, events.EventRunStarted, nil),
		events.New("run-1", 2, events.EventModelDelta, nil),
		events.New("run-1", 3, events.EventRunCompleted, nil),
	}
	missing := ReplayAfter(eventsList, "run-1-000001")
	if len(missing) != 2 || missing[0].ID != "run-1-000002" {
		t.Fatalf("unexpected replay: %+v", missing)
	}
}
