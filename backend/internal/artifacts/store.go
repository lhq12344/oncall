package artifacts

import (
	"context"
	"time"
)

type Ref struct {
	ID        string
	Kind      string
	URI       string
	Hash      string
	CreatedAt time.Time
}

type Store interface {
	Put(context.Context, string, []byte) (Ref, error)
	Get(context.Context, string) ([]byte, Ref, error)
	Delete(context.Context, string) error
}
