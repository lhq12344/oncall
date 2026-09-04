package context

import (
	"context"
	"testing"
	"time"
)

func TestContextManager(t *testing.T) {
	storage := NewMemoryStorage("oncall:test")
	cm := NewContextManager(storage)
	ctx := context.Background()

	t.Run("CreateSession", func(t *testing.T) {
		session, err := cm.CreateSession(ctx, "user123")
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}
		if session.SessionID == "" {
			t.Error("session ID is empty")
		}
		if session.UserID != "user123" {
			t.Errorf("expected user ID 'user123', got %q", session.UserID)
		}
	})

	t.Run("GetSession", func(t *testing.T) {
		session, err := cm.CreateSession(ctx, "user456")
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}
		retrieved, err := cm.GetSession(ctx, session.SessionID)
		if err != nil {
			t.Fatalf("failed to get session: %v", err)
		}
		if retrieved.SessionID != session.SessionID {
			t.Errorf("expected session ID %q, got %q", session.SessionID, retrieved.SessionID)
		}
	})

	t.Run("AddMessage", func(t *testing.T) {
		session, err := cm.CreateSession(ctx, "user789")
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}
		if err := cm.AddMessage(ctx, session.SessionID, "user", "Hello, world!"); err != nil {
			t.Fatalf("failed to add message: %v", err)
		}
		history, err := cm.GetHistory(ctx, session.SessionID, 10)
		if err != nil {
			t.Fatalf("failed to get history: %v", err)
		}
		if len(history) != 1 {
			t.Errorf("expected 1 message, got %d", len(history))
		}
		if history[0].Content != "Hello, world!" {
			t.Errorf("expected content 'Hello, world!', got %q", history[0].Content)
		}
	})

	t.Run("CreateAgentContext", func(t *testing.T) {
		agentCtx, err := cm.CreateAgentContext(ctx, "supervisor")
		if err != nil {
			t.Fatalf("failed to create agent context: %v", err)
		}
		if agentCtx.AgentID == "" {
			t.Error("agent ID is empty")
		}
		if agentCtx.AgentType != "supervisor" {
			t.Errorf("expected agent type 'supervisor', got %q", agentCtx.AgentType)
		}
	})

	t.Run("CreateExecutionContext", func(t *testing.T) {
		execCtx, err := cm.CreateExecutionContext(ctx, "plan123")
		if err != nil {
			t.Fatalf("failed to create execution context: %v", err)
		}
		if execCtx.ExecutionID == "" {
			t.Error("execution ID is empty")
		}
		if execCtx.PlanID != "plan123" {
			t.Errorf("expected plan ID 'plan123', got %q", execCtx.PlanID)
		}
	})

	t.Run("MigrateToL2", func(t *testing.T) {
		session, err := cm.CreateSession(ctx, "olduser")
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}
		session.LastActive = time.Now().Add(-time.Hour)
		if err := cm.MigrateToL2(ctx); err != nil {
			t.Fatalf("failed to migrate: %v", err)
		}
		if _, err := storage.LoadSession(ctx, session.SessionID); err != nil {
			t.Fatalf("migrated session was not saved to storage: %v", err)
		}
	})
}
