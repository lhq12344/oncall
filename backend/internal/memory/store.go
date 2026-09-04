package memory

import (
	"context"
	"time"
)

type Record struct {
	ID          string
	Source      string
	Scope       string
	Confidence  float64
	Owner       string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	ContentHash string
	Provenance  string
	Content     string
}

type Store interface {
	Upsert(context.Context, []Record) error
	Delete(context.Context, string) error
	Search(context.Context, Query) ([]Record, error)
}

type Query struct {
	Scope string
	Text  string
	Now   time.Time
}
