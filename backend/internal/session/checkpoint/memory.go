package checkpoint

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type MemoryStore struct {
	mu   sync.Mutex
	data map[string]Checkpoint
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{data: map[string]Checkpoint{}} }

func (s *MemoryStore) Save(ctx context.Context, checkpoint Checkpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(checkpoint.ID) == "" {
		return fmt.Errorf("checkpoint id is required")
	}
	if checkpoint.SchemaVersion == "" {
		checkpoint.SchemaVersion = "checkpoint/v1"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	checkpoint.State = append([]byte(nil), checkpoint.State...)
	s.data[checkpoint.ID] = checkpoint
	return nil
}

func (s *MemoryStore) Load(ctx context.Context, id string) (Checkpoint, bool, error) {
	if err := ctx.Err(); err != nil {
		return Checkpoint{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	checkpoint, ok := s.data[id]
	checkpoint.State = append([]byte(nil), checkpoint.State...)
	return checkpoint, ok, nil
}
