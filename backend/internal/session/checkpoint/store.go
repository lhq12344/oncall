package checkpoint

import "context"

type Checkpoint struct {
	ID                  string
	SchemaVersion       string
	WorkflowVersion     string
	PendingInterruptIDs []string
	IdempotencyReceipts map[string]string
	EventCursor         string
	State               []byte
}

type Store interface {
	Save(context.Context, Checkpoint) error
	Load(context.Context, string) (Checkpoint, bool, error)
}
