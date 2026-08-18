package knowledge

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestAssignChunkDocumentIDsAddsV2Metadata(t *testing.T) {
	docs := []*schema.Document{
		{ID: "doc-1", Content: "same content", MetaData: map[string]any{"title": "Runbook"}},
		{ID: "doc-1", Content: "same content", MetaData: map[string]any{"title": "Runbook"}},
	}
	assignChunkDocumentIDs(docs)
	if docs[0].ID == "" || docs[0].ID == docs[1].ID {
		t.Fatalf("chunk ids not unique: %q %q", docs[0].ID, docs[1].ID)
	}
	for _, doc := range docs {
		if doc.MetaData["source_type"] != "knowledge" {
			t.Fatalf("source_type missing: %#v", doc.MetaData)
		}
		if doc.MetaData["doc_id"] == "" || doc.MetaData["chunk_id"] == "" || doc.MetaData["content_hash"] == "" {
			t.Fatalf("v2 metadata missing: %#v", doc.MetaData)
		}
		if doc.ID != doc.MetaData["chunk_id"] {
			t.Fatalf("doc id should equal chunk_id: id=%q meta=%#v", doc.ID, doc.MetaData)
		}
	}
}
