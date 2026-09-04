package dialogue

import (
	"testing"

	"go_agent/internal/rag"
	"go_agent/utility/common"
)

func TestDialogueRetrieverCollectionsHonorHybridEnabled(t *testing.T) {
	milvusConfig := common.MilvusConfig{
		Collection:            "biz_v1",
		KnowledgeV2Collection: "biz_next",
		OpsV2Collection:       "ops_next",
	}

	knowledgeCollection, opsCollection, useHybrid := dialogueRetrieverCollections(rag.Config{HybridEnabled: true}, milvusConfig)
	if !useHybrid {
		t.Fatal("expected hybrid retrieval when HybridEnabled=true")
	}
	if knowledgeCollection != "biz_next" || opsCollection != "ops_next" {
		t.Fatalf("unexpected hybrid collections: knowledge=%q ops=%q", knowledgeCollection, opsCollection)
	}

	knowledgeCollection, opsCollection, useHybrid = dialogueRetrieverCollections(rag.Config{HybridEnabled: false}, milvusConfig)
	if useHybrid {
		t.Fatal("expected direct v2 vector retrieval when HybridEnabled=false")
	}
	if knowledgeCollection != "biz_next" || opsCollection != "ops_next" {
		t.Fatalf("hybrid disablement must not fall back to old collections: knowledge=%q ops=%q", knowledgeCollection, opsCollection)
	}
}
