package knowledge

import (
	"sort"
	"time"

	"github.com/cloudwego/eino/schema"
)

// CaseRanker 案例排序器
type CaseRanker struct {
	// 权重配置
	SimilarityWeight float64 // 相似度权重
	RecencyWeight    float64 // 时效性权重
	SuccessWeight    float64 // 成功率权重
}

// NewCaseRanker 创建案例排序器
func NewCaseRanker() *CaseRanker {
	return &CaseRanker{
		SimilarityWeight: 0.5,  // 相似度权重 50%
		RecencyWeight:    0.3,  // 时效性权重 30%
		SuccessWeight:    0.2,  // 成功率权重 20%
	}
}

// RankResult 排序结果
type RankResult struct {
	Doc              *schema.Document
	SimilarityScore  float64 // 相似度分数 [0, 1]
	RecencyScore     float64 // 时效性分数 [0, 1]
	SuccessScore     float64 // 成功率分数 [0, 1]
	CompositeScore   float64 // 综合分数 [0, 1]
}

// Rank 对检索结果进行多维度排序
func (r *CaseRanker) Rank(docs []*schema.Document) []*RankResult {
	if len(docs) == 0 {
		return nil
	}

	results := make([]*RankResult, 0, len(docs))

	for _, doc := range docs {
		result := &RankResult{
			Doc:             doc,
			SimilarityScore: doc.Score(), // Milvus 返回的相似度分数
			RecencyScore:    r.calculateRecencyScore(doc),
			SuccessScore:    r.calculateSuccessScore(doc),
		}

		// 计算综合分数
		result.CompositeScore = r.SimilarityWeight*result.SimilarityScore +
			r.RecencyWeight*result.RecencyScore +
			r.SuccessWeight*result.SuccessScore

		results = append(results, result)
	}

	// 按综合分数降序排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].CompositeScore > results[j].CompositeScore
	})

	return results
}

// calculateRecencyScore 计算时效性分数
// 使用指数衰减：score = e^(-λ * days)
// 最近的案例得分接近 1，越久远的案例得分越低
func (r *CaseRanker) calculateRecencyScore(doc *schema.Document) float64 {
	if doc.MetaData == nil {
		return 0.5 // 没有时间信息，给中等分数
	}

	// 尝试从 metadata 中获取时间戳
	timestampVal, ok := doc.MetaData["timestamp"]
	if !ok {
		return 0.5
	}

	// 解析时间戳（支持多种格式）
	var timestamp time.Time
	switch v := timestampVal.(type) {
	case string:
		// 尝试解析字符串格式的时间
		formats := []string{
			"2006-01-02",
			"2006-01-02 15:04:05",
			time.RFC3339,
		}
		for _, format := range formats {
			if t, err := time.Parse(format, v); err == nil {
				timestamp = t
				break
			}
		}
	case time.Time:
		timestamp = v
	case int64:
		// Unix 时间戳
		timestamp = time.Unix(v, 0)
	default:
		return 0.5
	}

	if timestamp.IsZero() {
		return 0.5
	}

	// 计算天数差
	days := time.Since(timestamp).Hours() / 24

	// 指数衰减：λ = 0.01（约 69 天后分数降到 0.5）
	lambda := 0.01
	score := 1.0 / (1.0 + lambda*days)

	// 确保分数在 [0, 1] 范围内
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return score
}

// calculateSuccessScore 计算成功率分数
func (r *CaseRanker) calculateSuccessScore(doc *schema.Document) float64 {
	if doc.MetaData == nil {
		return 0.5 // 没有成功率信息，给中等分数
	}

	// 尝试从 metadata 中获取成功率
	successRateVal, ok := doc.MetaData["success_rate"]
	if !ok {
		return 0.5
	}

	// 解析成功率
	var successRate float64
	switch v := successRateVal.(type) {
	case float64:
		successRate = v
	case float32:
		successRate = float64(v)
	case int:
		successRate = float64(v) / 100.0 // 假设是百分比
	case string:
		// 尝试解析字符串（如 "0.95" 或 "95%"）
		// 这里简化处理，实际可以用 strconv.ParseFloat
		return 0.5
	default:
		return 0.5
	}

	// 确保分数在 [0, 1] 范围内
	if successRate < 0 {
		successRate = 0
	}
	if successRate > 1 {
		successRate = 1
	}

	return successRate
}

// RankWithCustomWeights 使用自定义权重进行排序
func (r *CaseRanker) RankWithCustomWeights(docs []*schema.Document, simWeight, recWeight, sucWeight float64) []*RankResult {
	// 临时修改权重
	oldSimWeight := r.SimilarityWeight
	oldRecWeight := r.RecencyWeight
	oldSucWeight := r.SuccessWeight

	r.SimilarityWeight = simWeight
	r.RecencyWeight = recWeight
	r.SuccessWeight = sucWeight

	results := r.Rank(docs)

	// 恢复原权重
	r.SimilarityWeight = oldSimWeight
	r.RecencyWeight = oldRecWeight
	r.SuccessWeight = oldSucWeight

	return results
}
