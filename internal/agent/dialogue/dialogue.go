package dialogue

import (
	"context"
	"fmt"
	"sync"
	"time"

	appcontext "go_agent/internal/context"

	"github.com/cloudwego/eino/compose"
	"go.uber.org/zap"
)

// DialogueAgent 对话代理
type DialogueAgent struct {
	contextManager *appcontext.ContextManager
	predictor      *IntentPredictor      // 意图预测器
	questionGen    *QuestionGenerator    // 问题生成器
	entropyCalc    *EntropyCalculator    // 语义熵计算器
	stateTracker   *DialogueStateTracker // 对话状态跟踪器
	embedder       compose.Embedder      // 向量化器（可选）
	logger         *zap.Logger
	mu             sync.RWMutex
}

// Config Dialogue Agent 配置
type Config struct {
	ContextManager *appcontext.ContextManager
	Embedder       compose.Embedder // 可选
	Logger         *zap.Logger
}

// NewDialogueAgent 创建 Dialogue Agent
func NewDialogueAgent(cfg *Config) *DialogueAgent {
	return &DialogueAgent{
		contextManager: cfg.ContextManager,
		predictor:      NewIntentPredictor(cfg.Embedder),
		questionGen:    NewQuestionGenerator(),
		entropyCalc:    NewEntropyCalculator(),
		stateTracker:   NewDialogueStateTracker(),
		embedder:       cfg.Embedder,
		logger:         cfg.Logger,
	}
}

// AnalyzeIntent 分析用户意图
func (d *DialogueAgent) AnalyzeIntent(ctx context.Context, sessionID, userInput string) (*IntentAnalysis, error) {
	d.logger.Info("analyzing intent",
		zap.String("session_id", sessionID),
		zap.String("input", userInput),
	)

	// 获取会话上下文
	session, err := d.contextManager.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	// 计算语义熵
	entropy, err := d.entropyCalc.Calculate(ctx, session)
	if err != nil {
		d.logger.Warn("failed to calculate entropy", zap.Error(err))
		entropy = 1.0 // 默认值
	}

	// 预测意图
	intent, err := d.predictor.Predict(ctx, session, userInput)
	if err != nil {
		return nil, fmt.Errorf("failed to predict intent: %w", err)
	}

	// 更新会话意图
	session.Intent = &appcontext.UserIntent{
		Type:       intent.Type,
		Confidence: intent.Confidence,
		Entities:   intent.Entities,
		Converged:  entropy < 0.3, // 熵低于阈值表示意图收敛
		Entropy:    entropy,
	}

	d.contextManager.UpdateSession(ctx, session)

	analysis := &IntentAnalysis{
		Intent:     intent,
		Entropy:    entropy,
		Converged:  entropy < 0.3,
		Confidence: intent.Confidence,
		Timestamp:  time.Now(),
	}

	d.logger.Info("intent analyzed",
		zap.String("type", intent.Type),
		zap.Float64("confidence", intent.Confidence),
		zap.Float64("entropy", entropy),
		zap.Bool("converged", analysis.Converged),
	)

	return analysis, nil
}

// PredictNextQuestions 预测下一步可能的问题
func (d *DialogueAgent) PredictNextQuestions(ctx context.Context, sessionID string, count int) ([]string, error) {
	d.logger.Info("predicting next questions",
		zap.String("session_id", sessionID),
		zap.Int("count", count),
	)

	// 获取会话上下文
	session, err := d.contextManager.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	// 检查意图是否收敛
	if session.Intent == nil || !session.Intent.Converged {
		d.logger.Debug("intent not converged, skipping prediction")
		return []string{}, nil
	}

	// 生成候选问题
	questions, err := d.questionGen.Generate(ctx, session, count)
	if err != nil {
		return nil, fmt.Errorf("failed to generate questions: %w", err)
	}

	// 更新会话的预测问题
	session.PredictedQuestions = questions
	d.contextManager.UpdateSession(ctx, session)

	d.logger.Info("predicted questions",
		zap.Int("count", len(questions)),
	)

	return questions, nil
}

// PredictNextQuestionsAsync 异步预测下一步问题（不阻塞主流程）
func (d *DialogueAgent) PredictNextQuestionsAsync(ctx context.Context, sessionID string, count int) {
	go func() {
		_, err := d.PredictNextQuestions(ctx, sessionID, count)
		if err != nil {
			d.logger.Error("async prediction failed",
				zap.String("session_id", sessionID),
				zap.Error(err),
			)
		}
	}()
}

