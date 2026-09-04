package context

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type memoryItem struct {
	payload   []byte
	expiresAt time.Time
}

// MemoryStorage implements Storage in process memory.
type MemoryStorage struct {
	mu         sync.Mutex
	prefix     string
	sessions   map[string]memoryItem
	agents     map[string]memoryItem
	executions map[string]memoryItem
}

func NewMemoryStorage(prefix string) *MemoryStorage {
	return &MemoryStorage{
		prefix:     strings.TrimSpace(prefix),
		sessions:   make(map[string]memoryItem),
		agents:     make(map[string]memoryItem),
		executions: make(map[string]memoryItem),
	}
}

func (s *MemoryStorage) SaveSession(ctx context.Context, sessionID string, data []byte, ttl time.Duration) error {
	return s.save(ctx, s.sessions, sessionID, data, ttl)
}

func (s *MemoryStorage) LoadSession(ctx context.Context, sessionID string) (*SessionContext, error) {
	var session SessionContext
	if err := s.load(ctx, s.sessions, sessionID, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *MemoryStorage) DeleteSession(ctx context.Context, sessionID string) error {
	return s.delete(ctx, s.sessions, sessionID)
}

func (s *MemoryStorage) SaveAgentContext(ctx context.Context, agentID string, data []byte, ttl time.Duration) error {
	return s.save(ctx, s.agents, agentID, data, ttl)
}

func (s *MemoryStorage) LoadAgentContext(ctx context.Context, agentID string) (*AgentContext, error) {
	var agentCtx AgentContext
	if err := s.load(ctx, s.agents, agentID, &agentCtx); err != nil {
		return nil, err
	}
	return &agentCtx, nil
}

func (s *MemoryStorage) SaveExecutionContext(ctx context.Context, executionID string, data []byte, ttl time.Duration) error {
	return s.save(ctx, s.executions, executionID, data, ttl)
}

func (s *MemoryStorage) LoadExecutionContext(ctx context.Context, executionID string) (*ExecutionContext, error) {
	var execCtx ExecutionContext
	if err := s.load(ctx, s.executions, executionID, &execCtx); err != nil {
		return nil, err
	}
	return &execCtx, nil
}

func (s *MemoryStorage) ListSessions(ctx context.Context, pattern string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteExpiredLocked(s.sessions)

	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		pattern = "*"
	}
	keys := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		matched, err := filepath.Match(pattern, id)
		if err != nil {
			return nil, err
		}
		if matched {
			keys = append(keys, s.storageKey("session", id))
		}
	}
	return keys, nil
}

func (s *MemoryStorage) DeleteExpiredSessions(ctx context.Context, before time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, item := range s.sessions {
		if !item.expiresAt.IsZero() && item.expiresAt.Before(before) {
			delete(s.sessions, id)
		}
	}
	return nil
}

func (s *MemoryStorage) save(ctx context.Context, bucket map[string]memoryItem, id string, data []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("storage id is required")
	}
	item := memoryItem{payload: append([]byte(nil), data...)}
	if ttl > 0 {
		item.expiresAt = time.Now().Add(ttl)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket[id] = item
	return nil
}

func (s *MemoryStorage) load(ctx context.Context, bucket map[string]memoryItem, id string, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := bucket[id]
	if !ok {
		return fmt.Errorf("storage item not found: %s", id)
	}
	if !item.expiresAt.IsZero() && time.Now().After(item.expiresAt) {
		delete(bucket, id)
		return fmt.Errorf("storage item expired: %s", id)
	}
	return json.Unmarshal(item.payload, out)
}

func (s *MemoryStorage) delete(ctx context.Context, bucket map[string]memoryItem, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(bucket, id)
	return nil
}

func (s *MemoryStorage) deleteExpiredLocked(bucket map[string]memoryItem) {
	now := time.Now()
	for id, item := range bucket {
		if !item.expiresAt.IsZero() && now.After(item.expiresAt) {
			delete(bucket, id)
		}
	}
}

func (s *MemoryStorage) storageKey(kind, id string) string {
	if s.prefix == "" {
		return kind + ":" + id
	}
	return s.prefix + ":" + kind + ":" + id
}
