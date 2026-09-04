package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type EventType string

const Schema = "oncall.event/v1"

const (
	EventRunStarted       EventType = "run.started"
	EventRunCompleted     EventType = "run.completed"
	EventPhaseStarted     EventType = "phase.started"
	EventPhaseCompleted   EventType = "phase.completed"
	EventModelDelta       EventType = "model.delta"
	EventToken            EventType = "message.token"
	EventToolRequested    EventType = "tool.requested"
	EventToolStarted      EventType = "tool.started"
	EventToolResult       EventType = "tool.result"
	EventToolCall         EventType = "tool.call"
	EventApprovalRequired EventType = "approval.required"
	EventApprovalResolved EventType = "approval.resolved"
	EventWorkflowState    EventType = "workflow.state"
	EventRAGRetrieval     EventType = "rag.retrieval"
	EventContextCompacted EventType = "context.compacted"
	EventArtifactCreated  EventType = "artifact.created"
	EventInterrupt        EventType = "run.interrupt"
	EventError            EventType = "error"
	EventRunFinished      EventType = "run.finished"
	EventRunFailed        EventType = "run.failed"
)

// RunEvent is the single versioned event envelope shared by transports and UI.
type RunEvent struct {
	Version   string         `json:"version"`
	ID        string         `json:"id"`
	RunID     string         `json:"run_id"`
	TraceID   string         `json:"trace_id,omitempty"`
	Sequence  int64          `json:"sequence"`
	Type      EventType      `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Payload   map[string]any `json:"payload,omitempty"`
}

func New(runID string, seq int64, typ EventType, payload map[string]any) RunEvent {
	ts := time.Now().UTC()
	return RunEvent{Version: Schema, ID: fmt.Sprintf("%s-%06d", runID, seq), RunID: runID, Sequence: seq, Type: typ, Timestamp: ts, Payload: payload}
}

type Sink interface {
	Emit(context.Context, RunEvent) error
}

type Emitter struct {
	sink    Sink
	runID   string
	traceID string
	seq     atomic.Int64
}

func NewEmitter(runID, traceID string, sink Sink) (*Emitter, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("run id is required")
	}
	return &Emitter{runID: runID, traceID: strings.TrimSpace(traceID), sink: sink}, nil
}

func (e *Emitter) Emit(ctx context.Context, typ EventType, payload map[string]any) (RunEvent, error) {
	if e == nil {
		return RunEvent{}, fmt.Errorf("emitter is nil")
	}
	seq := e.seq.Add(1)
	event := New(e.runID, seq, typ, payload)
	event.TraceID = e.traceID
	if err := event.Validate(); err != nil {
		return RunEvent{}, err
	}
	if e.sink != nil {
		if err := e.sink.Emit(ctx, event); err != nil {
			return RunEvent{}, err
		}
	}
	return event, nil
}

type MemorySink struct {
	mu     sync.Mutex
	events []RunEvent
}

func (s *MemorySink) Emit(_ context.Context, event RunEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func (s *MemorySink) Events() []RunEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RunEvent, len(s.events))
	copy(out, s.events)
	return out
}

func (e RunEvent) Validate() error {
	if e.Version != Schema {
		return fmt.Errorf("unsupported event version %q", e.Version)
	}
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("event id is required")
	}
	if strings.TrimSpace(e.RunID) == "" {
		return fmt.Errorf("run id is required")
	}
	if e.Sequence < 0 {
		return fmt.Errorf("event sequence must be non-negative")
	}
	if strings.TrimSpace(string(e.Type)) == "" {
		return fmt.Errorf("event type is required")
	}
	if e.Timestamp.IsZero() {
		return fmt.Errorf("event timestamp is required")
	}
	return nil
}

func (e RunEvent) MarshalJSONLine() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
