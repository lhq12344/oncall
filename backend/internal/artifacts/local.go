package artifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type LocalStore struct {
	Root string
}

func NewLocalStore(root string) *LocalStore { return &LocalStore{Root: root} }

func (s *LocalStore) Put(ctx context.Context, kind string, data []byte) (Ref, error) {
	if err := ctx.Err(); err != nil {
		return Ref{}, err
	}
	redacted := Redact(data)
	sum := sha256.Sum256(redacted)
	id := hex.EncodeToString(sum[:])
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return Ref{}, err
	}
	path := filepath.Join(s.Root, id+".artifact")
	if err := os.WriteFile(path, redacted, 0o600); err != nil {
		return Ref{}, err
	}
	return Ref{ID: id, Kind: kind, URI: path, Hash: id, CreatedAt: time.Now().UTC()}, nil
}

func (s *LocalStore) Get(ctx context.Context, id string) ([]byte, Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, Ref{}, err
	}
	path := filepath.Join(s.Root, id+".artifact")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, Ref{}, err
	}
	return b, Ref{ID: id, URI: path, Hash: id}, nil
}

func (s *LocalStore) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("artifact id is required")
	}
	return os.Remove(filepath.Join(s.Root, id+".artifact"))
}
