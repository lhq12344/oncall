package transcript

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu       sync.Mutex
	messages map[string][]Message
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{messages: map[string][]Message{}} }

func (s *MemoryStore) Load(ctx context.Context, sessionID string) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Message(nil), s.messages[sessionID]...)
	return out, nil
}

func (s *MemoryStore) Append(ctx context.Context, sessionID string, turn Turn) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	if turn.CreatedAt.IsZero() {
		turn.CreatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[sessionID] = append(s.messages[sessionID], turn.User, turn.Assistant)
	return nil
}

func (s *MemoryStore) Compact(ctx context.Context, sessionID string, keepMessages int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if keepMessages <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.messages[sessionID]
	if len(items) > keepMessages {
		s.messages[sessionID] = append([]Message(nil), items[len(items)-keepMessages:]...)
	}
	return nil
}