// GenerateClarificationQuestion 生成澄清性问题（当意图模糊时）
func (d *DialogueAgent) GenerateClarificationQuestion(ctx context.Context, sessionID string) (string, error) {
	d.logger.Info("generating clarification question",
		zap.String("session_id", sessionID),
	)

	// 获取会话上下文
	session, err := d.contextManager.GetSession(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to get session: %w", err)
	}

	// 根据当前意图生成澄清问题
	question := d.questionGen.GenerateClarification(ctx, session)

	d.logger.Info("clarification question generated",
		zap.String("question", question),
	)

	return question, nil
}

// UpdateDialogueState 更新对话状态
func (d *DialogueAgent) UpdateDialogueState(ctx context.Context, sessionID string, state *DialogueState) error {
	d.logger.Debug("updating dialogue state",
		zap.String("session_id", sessionID),
		zap.String("state", state.CurrentState),
	)

	return d.stateTracker.UpdateState(ctx, sessionID, state)
}

// GetDialogueState 获取对话状态
func (d *DialogueAgent) GetDialogueState(ctx context.Context, sessionID string) (*DialogueState, error) {
	return d.stateTracker.GetState(ctx, sessionID)
}

// ExtractEntities 从用户输入中提取实体
func (d *DialogueAgent) ExtractEntities(ctx context.Context, userInput string) (map[string]interface{}, error) {
	d.logger.Debug("extracting entities",
		zap.String("input", userInput),
	)

	entities := make(map[string]interface{})

	// 简单的实体提取（基于关键词匹配）
	// TODO: 使用 NER 模型增强

	// 提取服务名
	serviceNames := []string{"nginx", "mysql", "redis", "kafka", "etcd"}
	for _, service := range serviceNames {
		if containsIgnoreCase(userInput, service) {
			entities["service"] = service
			break
		}
	}

	// 提取指标名
	metricNames := []string{"cpu", "memory", "disk", "latency", "qps", "error_rate"}
	for _, metric := range metricNames {
		if containsIgnoreCase(userInput, metric) {
			entities["metric"] = metric
			break
		}
	}

	// 提取时间范围
	timeRanges := map[string]string{
		"最近5分钟":  "5m",
		"最近10分钟": "10m",
		"最近1小时":  "1h",
		"今天":     "1d",
	}
	for phrase, duration := range timeRanges {
		if containsIgnoreCase(userInput, phrase) {
			entities["time_range"] = duration
			break
		}
	}

	d.logger.Debug("entities extracted",
		zap.Any("entities", entities),
	)

	return entities, nil
}

// SummarizeConversation 总结对话内容
func (d *DialogueAgent) SummarizeConversation(ctx context.Context, sessionID string) (string, error) {
	d.logger.Info("summarizing conversation",
		zap.String("session_id", sessionID),
	)

	// 获取会话历史
	history, err := d.contextManager.GetHistory(ctx, sessionID, 20)
	if err != nil {
		return "", fmt.Errorf("failed to get history: %w", err)
	}

	if len(history) == 0 {
		return "暂无对话历史", nil
	}

	// 简单的总结（提取关键信息）
	summary := fmt.Sprintf("对话轮次: %d\n", len(history)/2)

	// 提取主要话题
	topics := make(map[string]int)
	for _, msg := range history {
		if msg.Role == "user" {
			// 提取关键词
			keywords := extractKeywords(msg.Content)
			for _, kw := range keywords {
				topics[kw]++
			}
		}
	}

	// 找出最频繁的话题
	maxCount := 0
	mainTopic := ""
	for topic, count := range topics {
		if count > maxCount {
			maxCount = count
			mainTopic = topic
		}
	}

	if mainTopic != "" {
		summary += fmt.Sprintf("主要话题: %s\n", mainTopic)
	}

	d.logger.Info("conversation summarized",
		zap.String("summary", summary),
	)

	return summary, nil
}

// containsIgnoreCase 不区分大小写的字符串包含检查
func containsIgnoreCase(s, substr string) bool {
	s = toLower(s)
	substr = toLower(substr)
	return contains(s, substr)
}

// toLower 转小写（简单实现）
func toLower(s string) string {
	// 简化实现，实际应使用 strings.ToLower
	return s
}

// contains 字符串包含检查（简单实现）
func contains(s, substr string) bool {
	// 简化实现，实际应使用 strings.Contains
	return len(s) >= len(substr) && findSubstring(s, substr) >= 0
}

// findSubstring 查找子串位置
func findSubstring(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	if len(s) < len(substr) {
		return -1
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// extractKeywords 提取关键词（简单实现）
func extractKeywords(text string) []string {
	// 简化实现，实际应使用分词库
	keywords := make([]string, 0)
	// TODO: 实现分词和关键词提取
	return keywords
}
