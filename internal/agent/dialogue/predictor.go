package dialogue

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"

	appcontext "go_agent/internal/context"

	"github.com/cloudwego/eino/compose"
)

// IntentPredictor 意图预测器
type IntentPredictor struct {
	embedder compose.Embedder // 向量化器（可选）
	cache    sync.Map         // 缓存
}

// NewIntentPredictor 创建意图预测器
func NewIntentPredictor(embedder compose.Embedder) *IntentPredictor {
	return &IntentPredictor{
		embedder: embedder,
	}
}

// Predict 预测意图
func (p *IntentPredictor) Predict(ctx context.Context, session *appcontext.SessionContext, userInput string) (*Intent, error) {
	// 简单的基于关键词的意图预测
	// TODO: 使用 LLM 或分类模型增强

	intent := &Intent{
		Type:       "general",
		Confidence: 0.5,
		Entities:   make(map[string]interface{}),
	}

	input := strings.ToLower(userInput)

	// 监控类意图
	monitorKeywords := []string{"查看", "监控", "状态", "指标", "cpu", "内存", "磁盘"}
	if containsAny(input, monitorKeywords) {
		intent.Type = "monitor"
		intent.Confidence = 0.8
		return intent, nil
	}

	// 诊断类意图
	diagnoseKeywords := []string{"故障", "问题", "错误", "异常", "报错", "失败", "超时", "慢"}
	if containsAny(input, diagnoseKeywords) {
		intent.Type = "diagnose"
		intent.Confidence = 0.85
		return intent, nil
	}

	// 执行类意图
	executeKeywords := []string{"重启", "扩容", "缩容", "部署", "回滚", "执行", "修复"}
	if containsAny(input, executeKeywords) {
		intent.Type = "execute"
		intent.Confidence = 0.9
		return intent, nil
	}

	// 知识类意图
	knowledgeKeywords := []string{"历史", "案例", "文档", "经验", "之前", "类似", "怎么", "如何"}
	if containsAny(input, knowledgeKeywords) {
		intent.Type = "knowledge"
		intent.Confidence = 0.75
		return intent, nil
	}

	return intent, nil
}

// PredictWithVector 使用向量预测意图（需要 embedder）
func (p *IntentPredictor) PredictWithVector(ctx context.Context, session *appcontext.SessionContext, userInput string) (*Intent, error) {
	if p.embedder == nil {
		return p.Predict(ctx, session, userInput)
	}

	// 向量化用户输入
	vectors, err := p.embedder.EmbedStrings(ctx, []string{userInput})
	if err != nil {
		return nil, fmt.Errorf("failed to embed input: %w", err)
	}

	if len(vectors) == 0 {
		return nil, fmt.Errorf("no vectors returned")
	}

	// TODO: 使用向量进行意图分类
	// 这里可以：
	// 1. 与预定义的意图向量计算相似度
	// 2. 使用分类模型
	// 3. 检索历史相似对话的意图

	// 暂时回退到关键词匹配
	return p.Predict(ctx, session, userInput)
}

// containsAny 检查是否包含任意关键词
func containsAny(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

// EntropyCalculator 语义熵计算器
type EntropyCalculator struct {
	embedder compose.Embedder // 向量化器（可选）
}

// NewEntropyCalculator 创建熵计算器
func NewEntropyCalculator() *EntropyCalculator {
	return &EntropyCalculator{}
}

// Calculate 计算语义熵
func (e *EntropyCalculator) Calculate(ctx context.Context, session *appcontext.SessionContext) (float64, error) {
	session.mu.RLock()
	defer session.mu.RUnlock()

	if len(session.History) < 2 {
		return 1.0, nil // 初始状态，熵最大
	}

	// 简单实现：基于对话长度和重复度
	// 对话越长，重复度越高，熵越低

	// 计算最近几轮对话的关键词重复度
	recentMessages := session.History
	if len(recentMessages) > 6 {
		recentMessages = recentMessages[len(recentMessages)-6:]
	}

	// 提取所有关键词
	allKeywords := make(map[string]int)
	for _, msg := range recentMessages {
		keywords := extractSimpleKeywords(msg.Content)
		for _, kw := range keywords {
			allKeywords[kw]++
		}
	}

	// 计算重复度
	totalKeywords := 0
	repeatedKeywords := 0
	for _, count := range allKeywords {
		totalKeywords += count
		if count > 1 {
			repeatedKeywords += count
		}
	}

	if totalKeywords == 0 {
		return 1.0, nil
	}

	// 重复度越高，熵越低
	repeatRate := float64(repeatedKeywords) / float64(totalKeywords)
	entropy := 1.0 - repeatRate

	// 限制在 [0, 1] 范围内
	if entropy < 0 {
		entropy = 0
	}
	if entropy > 1 {
		entropy = 1
	}

	return entropy, nil
}

// CalculateWithVector 使用向量计算熵（需要 embedder）
func (e *EntropyCalculator) CalculateWithVector(ctx context.Context, session *appcontext.SessionContext) (float64, error) {
	if e.embedder == nil {
		return e.Calculate(ctx, session)
	}

	session.mu.RLock()
	defer session.mu.RUnlock()

	if len(session.History) < 2 {
		return 1.0, nil
	}

	// 获取最近两轮对话
	recentMessages := session.History
	if len(recentMessages) > 4 {
		recentMessages = recentMessages[len(recentMessages)-4:]
	}

	// 拼接对话内容
	text1 := ""
	text2 := ""
	mid := len(recentMessages) / 2

	for i := 0; i < mid; i++ {
		text1 += recentMessages[i].Content + " "
	}
	for i := mid; i < len(recentMessages); i++ {
		text2 += recentMessages[i].Content + " "
	}

	// 向量化
	vectors, err := e.embedder.EmbedStrings(ctx, []string{text1, text2})
	if err != nil {
		return 0, fmt.Errorf("failed to embed texts: %w", err)
	}

	if len(vectors) < 2 {
		return 1.0, nil
	}

	// 计算余弦相似度
	similarity := cosineSimilarity(vectors[0], vectors[1])

	// 相似度越高，熵越低
	entropy := 1.0 - similarity

	return entropy, nil
}

// cosineSimilarity 计算余弦相似度
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// extractSimpleKeywords 简单的关键词提取
func extractSimpleKeywords(text string) []string {
	// 简单分词（按空格）
	words := strings.Fields(text)

	// 过滤停用词
	stopWords := map[string]bool{
		"的": true, "了": true, "是": true, "在": true,
		"有": true, "和": true, "就": true, "不": true,
		"我": true, "你": true, "他": true, "她": true,
	}

	keywords := make([]string, 0)
	for _, word := range words {
		if !stopWords[word] && len(word) > 1 {
			keywords = append(keywords, word)
		}
	}

	return keywords
}
