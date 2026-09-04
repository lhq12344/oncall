package events

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRunEventValidationAndJSONLine(t *testing.T) {
	e := New("run-1", 1, EventRunStarted, map[string]any{"route": "chat"})
	b, err := e.MarshalJSONLine()
	if err != nil {
		t.Fatalf("MarshalJSONLine: %v", err)
	}
	var decoded RunEvent
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if decoded.Version != Schema || decoded.ID != "run-1-000001" {
		t.Fatalf("unexpected event: %+v", decoded)
	}
}

func TestEmitterCreatesMonotonicEvents(t *testing.T) {
	sink := &MemorySink{}
	emitter, err := NewEmitter("run-1", "trace-1", sink)
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}
	first, err := emitter.Emit(context.Background(), EventRunStarted, nil)
	if err != nil {
		t.Fatalf("Emit first: %v", err)
	}
	second, err := emitter.Emit(context.Background(), EventRunCompleted, map[string]any{"status": "ok"})
	if err != nil {
		t.Fatalf("Emit second: %v", err)
	}
	if first.Sequence != 1 || second.Sequence != 2 || second.TraceID != "trace-1" {
		t.Fatalf("unexpected events: %+v %+v", first, second)
	}
	if len(sink.Events()) != 2 {
		t.Fatalf("sink events=%d, want 2", len(sink.Events()))
	}
}
