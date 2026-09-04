package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ResolveUnderRoot(root, requested string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	requested = strings.TrimSpace(requested)
	if filepath.IsAbs(requested) {
		return "", fmt.Errorf("absolute skill paths are not allowed")
	}
	joined, err := filepath.Abs(filepath.Join(rootAbs, requested))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("skill path escapes allowed root")
	}
	if info, err := os.Lstat(joined); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("skill symlinks are not allowed")
	}
	return joined, nil
}

func AllowedToolsSubset(parent, requested []string) bool {
	allowed := map[string]bool{}
	for _, tool := range parent {
		allowed[tool] = true
	}
	for _, tool := range requested {
		if !allowed[tool] {
			return false
		}
	}
	return true
}
