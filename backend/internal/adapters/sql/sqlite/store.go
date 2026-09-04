package sqlite

import (
	"strings"

	"go_agent/internal/improvement"
)

func NewStore(path string) improvement.CaseStore {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(path) == ":memory:" {
		return improvement.NewMemoryCaseStore()
	}
	store, err := improvement.NewFileCaseStore(path)
	if err != nil {
		return improvement.NewMemoryCaseStore()
	}
	return store
}
