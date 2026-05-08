package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type PendingTurn struct {
	CheckpointID     string `json:"checkpoint_id"`
	SessionID        string `json:"session_id"`
	TurnID           string `json:"turn_id"`
	OriginalQuestion string `json:"original_question"`
	Source           string `json:"source"`
	CreatedAt        string `json:"created_at"`
}

type PendingTurnStore interface {
	SavePendingTurn(ctx context.Context, turn PendingTurn) error
	GetPendingTurn(ctx context.Context, checkpointID string) (*PendingTurn, error)
	MarkSavedOnce(ctx context.Context, checkpointID string) (bool, error)
}

type MemoryPendingTurnStore struct {
	mu    sync.Mutex
	turns map[string]PendingTurn
	saved map[string]struct{}
}

func NewMemoryPendingTurnStore() *MemoryPendingTurnStore {
	return &MemoryPendingTurnStore{
		turns: make(map[string]PendingTurn),
		saved: make(map[string]struct{}),
	}
}

func (m *MemoryPendingTurnStore) SavePendingTurn(ctx context.Context, turn PendingTurn) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	checkpointID := strings.TrimSpace(turn.CheckpointID)
	if checkpointID == "" {
		return fmt.Errorf("checkpoint id is required")
	}
	if strings.TrimSpace(turn.CreatedAt) == "" {
		turn.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns[checkpointID] = turn
	return nil
}

func (m *MemoryPendingTurnStore) GetPendingTurn(ctx context.Context, checkpointID string) (*PendingTurn, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	checkpointID = strings.TrimSpace(checkpointID)
	m.mu.Lock()
	defer m.mu.Unlock()
	turn, ok := m.turns[checkpointID]
	if !ok {
		return nil, nil
	}
	cp := turn
	return &cp, nil
}

func (m *MemoryPendingTurnStore) MarkSavedOnce(ctx context.Context, checkpointID string) (bool, error) {
	if err := ctxErr(ctx); err != nil {
		return false, err
	}
	checkpointID = strings.TrimSpace(checkpointID)
	if checkpointID == "" {
		return false, fmt.Errorf("checkpoint id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.saved[checkpointID]; ok {
		return false, nil
	}
	m.saved[checkpointID] = struct{}{}
	return true, nil
}

type RedisPendingTurnStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

func NewRedisPendingTurnStore(client *redis.Client, prefix string, ttl time.Duration) PendingTurnStore {
	if client == nil {
		return NewMemoryPendingTurnStore()
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "oncall"
	}
	if ttl <= 0 {
		ttl = defaultTraceTTL
	}
	return &RedisPendingTurnStore{client: client, prefix: prefix, ttl: ttl}
}

func (r *RedisPendingTurnStore) SavePendingTurn(ctx context.Context, turn PendingTurn) error {
	if r == nil || r.client == nil {
		return nil
	}
	turn.CheckpointID = strings.TrimSpace(turn.CheckpointID)
	if turn.CheckpointID == "" {
		return fmt.Errorf("checkpoint id is required")
	}
	if strings.TrimSpace(turn.CreatedAt) == "" {
		turn.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	payload, err := json.Marshal(turn)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.pendingKey(turn.CheckpointID), string(payload), r.ttl).Err()
}

func (r *RedisPendingTurnStore) GetPendingTurn(ctx context.Context, checkpointID string) (*PendingTurn, error) {
	if r == nil || r.client == nil {
		return nil, nil
	}
	checkpointID = strings.TrimSpace(checkpointID)
	if checkpointID == "" {
		return nil, nil
	}
	raw, err := r.client.Get(ctx, r.pendingKey(checkpointID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var turn PendingTurn
	if err := json.Unmarshal([]byte(raw), &turn); err != nil {
		return nil, err
	}
	return &turn, nil
}

func (r *RedisPendingTurnStore) MarkSavedOnce(ctx context.Context, checkpointID string) (bool, error) {
	if r == nil || r.client == nil {
		return true, nil
	}
	checkpointID = strings.TrimSpace(checkpointID)
	if checkpointID == "" {
		return false, fmt.Errorf("checkpoint id is required")
	}
	ok, err := r.client.SetNX(ctx, r.savedKey(checkpointID), time.Now().UTC().Format(time.RFC3339Nano), r.ttl).Result()
	return ok, err
}

func (r *RedisPendingTurnStore) pendingKey(checkpointID string) string {
	return r.prefix + ":pending_turn:" + checkpointID
}

func (r *RedisPendingTurnStore) savedKey(checkpointID string) string {
	return r.prefix + ":pending_turn_saved:" + checkpointID
}
