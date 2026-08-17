package main

import (
	"os"
	"path/filepath"
	"testing"

	"go_agent/internal/rag"
)

func TestNormalizeChunksAddsV2Fields(t *testing.T) {
	got := normalizeChunks([]rag.DocumentChunk{{ID: "a", Content: " redis timeout "}}, "knowledge")
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
	if got[0].SourceType != "knowledge" || got[0].ChunkID != "a" || got[0].ContentHash == "" {
		t.Fatalf("chunk not normalized: %#v", got[0])
	}
}

func TestEvalCasesRunsOfflineBM25(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RAG_BM25_ROOT", root)
	idx, err := rag.NewProfileBM25Index(root, rag.ProfileKnowledge)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(nil, []rag.DocumentChunk{{ID: "doc1", ChunkID: "doc1", Content: "redis timeout runbook"}}); err != nil {
		t.Fatal(err)
	}
	gold := filepath.Join(t.TempDir(), "gold.jsonl")
	if err := os.WriteFile(gold, []byte("{\"id\":\"case1\",\"profile\":\"knowledge\",\"query\":\"redis timeout\",\"expected_ids\":[\"doc1\"]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := evalCases(nil, []string{"-gold", gold, "-profile", "knowledge"}); err != nil {
		t.Fatal(err)
	}
}
