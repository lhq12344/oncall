package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// CheckPointStore implements the Eino checkpoint seam with Redis.
type CheckPointStore struct {
	client *Client
	prefix string
	ttl    time.Duration
}

// NewCheckPointStore creates a Redis-backed checkpoint adapter.
func NewCheckPointStore(client *Client, prefix string, ttl time.Duration) *CheckPointStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &CheckPointStore{
		client: client,
		prefix: prefix,
		ttl:    ttl,
	}
}

func (s *CheckPointStore) Get(ctx context.Context, checkpointID string) ([]byte, bool, error) {
	data, err := s.client.client.Get(ctx, s.key(checkpointID)).Bytes()
	if err == goredis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (s *CheckPointStore) Set(ctx context.Context, checkpointID string, checkpoint []byte) error {
	return s.client.client.Set(ctx, s.key(checkpointID), checkpoint, s.ttl).Err()
}

func (s *CheckPointStore) key(checkpointID string) string {
	return fmt.Sprintf("%s:checkpoint:%s", s.prefix, checkpointID)
}
