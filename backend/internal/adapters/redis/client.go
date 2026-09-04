package redis

import (
	"context"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Config describes the optional Redis connection.
type Config struct {
	Addr        string
	Password    string
	DB          int
	DialTimeout time.Duration
}

// Client owns the Redis SDK client and keeps the SDK type at the adapter seam.
type Client struct {
	client *goredis.Client
}

// Connect creates and verifies a Redis client.
func Connect(ctx context.Context, cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Addr) == "" {
		return nil, fmt.Errorf("redis address is required")
	}

	options := &goredis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	}
	if cfg.DialTimeout > 0 {
		options.DialTimeout = cfg.DialTimeout
	}

	client := goredis.NewClient(options)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}

	return &Client{client: client}, nil
}

// Close releases the Redis connection.
func (c *Client) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}
