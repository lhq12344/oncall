package test

import (
	"context"
	"testing"
	"time"

	"go_agent/internal/agent/knowledge"
	"go_agent/internal/agent/knowledge/tools"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestVectorSearchTool 测试向量检索工具
func TestVectorSearchTool(t *testing.T) {
	logger := zap.NewNop()
	baseTool := tools.NewVectorSearchTool(nil, logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok, "tool should implement InvokableTool")

	ctx := context.Background()

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "vector_search", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("ValidInput", func(t *testing.T) {
		input := `{"query": "Pod CPU 使用率过高", "top_k": 5}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, result, "degraded")
	})

	t.Run("MissingQuery", func(t *testing.T) {
		input := `{"top_k": 5}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query is required")
	})

	t.Run("DefaultTopK", func(t *testing.T) {
		input := `{"query": "测试查询"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		input := `{invalid json}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
	})
}

// TestKnowledgeIndexTool 测试知识索引工具
func TestKnowledgeIndexTool(t *testing.T) {
	logger := zap.NewNop()
	baseTool := tools.NewKnowledgeIndexTool(nil, logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	ctx := context.Background()

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "knowledge_index", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("ValidInput", func(t *testing.T) {
		input := `{
			"content": "故障案例：Pod OOMKilled，解决方案：增加内存限制",
			"metadata": {"timestamp": "2026-03-07", "success_rate": 0.95}
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, result, "degraded")
	})

	t.Run("MissingContent", func(t *testing.T) {
		input := `{"metadata": {}}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "content is required")
	})

	t.Run("WithoutMetadata", func(t *testing.T) {
		input := `{"content": "测试内容"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})
}

// TestCaseRanker 测试案例排序器
func TestCaseRanker(t *testing.T) {
	ranker := knowledge.NewCaseRanker()

	t.Run("DefaultWeights", func(t *testing.T) {
		assert.Equal(t, 0.5, ranker.SimilarityWeight)
		assert.Equal(t, 0.3, ranker.RecencyWeight)
		assert.Equal(t, 0.2, ranker.SuccessWeight)
	})

	t.Run("RankEmptyDocs", func(t *testing.T) {
		results := ranker.Rank([]*schema.Document{})
		assert.Nil(t, results)
	})

	t.Run("RankSingleDoc", func(t *testing.T) {
		doc := &schema.Document{
			ID:      "doc1",
			Content: "测试文档",
		}
		doc.WithScore(0.9)

		results := ranker.Rank([]*schema.Document{doc})
		require.Len(t, results, 1)
		assert.Equal(t, 0.9, results[0].SimilarityScore)
	})

	t.Run("RankMultipleDocs", func(t *testing.T) {
		now := time.Now()

		doc1 := &schema.Document{
			ID:      "doc1",
			Content: "最近的高成功率案例",
			MetaData: map[string]interface{}{
				"timestamp":    now.Add(-24 * time.Hour).Unix(),
				"success_rate": 0.95,
			},
		}
		doc1.WithScore(0.85)

		doc2 := &schema.Document{
			ID:      "doc2",
			Content: "较旧的低成功率案例",
			MetaData: map[string]interface{}{
				"timestamp":    now.Add(-30 * 24 * time.Hour).Unix(),
				"success_rate": 0.6,
			},
		}
		doc2.WithScore(0.9)

		results := ranker.Rank([]*schema.Document{doc1, doc2})
		require.Len(t, results, 2)

		// doc1 应该排在前面（更新、成功率更高）
		assert.Equal(t, "doc1", results[0].Doc.ID)
		assert.Greater(t, results[0].CompositeScore, results[1].CompositeScore)
	})

	t.Run("RecencyScore", func(t *testing.T) {
		now := time.Now()

		recentDoc := &schema.Document{
			MetaData: map[string]interface{}{
				"timestamp": now.Add(-1 * 24 * time.Hour).Unix(),
			},
		}
		recentDoc.WithScore(0.8)

		oldDoc := &schema.Document{
			MetaData: map[string]interface{}{
				"timestamp": now.Add(-100 * 24 * time.Hour).Unix(),
			},
		}
		oldDoc.WithScore(0.8)

		results := ranker.Rank([]*schema.Document{recentDoc, oldDoc})
		require.Len(t, results, 2)

		assert.Greater(t, results[0].RecencyScore, results[1].RecencyScore)
	})

	t.Run("SuccessScore", func(t *testing.T) {
		highSuccessDoc := &schema.Document{
			MetaData: map[string]interface{}{
				"success_rate": 0.95,
			},
		}
		highSuccessDoc.WithScore(0.8)

		lowSuccessDoc := &schema.Document{
			MetaData: map[string]interface{}{
				"success_rate": 0.5,
			},
		}
		lowSuccessDoc.WithScore(0.8)

		results := ranker.Rank([]*schema.Document{highSuccessDoc, lowSuccessDoc})
		require.Len(t, results, 2)

		assert.Greater(t, results[0].SuccessScore, results[1].SuccessScore)
	})

	t.Run("CustomWeights", func(t *testing.T) {
		doc := &schema.Document{
			ID:      "doc1",
			Content: "测试",
			MetaData: map[string]interface{}{
				"timestamp":    time.Now().Unix(),
				"success_rate": 0.9,
			},
		}
		doc.WithScore(0.8)

		// 只关注相似度
		results := ranker.RankWithCustomWeights([]*schema.Document{doc}, 1.0, 0.0, 0.0)
		require.Len(t, results, 1)
		assert.Equal(t, 0.8, results[0].CompositeScore)

		// 只关注成功率
		results = ranker.RankWithCustomWeights([]*schema.Document{doc}, 0.0, 0.0, 1.0)
		require.Len(t, results, 1)
		assert.Equal(t, 0.9, results[0].CompositeScore)
	})

	t.Run("MissingMetadata", func(t *testing.T) {
		doc := &schema.Document{
			ID:      "doc1",
			Content: "无元数据的文档",
		}
		doc.WithScore(0.8)

		results := ranker.Rank([]*schema.Document{doc})
		require.Len(t, results, 1)

		// 应该使用默认分数
		assert.Equal(t, 0.5, results[0].RecencyScore)
		assert.Equal(t, 0.5, results[0].SuccessScore)
	})
}

// TestKnowledgeAgent 测试知识代理
func TestKnowledgeAgent(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	t.Run("NilConfig", func(t *testing.T) {
		_, err := knowledge.NewKnowledgeAgent(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config is required")
	})

	t.Run("NilChatModel", func(t *testing.T) {
		cfg := &knowledge.Config{
			Logger: logger,
		}
		_, err := knowledge.NewKnowledgeAgent(ctx, cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chat model is required")
	})

	t.Run("ValidConfig", func(t *testing.T) {
		chatModel, err := models.GetChatModel()
		if err != nil {
			t.Skip("Skipping test: ChatModel initialization failed")
		}

		cfg := &knowledge.Config{
			ChatModel: chatModel,
			Logger:    logger,
		}

		agent, err := knowledge.NewKnowledgeAgent(ctx, cfg)
		if err != nil {
			t.Logf("Agent creation failed (expected if Milvus unavailable): %v", err)
			return
		}

		assert.NotNil(t, agent)
	})
}
