package ingest

import (
	"context"

	"go_agent/internal/rag"
)

type IndexWriter interface {
	WriteChunks(context.Context, Manifest, []rag.DocumentChunk) error
}

type MemoryIndexWriter struct {
	Written []rag.DocumentChunk
}

func (w *MemoryIndexWriter) WriteChunks(_ context.Context, _ Manifest, chunks []rag.DocumentChunk) error {
	w.Written = append(w.Written, chunks...)
	return nil
}
