package checkpoint

import (
	"context"
	"testing"
)

func TestMemoryCheckpointPreservesResumeFields(t *testing.T) {
	store := NewMemoryStore()
	input := Checkpoint{ID: "cp1", WorkflowVersion: "incident/v1", PendingInterruptIDs: []string{"i1"}, IdempotencyReceipts: map[string]string{"step1": "success"}, EventCursor: "event-1", State: []byte("state")}
	if err := store.Save(context.Background(), input); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok, err := store.Load(context.Background(), "cp1")
	if err != nil || !ok {
		t.Fatalf("Load ok=%v err=%v", ok, err)
	}
	if got.SchemaVersion != "checkpoint/v1" || got.IdempotencyReceipts["step1"] != "success" || got.EventCursor != "event-1" {
		t.Fatalf("unexpected checkpoint: %+v", got)
	}
}
