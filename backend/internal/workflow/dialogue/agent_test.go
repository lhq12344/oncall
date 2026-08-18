package dialogue

import (
	"testing"

	"go_agent/internal/rag"
	"go_agent/utility/common"
)

func TestDialogueRetrieverCollectionsHonorHybridEnabled(t *testing.T) {
	milvusConfig := common.MilvusConfig{
		Collection:            "biz_legacy",
		KnowledgeV2Collection: "biz_next",
		OpsV2Collection:       "ops_next",
	}

	knowledgePrimary, knowledgeLegacy, opsPrimary, opsLegacy, useHybrid := dialogueRetrieverCollections(rag.Config{HybridEnabled: true}, milvusConfig)
	if !useHybrid {
		t.Fatal("expected hybrid retrieval when HybridEnabled=true")
	}
	if knowledgePrimary != "biz_next" || knowledgeLegacy != "biz_legacy" || opsPrimary != "ops_next" || opsLegacy != common.MilvusOpsCollection {
		t.Fatalf("unexpected hybrid collections: knowledge=%q/%q ops=%q/%q", knowledgePrimary, knowledgeLegacy, opsPrimary, opsLegacy)
	}

	knowledgePrimary, knowledgeLegacy, opsPrimary, opsLegacy, useHybrid = dialogueRetrieverCollections(rag.Config{HybridEnabled: false}, milvusConfig)
	if useHybrid {
		t.Fatal("expected legacy retrieval when HybridEnabled=false")
	}
	if knowledgePrimary != "biz_legacy" || knowledgeLegacy != "" || opsPrimary != common.MilvusOpsCollection || opsLegacy != "" {
		t.Fatalf("unexpected legacy collections: knowledge=%q/%q ops=%q/%q", knowledgePrimary, knowledgeLegacy, opsPrimary, opsLegacy)
	}
}
