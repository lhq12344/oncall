package hooks

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/callbacks"
)

func TestEinoCallbackHandlerMapsLifecycleEvents(t *testing.T) {
	t.Parallel()
	engine := NewEngine()
	if err := engine.LoadHooks([]Hook{
		{ID: "start", Event: EventAgentStart, Action: Action{Type: ActionAudit, Message: "start"}},
		{ID: "end", Event: EventAgentEnd, Action: Action{Type: ActionAudit, Message: "end"}},
		{ID: "error", Event: EventAgentError, Action: Action{Type: ActionAudit, Message: "error"}},
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewEinoCallbackHandler(engine, HookContext{SessionID: "s1", CheckpointID: "cp1"})
	info := &callbacks.RunInfo{Name: "dialogue_agent", Type: "agent"}
	ctx := context.Background()

	ctx = handler.OnStart(ctx, info, "input")
	ctx = handler.OnEnd(ctx, info, "output")
	ctx = handler.OnError(ctx, info, errors.New("boom"))

	notes := engine.DrainNotifications(0)
	if len(notes) != 3 {
		t.Fatalf("expected 3 notifications, got %#v", notes)
	}
	if notes[0].Context.AgentName != "dialogue_agent" || notes[0].Context.SessionID != "s1" {
		t.Fatalf("unexpected start context: %#v", notes[0].Context)
	}
	if notes[2].Context.Error != "boom" || notes[2].Result.Event != EventAgentError {
		t.Fatalf("unexpected error context: %#v", notes[2])
	}
}
