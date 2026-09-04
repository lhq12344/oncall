package transcript

import (
	"context"
	"time"
)

type Message struct {
	Role      string
	Content   string
	CreatedAt time.Time
}

type Turn struct {
	User             Message
	Assistant        Message
	PromptTokenCount int
	OutputTokenCount int
	CreatedAt        time.Time
}

type Store interface {
	Load(context.Context, string) ([]Message, error)
	Append(context.Context, string, Turn) error
	Compact(context.Context, string, int) error
}
