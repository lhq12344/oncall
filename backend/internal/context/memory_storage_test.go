package context

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestMemoryStorageRoundTripsSessionWithoutRedis(t *testing.T) {
	t.Parallel()

	storage := NewMemoryStorage("oncall:test")
	createdAt := time.Now().UTC().Truncate(time.Second)
	session := &SessionContext{
		SessionID:  "s1",
		UserID:     "user1",
		CreatedAt:  createdAt,
		LastActive: createdAt,
		History: []*Message{{
			Role:      "user",
			Content:   "hello",
			Timestamp: createdAt,
		}},
		Metadata: map[string]interface{}{"source": "test"},
	}
	payload, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}

	if err := storage.SaveSession(context.Background(), session.SessionID, payload, time.Hour); err != nil {
		t.Fatalf("SaveSession returned error: %v", err)
	}
	loaded, err := storage.LoadSession(context.Background(), session.SessionID)
	if err != nil {
		t.Fatalf("LoadSession returned error: %v", err)
	}
	if loaded.SessionID != session.SessionID || loaded.UserID != session.UserID {
		t.Fatalf("loaded session=%+v, want id=%s user=%s", loaded, session.SessionID, session.UserID)
	}
	if len(loaded.History) != 1 || loaded.History[0].Content != "hello" {
		t.Fatalf("loaded history=%+v", loaded.History)
	}
}

func TestContextManagerCanMigrateToMemoryStorage(t *testing.T) {
	t.Parallel()

	storage := NewMemoryStorage("oncall:test")
	manager := NewContextManager(storage)
	ctx := context.Background()
	session, err := manager.CreateSession(ctx, "inactive-user")
	if err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	session.LastActive = time.Now().Add(-time.Hour)

	if err := manager.MigrateToL2(ctx); err != nil {
		t.Fatalf("MigrateToL2 returned error: %v", err)
	}
	if got := manager.ListActiveSessions(ctx); len(got) != 0 {
		t.Fatalf("active sessions after migration=%d, want 0", len(got))
	}
	loaded, err := storage.LoadSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("LoadSession from memory storage returned error: %v", err)
	}
	if loaded.SessionID != session.SessionID {
		t.Fatalf("loaded migrated session id=%s, want %s", loaded.SessionID, session.SessionID)
	}
}
