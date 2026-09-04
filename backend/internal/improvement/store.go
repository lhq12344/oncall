package improvement

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type CaseStore interface {
	SaveCase(context.Context, Case) error
	ListCases(context.Context) ([]Case, error)
}

type MemoryCaseStore struct {
	mu    sync.Mutex
	cases []Case
}

func NewMemoryCaseStore() *MemoryCaseStore { return &MemoryCaseStore{} }

func (s *MemoryCaseStore) SaveCase(ctx context.Context, item Case) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cases = append(s.cases, item)
	return nil
}

func (s *MemoryCaseStore) ListCases(ctx context.Context) ([]Case, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Case(nil), s.cases...)
	return out, nil
}

// FileCaseStore is the dependency-free durable MVP store used by the local
// SQLite adapter. It keeps the CaseStore seam independent from a driver while
// preserving restart durability through atomic replace.
type FileCaseStore struct {
	mu   *sync.Mutex
	path string
}

var fileCaseStoreLocks sync.Map

func NewFileCaseStore(path string) (*FileCaseStore, error) {
	path = filepath.Clean(path)
	if path == "." || path == "" {
		return nil, fmt.Errorf("case store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	lock, _ := fileCaseStoreLocks.LoadOrStore(path, &sync.Mutex{})
	return &FileCaseStore{path: path, mu: lock.(*sync.Mutex)}, nil
}

func (s *FileCaseStore) SaveCase(ctx context.Context, item Case) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.readLocked()
	if err != nil {
		return err
	}
	items = append(items, item)
	return s.writeLocked(items)
}

func (s *FileCaseStore) ListCases(ctx context.Context) ([]Case, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	return append([]Case(nil), items...), nil
}

func (s *FileCaseStore) readLocked() ([]Case, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return []Case{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return []Case{}, nil
	}
	var items []Case
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("decode case store: %w", err)
	}
	return items, nil
}

func (s *FileCaseStore) writeLocked(items []Case) error {
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("encode case store: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".cases-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}
