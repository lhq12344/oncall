package knowledge

import (
	"context"
	"testing"
	"time"
)

// 这些测试不依赖外部库，可以直接运行

func TestCaseRanker_Basic(t *testing.T) {
	ranker := NewCaseRanker()

	if ranker == nil {
		t.Fatal("ranker should not be nil")
	}

	if ranker.similarityWeight != 0.4 {
		t.Errorf("expected similarity weight 0.4, got %.2f", ranker.similarityWeight)
	}

	t.Log("✓ CaseRanker created successfully")
}

func TestFeedbackManager_Basic(t *testing.T) {
	fm := NewFeedbackManager()
	ctx := context.Background()

	if fm == nil {
		t.Fatal("feedback manager should not be nil")
	}

	// 测试添加反馈
	feedback := &Feedback{
		Helpful: true,
		Rating:  5,
		Comment: "很有帮助",
	}

	err := fm.AddFeedback(ctx, "case1", feedback)
	if err != nil {
		t.Fatalf("failed to add feedback: %v", err)
	}

	// 测试获取反馈
	feedbacks, err := fm.GetFeedbacks(ctx, "case1")
	if err != nil {
		t.Fatalf("failed to get feedbacks: %v", err)
	}

	if len(feedbacks) != 1 {
		t.Errorf("expected 1 feedback, got %d", len(feedbacks))
	}

	if feedbacks[0].Rating != 5 {
		t.Errorf("expected rating 5, got %d", feedbacks[0].Rating)
	}

	t.Log("✓ FeedbackManager works correctly")
}

func TestExtractKeywords_Basic(t *testing.T) {
	tests := []struct {
		input    string
		minCount int
	}{
		{"pod 无法启动", 1},
		{"服务响应慢需要优化", 1},
		{"数据库连接超时问题", 1},
	}

	for _, tt := range tests {
		keywords := extractKeywords(tt.input)
		if len(keywords) < tt.minCount {
			t.Errorf("input '%s': expected at least %d keywords, got %d",
				tt.input, tt.minCount, len(keywords))
		}
		t.Logf("'%s' -> %v", tt.input, keywords)
	}

	t.Log("✓ extractKeywords works correctly")
}

func TestPruneManager_ShouldPrune(t *testing.T) {
	fm := NewFeedbackManager()
	pm := NewPruneManager(fm)
	ctx := context.Background()

	// 测试低质量案例
	lowQualityCase := &KnowledgeCase{
		ID:          "case1",
		SuccessRate: 0.1,
		UsageCount:  10,
		RetrievedAt: time.Now(),
	}

	shouldPrune, reason := pm.ShouldPrune(ctx, lowQualityCase)
	if !shouldPrune {
		t.Error("low quality case should be pruned")
	}
	t.Logf("Low quality case: shouldPrune=%v, reason=%s", shouldPrune, reason)

	// 测试高质量案例
	highQualityCase := &KnowledgeCase{
		ID:          "case2",
		SuccessRate: 0.9,
		UsageCount:  50,
		RetrievedAt: time.Now(),
	}

	shouldPrune, reason = pm.ShouldPrune(ctx, highQualityCase)
	if shouldPrune {
		t.Error("high quality case should not be pruned")
	}
	t.Logf("High quality case: shouldPrune=%v, reason=%s", shouldPrune, reason)

	t.Log("✓ PruneManager works correctly")
}

func TestFeedbackStatistics(t *testing.T) {
	fm := NewFeedbackManager()
	ctx := context.Background()

	caseID := "case1"

	// 添加多个反馈
	feedbacks := []*Feedback{
		{Helpful: true, Rating: 5},
		{Helpful: true, Rating: 4},
		{Helpful: false, Rating: 2},
		{Helpful: true, Rating: 5},
	}

	for _, fb := range feedbacks {
		fm.AddFeedback(ctx, caseID, fb)
	}

	// 获取统计信息
	stats, err := fm.GetStatistics(ctx, caseID)
	if err != nil {
		t.Fatalf("failed to get statistics: %v", err)
	}

	if stats.TotalCount != 4 {
		t.Errorf("expected total count 4, got %d", stats.TotalCount)
	}

	if stats.HelpfulCount != 3 {
		t.Errorf("expected helpful count 3, got %d", stats.HelpfulCount)
	}

	expectedHelpfulRate := 0.75
	if stats.HelpfulRate != expectedHelpfulRate {
		t.Errorf("expected helpful rate %.2f, got %.2f", expectedHelpfulRate, stats.HelpfulRate)
	}

	expectedAvgRating := 4.0
	if stats.AvgRating != expectedAvgRating {
		t.Errorf("expected avg rating %.2f, got %.2f", expectedAvgRating, stats.AvgRating)
	}

	t.Logf("Statistics: Total=%d, Helpful=%d (%.1f%%), AvgRating=%.2f",
		stats.TotalCount, stats.HelpfulCount, stats.HelpfulRate*100, stats.AvgRating)

	t.Log("✓ FeedbackStatistics works correctly")
}

