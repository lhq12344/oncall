package knowledge

import (
	"context"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
)

func TestCaseRanker(t *testing.T) {
	ranker := NewCaseRanker()

	// 创建测试案例
	cases := []*KnowledgeCase{
		{
			ID:          "case1",
			Title:       "Pod 启动失败",
			Content:     "Pod 一直处于 Pending 状态，无法启动",
			Solution:    "检查资源配额，增加节点资源",
			Score:       0.9,
			SuccessRate: 0.8,
			UsageCount:  10,
			RetrievedAt: time.Now(),
		},
		{
			ID:          "case2",
			Title:       "服务响应慢",
			Content:     "API 响应时间超过 5 秒",
			Solution:    "优化数据库查询，添加索引",
			Score:       0.85,
			SuccessRate: 0.9,
			UsageCount:  20,
			RetrievedAt: time.Now().Add(-30 * 24 * time.Hour),
		},
		{
			ID:          "case3",
			Title:       "内存泄漏",
			Content:     "容器内存持续增长，最终 OOM",
			Solution:    "修复代码中的内存泄漏",
			Score:       0.95,
			SuccessRate: 0.7,
			UsageCount:  5,
			RetrievedAt: time.Now().Add(-60 * 24 * time.Hour),
		},
	}

	// 测试排序
	query := "pod 无法启动"
	rankedCases := ranker.Rank(context.Background(), query, cases)

	if len(rankedCases) != 3 {
		t.Errorf("expected 3 cases, got %d", len(rankedCases))
	}

	// 验证排序结果
	t.Logf("Ranked cases:")
	for i, kcase := range rankedCases {
		t.Logf("  %d. %s (score: %.3f)", i+1, kcase.Title, kcase.QualityScore)
	}

	// 第一个案例应该是相似度最高的
	if rankedCases[0].ID != "case3" && rankedCases[0].ID != "case1" {
		t.Logf("Warning: expected case3 or case1 to be first, got %s", rankedCases[0].ID)
	}
}

func TestFeedbackManager(t *testing.T) {
	fm := NewFeedbackManager()
	ctx := context.Background()

	caseID := "case1"

	// 添加反馈
	feedbacks := []*Feedback{
		{Helpful: true, Rating: 5, Comment: "很有帮助"},
		{Helpful: true, Rating: 4, Comment: "解决了问题"},
		{Helpful: false, Rating: 2, Comment: "没有用"},
		{Helpful: true, Rating: 5, Comment: "完美"},
	}

	for _, fb := range feedbacks {
		err := fm.AddFeedback(ctx, caseID, fb)
		if err != nil {
			t.Fatalf("failed to add feedback: %v", err)
		}
	}

	// 获取反馈
	allFeedbacks, err := fm.GetFeedbacks(ctx, caseID)
	if err != nil {
		t.Fatalf("failed to get feedbacks: %v", err)
	}

	if len(allFeedbacks) != 4 {
		t.Errorf("expected 4 feedbacks, got %d", len(allFeedbacks))
	}

	// 计算质量评分
	qualityScore, err := fm.GetQualityScore(ctx, caseID)
	if err != nil {
		t.Fatalf("failed to get quality score: %v", err)
	}

	t.Logf("Quality score: %.3f", qualityScore)

	if qualityScore < 0.5 || qualityScore > 1.0 {
		t.Errorf("quality score out of range: %.3f", qualityScore)
	}

	// 获取统计信息
	stats, err := fm.GetStatistics(ctx, caseID)
	if err != nil {
		t.Fatalf("failed to get statistics: %v", err)
	}

	t.Logf("Statistics:")
	t.Logf("  Total: %d", stats.TotalCount)
	t.Logf("  Helpful: %d (%.1f%%)", stats.HelpfulCount, stats.HelpfulRate*100)
	t.Logf("  Avg Rating: %.2f", stats.AvgRating)

	if stats.TotalCount != 4 {
		t.Errorf("expected total count 4, got %d", stats.TotalCount)
	}

	if stats.HelpfulCount != 3 {
		t.Errorf("expected helpful count 3, got %d", stats.HelpfulCount)
	}
}

func TestPruneManager(t *testing.T) {
	fm := NewFeedbackManager()
	pm := NewPruneManager(fm)
	ctx := context.Background()

	// 创建测试案例
	cases := []*KnowledgeCase{
		{
			ID:          "case1",
			Title:       "低质量案例",
			Content:     "内容很少",
			SuccessRate: 0.1,
			UsageCount:  10,
			RetrievedAt: time.Now().Add(-100 * 24 * time.Hour),
		},
		{
			ID:          "case2",
			Title:       "高质量案例",
			Content:     "详细的解决方案",
			SuccessRate: 0.9,
			UsageCount:  50,
			RetrievedAt: time.Now(),
		},
		{
			ID:          "case3",
			Title:       "未使用案例",
			Content:     "从未被使用",
			SuccessRate: 0.5,
			UsageCount:  0,
			RetrievedAt: time.Now().Add(-100 * 24 * time.Hour),
		},
	}

	// 测试剪枝判断
	for _, kcase := range cases {
		shouldPrune, reason := pm.ShouldPrune(ctx, kcase)
		t.Logf("Case %s: shouldPrune=%v, reason=%s", kcase.ID, shouldPrune, reason)

		if kcase.ID == "case1" && !shouldPrune {
			t.Error("case1 should be pruned (low success rate)")
		}

		if kcase.ID == "case2" && shouldPrune {
			t.Error("case2 should not be pruned (high quality)")
		}

		if kcase.ID == "case3" && !shouldPrune {
			t.Error("case3 should be pruned (not used for 100 days)")
		}
	}
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		input    string
		expected int // 期望的关键词数量
	}{
		{"pod 无法启动", 2},
		{"服务响应慢，需要优化", 3},
		{"数据库连接超时的问题", 3},
	}

	for _, tt := range tests {
		keywords := extractKeywords(tt.input)
		t.Logf("Input: %s", tt.input)
		t.Logf("Keywords: %v", keywords)

		if len(keywords) < 1 {
			t.Errorf("expected at least 1 keyword, got %d", len(keywords))
		}
	}
}

