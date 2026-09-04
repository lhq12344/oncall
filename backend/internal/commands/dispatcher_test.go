package commands

import (
	"context"
	"strings"
	"testing"

	"go_agent/internal/commands/slash"
)

func TestDispatcherReturnsStrongTypedCommandResult(t *testing.T) {
	reg := slash.NewRegistry()
	if err := reg.Register(slash.Command{Name: "clear", Type: slash.TypeClientAction, Source: slash.SourceBuiltin, Builtin: true, Handler: func(*slash.Context) (slash.Result, error) {
		return slash.Result{Type: slash.TypeClientAction, Action: "clear_session", Payload: map[string]any{"scope": "current"}}, nil
	}}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	result, handled, err := NewDispatcher(NewRegistry(reg)).Dispatch(context.Background(), "/clear", nil)
	if err != nil || !handled {
		t.Fatalf("Dispatch err=%v handled=%v", err, handled)
	}
	if result.Type != TypeClientAction || result.Action != "clear_session" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestDispatcherRejectsUnknownSlashCommand(t *testing.T) {
	_, handled, err := NewDispatcher(LoadDefault(".")).Dispatch(context.Background(), "/missing", nil)
	if !handled || err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
}
