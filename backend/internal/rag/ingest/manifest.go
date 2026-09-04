package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"go_agent/internal/rag"
)

type Manifest struct {
	Version      string              `json:"version"`
	IndexVersion string              `json:"index_version"`
	Chunks       []rag.DocumentChunk `json:"chunks"`
}

func (m Manifest) Hash() (string, error) {
	chunks := append([]rag.DocumentChunk(nil), m.Chunks...)
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].ChunkID < chunks[j].ChunkID })
	m.Chunks = chunks
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