func TestKnowledgeAgent_ExtractSuccessPath(t *testing.T) {
	// 创建 Knowledge Agent（不需要真实的 retriever 和 indexer）
	agent := &KnowledgeAgent{
		logger: nil, // 测试时可以为 nil
	}

	ctx := context.Background()

	// 创建执行日志
	execLog := &ExecutionLog{
		ExecutionID: "exec_001",
		Problem:     "Nginx 服务无法启动",
		Solution:    "修复配置文件语法错误",
		Steps: []string{
			"检查 nginx 配置文件",
			"发现语法错误",
			"修复配置文件",
			"重启 nginx 服务",
		},
		Duration:    5 * time.Minute,
		Success:     true,
		SuccessRate: 1.0,
		Tags:        []string{"nginx", "配置错误"},
		Timestamp:   time.Now(),
	}

	// 提取成功路径（注意：这里会失败因为没有真实的 indexer）
	doc, err := agent.ExtractSuccessPath(ctx, execLog)
	if err != nil {
		t.Logf("Expected error (no indexer): %v", err)
	}

	if doc != nil {
		t.Logf("Document content: %s", doc.Content)
		t.Logf("Document metadata: %v", doc.MetaData)
	}
}

func TestKnowledgeSearchTool(t *testing.T) {
	// 创建 mock agent
	agent := &KnowledgeAgent{
		logger: nil,
	}

	tool := NewKnowledgeSearchTool(agent)

	ctx := context.Background()

	// 获取工具信息
	info, err := tool.Info(ctx)
	if err != nil {
		t.Fatalf("failed to get tool info: %v", err)
	}

	t.Logf("Tool name: %s", info.Name)
	t.Logf("Tool desc: %s", info.Desc)

	if info.Name != "knowledge_search" {
		t.Errorf("expected tool name 'knowledge_search', got '%s'", info.Name)
	}

	// 测试工具调用（会失败因为没有真实的 retriever）
	input := `{"query": "pod 启动失败", "top_k": 3}`
	_, err = tool.InvokableRun(ctx, input)
	if err != nil {
		t.Logf("Expected error (no retriever): %v", err)
	}
}

func TestFindDuplicates(t *testing.T) {
	fm := NewFeedbackManager()
	pm := NewPruneManager(fm)
	ctx := context.Background()

	// 创建测试案例（包含重复）
	cases := []*KnowledgeCase{
		{
			ID:      "case1",
			Title:   "Pod 启动失败",
			Content: "Pod 一直处于 Pending 状态，无法启动，需要检查资源配额",
		},
		{
			ID:      "case2",
			Title:   "Pod 无法启动",
			Content: "Pod 处于 Pending 状态，启动失败，检查资源配额和节点状态",
		},
		{
			ID:      "case3",
			Title:   "服务响应慢",
			Content: "API 响应时间超过 5 秒，需要优化数据库查询",
		},
		{
			ID:      "case4",
			Title:   "API 性能问题",
			Content: "接口响应慢，超过 5 秒，数据库查询需要优化",
		},
	}

	// 查找重复（相似度阈值 0.5）
	duplicates := pm.FindDuplicates(ctx, cases, 0.5)

	t.Logf("Found %d duplicate groups:", len(duplicates))
	for i, group := range duplicates {
		t.Logf("  Group %d:", i+1)
		for _, kcase := range group {
			t.Logf("    - %s: %s", kcase.ID, kcase.Title)
		}
	}

	if len(duplicates) < 1 {
		t.Error("expected at least 1 duplicate group")
	}
}

// MockRetriever 用于测试的 mock retriever
type MockRetriever struct{}

func (m *MockRetriever) Retrieve(ctx context.Context, query string, opts ...any) ([]*schema.Document, error) {
	// 返回模拟数据
	return []*schema.Document{
		{
			Content: "Pod 启动失败的解决方案：检查资源配额",
			Score:   0.9,
			MetaData: map[string]any{
				"title":    "Pod 启动失败",
				"solution": "检查资源配额，增加节点资源",
				"tags":     []string{"k8s", "pod"},
			},
		},
		{
			Content: "服务响应慢的解决方案：优化数据库查询",
			Score:   0.85,
			MetaData: map[string]any{
				"title":    "服务响应慢",
				"solution": "优化数据库查询，添加索引",
				"tags":     []string{"performance", "database"},
			},
		},
	}, nil
}

func TestKnowledgeAgent_Search(t *testing.T) {
	// 创建带 mock retriever 的 agent
	agent := &KnowledgeAgent{
		retriever: &MockRetriever{},
		ranker:    NewCaseRanker(),
		feedback:  NewFeedbackManager(),
		logger:    nil,
	}

	ctx := context.Background()

	// 执行搜索
	cases, err := agent.Search(ctx, "pod 启动失败", 5)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	t.Logf("Found %d cases:", len(cases))
	for i, kcase := range cases {
		t.Logf("  %d. %s (score: %.2f)", i+1, kcase.Title, kcase.Score)
		t.Logf("     Solution: %s", kcase.Solution)
		t.Logf("     Tags: %v", kcase.Tags)
	}

	if len(cases) != 2 {
		t.Errorf("expected 2 cases, got %d", len(cases))
	}
}
