package memory

import (
	"context"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu      sync.Mutex
	records map[string]Record
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{records: map[string]Record{}} }

func (s *MemoryStore) Upsert(ctx context.Context, records []Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range records {
		s.records[record.ID] = record
	}
	return nil
}

func (s *MemoryStore) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	return nil
}

func (s *MemoryStore) Search(ctx context.Context, q Query) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if q.Now.IsZero() {
		q.Now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0)
	for _, record := range s.records {
		if record.Scope != q.Scope {
			continue
		}
		if !record.ExpiresAt.IsZero() && !q.Now.Before(record.ExpiresAt) {
			continue
		}
		if q.Text == "" || strings.Contains(strings.ToLower(record.Content), strings.ToLower(q.Text)) {
			out = append(out, record)
		}
	}
	return out, nil
}
