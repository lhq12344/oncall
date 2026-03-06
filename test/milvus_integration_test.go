package test

import (
	"context"
	"testing"

	"go_agent/internal/ai/indexer"
	"go_agent/internal/ai/retriever"

	"github.com/cloudwego/eino/schema"
)

// TestMilvusIntegration 测试 Milvus 集成
func TestMilvusIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping milvus integration test")
	}

	ctx := context.Background()

	// 1. 测试 Indexer
	t.Run("Indexer", func(t *testing.T) {
		idx, err := indexer.NewMilvusIndexer(ctx)
		if err != nil {
			t.Fatalf("failed to create indexer: %v", err)
		}

		// 索引测试文档
		docs := []*schema.Document{
			{
				ID:      "test_doc_1",
				Content: "Pod 启动失败，错误信息：ImagePullBackOff。解决方案：检查镜像名称和仓库权限。",
				MetaData: map[string]interface{}{
					"category":    "k8s",
					"success_rate": 0.95,
					"timestamp":   "2026-03-06",
				},
			},
			{
				ID:      "test_doc_2",
				Content: "服务响应慢，CPU 使用率高。解决方案：优化查询语句，添加索引。",
				MetaData: map[string]interface{}{
					"category":    "performance",
					"success_rate": 0.88,
					"timestamp":   "2026-03-05",
				},
			},
		}

		ids, err := idx.Store(ctx, docs)
		if err != nil {
			t.Fatalf("failed to index documents: %v", err)
		}

		t.Logf("Indexed %d documents: %v", len(ids), ids)
	})

	// 2. 测试 Retriever
	t.Run("Retriever", func(t *testing.T) {
		rtr, err := retriever.NewMilvusRetriever(ctx)
		if err != nil {
			t.Fatalf("failed to create retriever: %v", err)
		}

		// 检索测试
		query := "Pod 启动失败怎么办"
		docs, err := rtr.Retrieve(ctx, query)
		if err != nil {
			t.Fatalf("failed to retrieve: %v", err)
		}

		t.Logf("Query: %s", query)
		t.Logf("Found %d documents:", len(docs))
		for i, doc := range docs {
			t.Logf("  %d. [Score: %.4f] %s", i+1, doc.Score(), doc.Content)
		}

		if len(docs) == 0 {
			t.Log("No documents found (this is expected if collection is empty)")
		}
	})
}
