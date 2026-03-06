package knowledge

import (
	"context"
	"math"
	"sort"
	"strings"
)

// CaseRanker 案例排序器
type CaseRanker struct {
	// 权重配置
	similarityWeight float64 // 相似度权重
	qualityWeight    float64 // 质量权重
	freshnessWeight  float64 // 时效性权重
	usageWeight      float64 // 使用频率权重
}

// NewCaseRanker 创建案例排序器
func NewCaseRanker() *CaseRanker {
	return &CaseRanker{
		similarityWeight: 0.4,
		qualityWeight:    0.3,
		freshnessWeight:  0.2,
		usageWeight:      0.1,
	}
}

// Rank 对案例进行排序
func (r *CaseRanker) Rank(ctx context.Context, query string, cases []*KnowledgeCase) []*KnowledgeCase {
	if len(cases) == 0 {
		return cases
	}

	// 计算每个案例的综合评分
	for _, kcase := range cases {
		kcase.QualityScore = r.calculateScore(query, kcase)
	}

	// 按综合评分排序
	sort.Slice(cases, func(i, j int) bool {
		return cases[i].QualityScore > cases[j].QualityScore
	})

	return cases
}

// calculateScore 计算综合评分
func (r *CaseRanker) calculateScore(query string, kcase *KnowledgeCase) float64 {
	// 1. 相似度评分（来自向量检索）
	similarityScore := kcase.Score

	// 2. 质量评分（基于用户反馈）
	qualityScore := kcase.SuccessRate
	if qualityScore == 0 {
		qualityScore = 0.5 // 默认值
	}

	// 3. 时效性评分（越新越好）
	freshnessScore := r.calculateFreshnessScore(kcase)

	// 4. 使用频率评分（越常用越好）
	usageScore := r.calculateUsageScore(kcase)

	// 5. 关键词匹配加成
	keywordBonus := r.calculateKeywordBonus(query, kcase)

	// 综合评分
	score := r.similarityWeight*similarityScore +
		r.qualityWeight*qualityScore +
		r.freshnessWeight*freshnessScore +
		r.usageWeight*usageScore +
		keywordBonus

	return score
}

// calculateFreshnessScore 计算时效性评分
func (r *CaseRanker) calculateFreshnessScore(kcase *KnowledgeCase) float64 {
	// 使用指数衰减函数
	// score = e^(-λ * days)
	// λ = 0.01 表示约 100 天后衰减到 37%

	if kcase.RetrievedAt.IsZero() {
		return 0.5
	}

	// 假设案例创建时间存储在 metadata 中
	createdAt, ok := kcase.Metadata["created_at"].(int64)
	if !ok {
		return 0.5
	}

	daysSinceCreation := float64(kcase.RetrievedAt.Unix()-createdAt) / 86400.0
	lambda := 0.01
	score := math.Exp(-lambda * daysSinceCreation)

	return score
}

// calculateUsageScore 计算使用频率评分
func (r *CaseRanker) calculateUsageScore(kcase *KnowledgeCase) float64 {
	// 使用对数函数归一化
	// score = log(1 + usage_count) / log(1 + max_usage)

	if kcase.UsageCount == 0 {
		return 0.0
	}

	maxUsage := 100.0 // 假设最大使用次数
	score := math.Log(1+float64(kcase.UsageCount)) / math.Log(1+maxUsage)

	return score
}

// calculateKeywordBonus 计算关键词匹配加成
func (r *CaseRanker) calculateKeywordBonus(query string, kcase *KnowledgeCase) float64 {
	// 提取查询关键词
	queryKeywords := extractKeywords(query)

	// 检查标题和内容中的关键词匹配
	matchCount := 0
	content := strings.ToLower(kcase.Title + " " + kcase.Content)

	for _, keyword := range queryKeywords {
		if strings.Contains(content, keyword) {
			matchCount++
		}
	}

	// 匹配度加成（最多 0.1）
	bonus := float64(matchCount) / float64(len(queryKeywords)) * 0.1

	return bonus
}

// extractKeywords 提取关键词（简单实现）
func extractKeywords(text string) []string {
	// 转小写
	text = strings.ToLower(text)

	// 分词（简单按空格分割）
	words := strings.Fields(text)

	// 过滤停用词
	stopWords := map[string]bool{
		"的": true, "了": true, "是": true, "在": true,
		"有": true, "和": true, "就": true, "不": true,
		"人": true, "都": true, "一": true, "我": true,
	}

	keywords := make([]string, 0)
	for _, word := range words {
		if !stopWords[word] && len(word) > 1 {
			keywords = append(keywords, word)
		}
	}

	return keywords
}

// RankWithBoost 带权重提升的排序
func (r *CaseRanker) RankWithBoost(ctx context.Context, query string, cases []*KnowledgeCase, boostTags []string) []*KnowledgeCase {
	// 先进行常规排序
	rankedCases := r.Rank(ctx, query, cases)

	// 对包含特定标签的案例进行权重提升
	for _, kcase := range rankedCases {
		for _, tag := range kcase.Tags {
			for _, boostTag := range boostTags {
				if tag == boostTag {
					kcase.QualityScore *= 1.2 // 提升 20%
					break
				}
			}
		}
	}

	// 重新排序
	sort.Slice(rankedCases, func(i, j int) bool {
		return rankedCases[i].QualityScore > rankedCases[j].QualityScore
	})

	return rankedCases
}
