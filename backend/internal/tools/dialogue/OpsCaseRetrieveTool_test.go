package tools

import (
	"testing"

	"go_agent/internal/rag"
)

func TestOpsItemsPreserveHybridSource(t *testing.T) {
	items := opsItemsFromRAG([]rag.RetrievedResult{{
		ID:            "case-1",
		Content:       "redis timeout",
		Score:         0.5,
		Source:        "embedding,bm25",
		RetrievalPath: []string{"embedding", "bm25", "rrf"},
		Meta:          map[string]any{"chunk_id": "case-1"},
	}})
	got := ragResultsFromOpsItems(items)
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
	if got[0].Source != "embedding,bm25" {
		t.Fatalf("source lost: %#v", got[0])
	}
	if !containsStringForOpsTest(got[0].RetrievalPath, "bm25") || !containsStringForOpsTest(got[0].RetrievalPath, "rrf") {
		t.Fatalf("retrieval path lost: %#v", got[0].RetrievalPath)
	}
}

func containsStringForOpsTest(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
