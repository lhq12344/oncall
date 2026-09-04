package mcp

import "time"

type ServerConfig struct {
	Name        string
	Allowed     []string
	Denied      []string
	Credentials map[string]string
	Timeout     time.Duration
	Concurrency int
	Instruction string
}

func (c ServerConfig) Normalize() ServerConfig {
	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Second
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 1
	}
	return c
}
