package transcript

import "path/filepath"

type FileStoreConfig struct {
	Root string
}

func (c FileStoreConfig) SessionPath(sessionID string) string {
	return filepath.Join(c.Root, sessionID+".jsonl")
}
