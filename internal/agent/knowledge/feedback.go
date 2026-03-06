package knowledge

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// FeedbackManager 反馈管理器
type FeedbackManager struct {
	feedbacks map[string][]*Feedback // caseID -> feedbacks
	mu        sync.RWMutex
}

// NewFeedbackManager 创建反馈管理器
func NewFeedbackManager() *FeedbackManager {
	return &FeedbackManager{
		feedbacks: make(map[string][]*Feedback),
	}
}

// AddFeedback 添加反馈
func (f *FeedbackManager) AddFeedback(ctx context.Context, caseID string, feedback *Feedback) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	feedback.CaseID = caseID
	feedback.Timestamp = time.Now()

	if _, ok := f.feedbacks[caseID]; !ok {
		f.feedbacks[caseID] = make([]*Feedback, 0)
	}

	f.feedbacks[caseID] = append(f.feedbacks[caseID], feedback)

	return nil
}

// GetFeedbacks 获取案例的所有反馈
func (f *FeedbackManager) GetFeedbacks(ctx context.Context, caseID string) ([]*Feedback, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	feedbacks, ok := f.feedbacks[caseID]
	if !ok {
		return []*Feedback{}, nil
	}

	return feedbacks, nil
}

// GetQualityScore 计算案例的质量评分
func (f *FeedbackManager) GetQualityScore(ctx context.Context, caseID string) (float64, error) {
	feedbacks, err := f.GetFeedbacks(ctx, caseID)
	if err != nil {
		return 0, err
	}

	if len(feedbacks) == 0 {
		return 0.5, nil // 默认评分
	}

	// 计算平均评分
	totalScore := 0.0
	helpfulCount := 0

	for _, fb := range feedbacks {
		if fb.Helpful {
			helpfulCount++
		}
		if fb.Rating > 0 {
			totalScore += float64(fb.Rating)
		}
	}

	// 有用率
	helpfulRate := float64(helpfulCount) / float64(len(feedbacks))

	// 平均评分（归一化到 0-1）
	avgRating := 0.5
	if totalScore > 0 {
		avgRating = totalScore / float64(len(feedbacks)) / 5.0
	}

	// 综合评分（有用率 60% + 平均评分 40%）
	qualityScore := helpfulRate*0.6 + avgRating*0.4

	return qualityScore, nil
}

// GetStatistics 获取反馈统计
func (f *FeedbackManager) GetStatistics(ctx context.Context, caseID string) (*FeedbackStatistics, error) {
	feedbacks, err := f.GetFeedbacks(ctx, caseID)
	if err != nil {
		return nil, err
	}

	stats := &FeedbackStatistics{
		CaseID:       caseID,
		TotalCount:   len(feedbacks),
		HelpfulCount: 0,
		RatingSum:    0,
		RatingCount:  0,
	}

	for _, fb := range feedbacks {
		if fb.Helpful {
			stats.HelpfulCount++
		}
		if fb.Rating > 0 {
			stats.RatingSum += fb.Rating
			stats.RatingCount++
		}
	}

	if stats.TotalCount > 0 {
		stats.HelpfulRate = float64(stats.HelpfulCount) / float64(stats.TotalCount)
	}

	if stats.RatingCount > 0 {
		stats.AvgRating = float64(stats.RatingSum) / float64(stats.RatingCount)
	}

	return stats, nil
}

// FeedbackStatistics 反馈统计
type FeedbackStatistics struct {
	CaseID       string  `json:"case_id"`       // 案例 ID
	TotalCount   int     `json:"total_count"`   // 总反馈数
	HelpfulCount int     `json:"helpful_count"` // 有用数
	HelpfulRate  float64 `json:"helpful_rate"`  // 有用率
	RatingSum    int     `json:"rating_sum"`    // 评分总和
	RatingCount  int     `json:"rating_count"`  // 评分数量
	AvgRating    float64 `json:"avg_rating"`    // 平均评分
}

// PruneManager 知识剪枝管理器
type PruneManager struct {
	feedbackMgr *FeedbackManager
	mu          sync.RWMutex
}

// NewPruneManager 创建剪枝管理器
func NewPruneManager(feedbackMgr *FeedbackManager) *PruneManager {
	return &PruneManager{
		feedbackMgr: feedbackMgr,
	}
}

// ShouldPrune 判断是否应该剪枝
func (p *PruneManager) ShouldPrune(ctx context.Context, kcase *KnowledgeCase) (bool, string) {
	// 1. 质量评分过低
	qualityScore, err := p.feedbackMgr.GetQualityScore(ctx, kcase.ID)
	if err == nil && qualityScore < 0.3 {
		return true, fmt.Sprintf("quality score too low: %.2f", qualityScore)
	}

	// 2. 长时间未使用
	if kcase.UsageCount == 0 {
		daysSinceCreation := time.Since(kcase.RetrievedAt).Hours() / 24
		if daysSinceCreation > 90 {
			return true, "not used for 90 days"
		}
	}

	// 3. 成功率过低
	if kcase.SuccessRate < 0.2 && kcase.UsageCount > 5 {
		return true, fmt.Sprintf("success rate too low: %.2f", kcase.SuccessRate)
	}

	return false, ""
}

// FindDuplicates 查找重复案例
func (p *PruneManager) FindDuplicates(ctx context.Context, cases []*KnowledgeCase, threshold float64) [][]*KnowledgeCase {
	duplicates := make([][]*KnowledgeCase, 0)

	visited := make(map[string]bool)

	for i, case1 := range cases {
		if visited[case1.ID] {
			continue
		}

		group := []*KnowledgeCase{case1}
		visited[case1.ID] = true

		for j := i + 1; j < len(cases); j++ {
			case2 := cases[j]
			if visited[case2.ID] {
				continue
			}

			// 计算相似度（简单实现：基于内容长度和关键词）
			similarity := p.calculateSimilarity(case1, case2)
			if similarity > threshold {
				group = append(group, case2)
				visited[case2.ID] = true
			}
		}

		if len(group) > 1 {
			duplicates = append(duplicates, group)
		}
	}

	return duplicates
}

// calculateSimilarity 计算两个案例的相似度
func (p *PruneManager) calculateSimilarity(case1, case2 *KnowledgeCase) float64 {
	// 简单实现：基于标题和内容的 Jaccard 相似度

	keywords1 := extractKeywords(case1.Title + " " + case1.Content)
	keywords2 := extractKeywords(case2.Title + " " + case2.Content)

	// 计算交集和并集
	intersection := 0
	keywordSet := make(map[string]bool)

	for _, kw := range keywords1 {
		keywordSet[kw] = true
	}

	for _, kw := range keywords2 {
		if keywordSet[kw] {
			intersection++
		} else {
			keywordSet[kw] = true
		}
	}

	union := len(keywordSet)

	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}