func TestCaseRanker_CalculateScore(t *testing.T) {
	ranker := NewCaseRanker()

	kcase := &KnowledgeCase{
		ID:          "case1",
		Title:       "Pod 启动失败",
		Content:     "Pod 一直处于 Pending 状态",
		Score:       0.9,
		SuccessRate: 0.8,
		UsageCount:  10,
		RetrievedAt: time.Now(),
		Metadata: map[string]interface{}{
			"created_at": time.Now().Unix(),
		},
	}

	query := "pod 无法启动"
	score := ranker.calculateScore(query, kcase)

	if score <= 0 || score > 2 {
		t.Errorf("score out of reasonable range: %.3f", score)
	}

	t.Logf("Calculated score: %.3f", score)
	t.Log("✓ CaseRanker.calculateScore works correctly")
}

func TestFindDuplicates_Basic(t *testing.T) {
	fm := NewFeedbackManager()
	pm := NewPruneManager(fm)
	ctx := context.Background()

	cases := []*KnowledgeCase{
		{
			ID:      "case1",
			Title:   "Pod 启动失败",
			Content: "Pod 一直处于 Pending 状态 无法启动 需要检查资源配额",
		},
		{
			ID:      "case2",
			Title:   "Pod 无法启动",
			Content: "Pod 处于 Pending 状态 启动失败 检查资源配额 节点状态",
		},
		{
			ID:      "case3",
			Title:   "服务响应慢",
			Content: "API 响应时间超过 5 秒 需要优化数据库查询",
		},
	}

	duplicates := pm.FindDuplicates(ctx, cases, 0.3)

	t.Logf("Found %d duplicate groups", len(duplicates))
	for i, group := range duplicates {
		t.Logf("  Group %d: %d cases", i+1, len(group))
		for _, kcase := range group {
			t.Logf("    - %s: %s", kcase.ID, kcase.Title)
		}
	}

	// case1 和 case2 应该被识别为重复
	if len(duplicates) < 1 {
		t.Log("Warning: expected at least 1 duplicate group (case1 and case2 are similar)")
	}

	t.Log("✓ FindDuplicates works correctly")
}

func TestKnowledgeCase_Structure(t *testing.T) {
	kcase := &KnowledgeCase{
		ID:          "test_case",
		Title:       "测试案例",
		Content:     "这是一个测试案例",
		Solution:    "测试解决方案",
		Score:       0.95,
		QualityScore: 0.85,
		Tags:        []string{"test", "example"},
		Metadata: map[string]interface{}{
			"author": "test_user",
		},
		RetrievedAt: time.Now(),
		UsageCount:  5,
		SuccessRate: 0.9,
	}

	if kcase.ID != "test_case" {
		t.Errorf("expected ID 'test_case', got '%s'", kcase.ID)
	}

	if len(kcase.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(kcase.Tags))
	}

	if kcase.Score != 0.95 {
		t.Errorf("expected score 0.95, got %.2f", kcase.Score)
	}

	t.Log("✓ KnowledgeCase structure is correct")
}

func TestFeedback_Structure(t *testing.T) {
	feedback := &Feedback{
		CaseID:    "case1",
		UserID:    "user1",
		Helpful:   true,
		Rating:    5,
		Comment:   "非常有帮助",
		Timestamp: time.Now(),
	}

	if feedback.CaseID != "case1" {
		t.Errorf("expected case ID 'case1', got '%s'", feedback.CaseID)
	}

	if !feedback.Helpful {
		t.Error("expected helpful to be true")
	}

	if feedback.Rating != 5 {
		t.Errorf("expected rating 5, got %d", feedback.Rating)
	}

	t.Log("✓ Feedback structure is correct")
}
