package session

import (
	"context"
	"testing"
)

func TestMemoryPendingTurnStoreMarksSavedOnce(t *testing.T) {
	store := NewMemoryPendingTurnStore()
	turn := PendingTurn{
		CheckpointID:     "cp-1",
		SessionID:        "session-1",
		TurnID:           "turn-1",
		OriginalQuestion: "原始问题",
		Source:           "chat_stream_graph",
	}
	if err := store.SavePendingTurn(context.Background(), turn); err != nil {
		t.Fatalf("save pending turn: %v", err)
	}
	loaded, err := store.GetPendingTurn(context.Background(), "cp-1")
	if err != nil {
		t.Fatalf("get pending turn: %v", err)
	}
	if loaded == nil || loaded.OriginalQuestion != "原始问题" {
		t.Fatalf("loaded turn = %#v", loaded)
	}

	ok, err := store.MarkSavedOnce(context.Background(), "cp-1")
	if err != nil || !ok {
		t.Fatalf("first mark saved = %v, %v; want true, nil", ok, err)
	}
	ok, err = store.MarkSavedOnce(context.Background(), "cp-1")
	if err != nil || ok {
		t.Fatalf("second mark saved = %v, %v; want false, nil", ok, err)
	}
}
