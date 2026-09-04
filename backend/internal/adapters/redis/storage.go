package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appcontext "go_agent/internal/context"
)

// Storage implements the context storage seam with Redis.
type Storage struct {
	client *Client
	prefix string
}

// NewStorage creates a Redis-backed context storage adapter.
func NewStorage(client *Client, prefix string) *Storage {
	return &Storage{
		client: client,
		prefix: prefix,
	}
}

func (s *Storage) SaveSession(ctx context.Context, sessionID string, data []byte, ttl time.Duration) error {
	return s.client.client.Set(ctx, s.key("session", sessionID), data, ttl).Err()
}

func (s *Storage) LoadSession(ctx context.Context, sessionID string) (*appcontext.SessionContext, error) {
	data, err := s.client.client.Get(ctx, s.key("session", sessionID)).Bytes()
	if err != nil {
		return nil, err
	}

	var session appcontext.SessionContext
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *Storage) DeleteSession(ctx context.Context, sessionID string) error {
	return s.client.client.Del(ctx, s.key("session", sessionID)).Err()
}

func (s *Storage) SaveAgentContext(ctx context.Context, agentID string, data []byte, ttl time.Duration) error {
	return s.client.client.Set(ctx, s.key("agent", agentID), data, ttl).Err()
}

func (s *Storage) LoadAgentContext(ctx context.Context, agentID string) (*appcontext.AgentContext, error) {
	data, err := s.client.client.Get(ctx, s.key("agent", agentID)).Bytes()
	if err != nil {
		return nil, err
	}

	var agentContext appcontext.AgentContext
	if err := json.Unmarshal(data, &agentContext); err != nil {
		return nil, err
	}
	return &agentContext, nil
}

func (s *Storage) SaveExecutionContext(ctx context.Context, executionID string, data []byte, ttl time.Duration) error {
	return s.client.client.Set(ctx, s.key("execution", executionID), data, ttl).Err()
}

func (s *Storage) LoadExecutionContext(ctx context.Context, executionID string) (*appcontext.ExecutionContext, error) {
	data, err := s.client.client.Get(ctx, s.key("execution", executionID)).Bytes()
	if err != nil {
		return nil, err
	}

	var executionContext appcontext.ExecutionContext
	if err := json.Unmarshal(data, &executionContext); err != nil {
		return nil, err
	}
	return &executionContext, nil
}

func (s *Storage) ListSessions(ctx context.Context, pattern string) ([]string, error) {
	return s.client.client.Keys(ctx, s.key("session", pattern)).Result()
}

func (s *Storage) DeleteExpiredSessions(context.Context, time.Time) error {
	return nil
}

func (s *Storage) key(kind, id string) string {
	return fmt.Sprintf("%s:%s:%s", s.prefix, kind, id)
}

var _ appcontext.Storage = (*Storage)(nil)
