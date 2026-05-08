package dialogue

import (
	"context"
	"encoding/json"
	"testing"

	"go_agent/internal/logic/ai/models"

	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// mockRetriever 模拟 Milvus 检索器，返回预设文档。
type mockRetriever struct {
	docs map[string][]*schema.Document
}

func (m *mockRetriever) Retrieve(ctx context.Context, query string, opts ...einoretriever.Option) ([]*schema.Document, error) {
	if docs, ok := m.docs[query]; ok {
		return docs, nil
	}
	return []*schema.Document{{Content: "通用文档：" + query}}, nil
}

// TestDecomposeNode_Fallback 验证 LLM 为 nil 时降级返回原始问题。
func TestDecomposeNode_Fallback(t *testing.T) {
	ctx := context.Background()
	fn := buildDecomposeNode(ctx, nil, nil)

	result, err := fn(ctx, "玩法和充值的问题")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0] != "玩法和充值的问题" {
		t.Errorf("expected fallback to original question, got %v", result)
	}
}

// TestParallelRAGNode_NilRetriever 验证 retriever 为 nil 时返回空文档 map。
func TestParallelRAGNode_NilRetriever(t *testing.T) {
	ctx := context.Background()
	fn := buildParallelRAGNode(nil, nil)

	subQs := []string{"问题A", "问题B"}
	result, err := fn(ctx, subQs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 entries, got %d", len(result))
	}
	for _, q := range subQs {
		if docs, ok := result[q]; !ok || docs != nil {
			t.Errorf("expected nil docs for %q, got %v", q, docs)
		}
	}
}

// TestParallelRAGNode_WithRetriever 验证并行检索返回文档。
func TestParallelRAGNode_WithRetriever(t *testing.T) {
	ctx := context.Background()
	rtr := &mockRetriever{
		docs: map[string][]*schema.Document{
			"体力恢复": {{Content: "体力每10分钟恢复1点"}},
			"充值优惠": {{Content: "充值满100送20"}},
		},
	}
	fn := buildParallelRAGNode(rtr, nil)

	result, err := fn(ctx, []string{"体力恢复", "充值优惠"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result["体力恢复"]) == 0 {
		t.Error("expected docs for 体力恢复")
	}
	if len(result["充值优惠"]) == 0 {
		t.Error("expected docs for 充值优惠")
	}
}

// TestEvaluateNode_NilLLM_WithDocs 验证 LLM 为 nil 时按文档存在性分类。
func TestEvaluateNode_NilLLM_WithDocs(t *testing.T) {
	ctx := context.Background()
	fn := buildEvaluateNode(ctx, nil, nil)

	ragResults := map[string][]schema.Document{
		"有文档的问题": {{Content: "相关内容"}},
		"无文档的问题": {},
	}
	result, err := fn(ctx, ragResults)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.SolvedContexts) != 1 {
		t.Errorf("expected 1 solved, got %d", len(result.SolvedContexts))
	}
	if len(result.PendingQuestions) != 1 {
		t.Errorf("expected 1 pending, got %d", len(result.PendingQuestions))
	}
	if result.PendingQuestions[0] != "无文档的问题" {
		t.Errorf("expected pending '无文档的问题', got %q", result.PendingQuestions[0])
	}
}

// TestEvaluateNode_EmptyInput 验证空输入返回空结果。
func TestEvaluateNode_EmptyInput(t *testing.T) {
	ctx := context.Background()
	fn := buildEvaluateNode(ctx, nil, nil)

	result, err := fn(ctx, map[string][]schema.Document{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.SolvedContexts) != 0 || len(result.PendingQuestions) != 0 {
		t.Errorf("expected empty result, got solved=%v pending=%v", result.SolvedContexts, result.PendingQuestions)
	}
}

// TestKnowledgeSearchExpertTool_Info 验证 Tool Info 返回正确名称。
func TestKnowledgeSearchExpertTool_Info(t *testing.T) {
	ctx := context.Background()
	rtr := &mockRetriever{docs: map[string][]*schema.Document{}}
	toolInst, err := NewKnowledgeSearchExpertTool(ctx, nil, rtr, nil)
	if err != nil {
		t.Fatalf("failed to create tool: %v", err)
	}
	info, err := toolInst.Info(ctx)
	if err != nil {
		t.Fatalf("Info() error: %v", err)
	}
	if info.Name != "knowledge_search_expert" {
		t.Errorf("expected name 'knowledge_search_expert', got %q", info.Name)
	}
}

// TestKnowledgeSearchExpertTool_InvokableRun 验证 Tool 返回合法 JSON。
func TestKnowledgeSearchExpertTool_InvokableRun(t *testing.T) {
	ctx := context.Background()
	rtr := &mockRetriever{
		docs: map[string][]*schema.Document{
			"体力恢复机制": {{Content: "体力每10分钟恢复1点，上限100"}},
		},
	}
	toolInst, err := NewKnowledgeSearchExpertTool(ctx, nil, rtr, nil)
	if err != nil {
		t.Fatalf("failed to create tool: %v", err)
	}

	inv, ok := toolInst.(tool.InvokableTool)
	if !ok {
		t.Fatal("tool does not implement tool.InvokableTool")
	}

	args, _ := json.Marshal(map[string]string{"question": "体力恢复机制是什么？"})
	out, err := inv.InvokableRun(ctx, string(args))
	if err != nil {
		t.Fatalf("InvokableRun error: %v", err)
	}

	var result KnowledgeSpecialistResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
}

// TestKnowledgeSearchExpertTool_Degraded 验证 retriever 为 nil 时降级返回 pending。
func TestKnowledgeSearchExpertTool_Degraded(t *testing.T) {
	ctx := context.Background()
	toolInst, err := NewKnowledgeSearchExpertTool(ctx, (*models.ChatModel)(nil), nil, nil)
	if err != nil {
		t.Fatalf("failed to create tool: %v", err)
	}

	inv, ok := toolInst.(tool.InvokableTool)
	if !ok {
		t.Fatal("tool does not implement tool.InvokableTool")
	}

	args, _ := json.Marshal(map[string]string{"question": "复杂混合问题"})
	out, err := inv.InvokableRun(ctx, string(args))
	if err != nil {
		t.Fatalf("InvokableRun error: %v", err)
	}

	var result KnowledgeSpecialistResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(result.PendingQuestions) == 0 {
		t.Error("expected at least one pending question when retriever is nil")
	}
}
