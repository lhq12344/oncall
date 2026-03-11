package context

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCheckPointStore 基于 Redis 的 ADK CheckPointStore 实现。
type RedisCheckPointStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// NewRedisCheckPointStore 创建 Redis checkpoint store。
func NewRedisCheckPointStore(client *redis.Client, prefix string, ttl time.Duration) *RedisCheckPointStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &RedisCheckPointStore{
		client: client,
		prefix: prefix,
		ttl:    ttl,
	}
}

// Get 获取 checkpoint 数据。
func (r *RedisCheckPointStore) Get(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	key := fmt.Sprintf("%s:checkpoint:%s", r.prefix, checkPointID)
	data, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// Set 保存 checkpoint 数据。
func (r *RedisCheckPointStore) Set(ctx context.Context, checkPointID string, checkPoint []byte) error {
	key := fmt.Sprintf("%s:checkpoint:%s", r.prefix, checkPointID)
	return r.client.Set(ctx, key, checkPoint, r.ttl).Err()
}
