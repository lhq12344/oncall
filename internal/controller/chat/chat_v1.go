package chat

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	v1 "go_agent/api/chat/v1"
	"go_agent/internal/agent/ops"
	"go_agent/internal/cache"
	"go_agent/internal/concurrent"
	"go_agent/internal/healing"
	"go_agent/utility/mem"
	"go_agent/utility/tokenizer"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type ControllerV1 struct {
	supervisorAgent adk.ResumableAgent
	logger          *zap.Logger

	cacheManager   *cache.Manager
	llmCache       *cache.LLMCache
	cbManager      *concurrent.CircuitBreakerManager
	opsExecutor    *ops.IntegratedOpsExecutor
	healingManager *healing.HealingLoopManager

	cacheHits   int64
	cacheMisses int64
}

func NewV1(
	supervisorAgent adk.ResumableAgent,
	logger *zap.Logger,
	redisClient *redis.Client,
	opsExecutor *ops.IntegratedOpsExecutor,
	healingManager *healing.HealingLoopManager,
) *ControllerV1 {
	ctrl := &ControllerV1{
		supervisorAgent: supervisorAgent,
		logger:          logger,
		opsExecutor:     opsExecutor,
		healingManager:  healingManager,
	}

	ctrl.cbManager = concurrent.NewCircuitBreakerManager(logger)

	if redisClient != nil {
		cacheCfg := cache.DefaultConfig()
		cacheCfg.RedisClient = redisClient
		cacheCfg.Logger = logger

		cacheManager, err := cache.NewManager(cacheCfg)
		if err != nil {
			if logger != nil {
				logger.Warn("cache disabled due to init failure", zap.Error(err))
			}
		} else {
			ctrl.cacheManager = cacheManager
			ctrl.llmCache = cache.NewLLMCache(cacheManager)
			if logger != nil {
				logger.Info("chat controller cache initialized")
			}
		}
	}

	return ctrl
}

func (c *ControllerV1) Chat(ctx context.Context, req *v1.ChatReq) (res *v1.ChatRes, err error) {
	// 参数验证
	if req.Question == "" {
		return nil, fmt.Errorf("question is required")
	}
	if req.Id == "" {
		req.Id = "default-session"
	}

	c.logger.Info("chat request received",
		zap.String("session_id", req.Id),
		zap.String("question", req.Question))

	// 获取会话历史
	memory := mem.GetSimpleMemory(req.Id)
	historyMsgs, err := mem.GetMessagesForRequest(ctx, req.Id, schema.UserMessage(req.Question), 0)
	if err != nil {
		c.logger.Warn("failed to get history, using empty history",
			zap.String("session_id", req.Id),
			zap.Error(err))
		historyMsgs = []*schema.Message{}
	}

	cacheKey := c.buildLLMCacheKey(req.Id, historyMsgs, req.Question)
	if c.llmCache != nil {
		cached, cacheErr := c.llmCache.Get(ctx, cacheKey)
		if cacheErr == nil && cached != nil && cached.Response != nil {
			atomic.AddInt64(&c.cacheHits, 1)
			c.logger.Info("llm cache hit",
				zap.String("session_id", req.Id),
				zap.Float64("cache_hit_rate", c.GetCacheHitRate()))
			return &v1.ChatRes{Answer: cached.Response.Content}, nil
		}

		atomic.AddInt64(&c.cacheMisses, 1)
		if cacheErr != nil && cacheErr != cache.ErrCacheMiss {
			c.logger.Warn("llm cache get failed", zap.String("session_id", req.Id), zap.Error(cacheErr))
		}
	}

	// 构建输入
	input := &adk.AgentInput{
		Messages: []adk.Message{
			{
				Role:    schema.User,
				Content: req.Question,
			},
		},
	}

	// 如果有历史消息，添加到输入
	if len(historyMsgs) > 0 {
		for _, msg := range historyMsgs {
			input.Messages = append([]adk.Message{{
				Role:    msg.Role,
				Content: msg.Content,
			}}, input.Messages...)
		}
	}

	cb := c.cbManager.GetOrCreate("supervisor_chat", &concurrent.CircuitBreakerConfig{
		FailureThreshold: 3,
		MinRequestCount:  3,
		Timeout:          30 * time.Second,
	})

	result, execErr := cb.Execute(ctx, func(execCtx context.Context) (interface{}, error) {
		iterator := c.supervisorAgent.Run(execCtx, input)

		var answer string
		for {
			event, ok := iterator.Next()
			if !ok {
				break
			}

			if event.Err != nil {
				return nil, event.Err
			}

			if event.Output != nil && event.Output.MessageOutput != nil {
				if event.Output.MessageOutput.Message != nil {
					answer += event.Output.MessageOutput.Message.Content
				}
			}
		}

		return answer, nil
	})
	if execErr != nil {
		c.logger.Error("supervisor agent execution failed",
			zap.String("session_id", req.Id),
			zap.Error(execErr))
		return nil, fmt.Errorf("agent error: %w", execErr)
	}

	answer, _ := result.(string)

	if answer == "" {
		answer = "抱歉，我无法生成回答。"
	}

	if c.llmCache != nil {
		cacheCtx := context.WithValue(ctx, cache.CacheTimestampContextKey, time.Now().Unix())
		if cacheErr := c.llmCache.Set(cacheCtx, cacheKey, schema.AssistantMessage(answer, nil)); cacheErr != nil {
			c.logger.Warn("failed to set llm cache",
				zap.String("session_id", req.Id),
				zap.Error(cacheErr))
		}
	}

	// 保存对话历史
	userMsg := schema.UserMessage(req.Question)
	assistantMsg := schema.AssistantMessage(answer, nil) // 第二个参数是 ToolCalls

	// 优先使用 DeepSeek Tokenization 做精确计数，失败则回退到粗估算。
	promptTokens := len(req.Question) / 4
	if precisePromptTokens, e := tokenizer.CountMessagesTokens(ctx, historyMsgs, false); e == nil && precisePromptTokens > 0 {
		promptTokens = precisePromptTokens
	}

	completionTokens := len(answer) / 4
	if preciseCompletionTokens, e := tokenizer.CountMessageTokens(ctx, assistantMsg, false); e == nil && preciseCompletionTokens > 0 {
		completionTokens = preciseCompletionTokens
	}

	err = memory.SetMessages(ctx, userMsg, assistantMsg, historyMsgs, promptTokens, completionTokens)
	if err != nil {
		c.logger.Warn("failed to save history",
			zap.String("session_id", req.Id),
			zap.Error(err))
	}

	c.logger.Info("chat response generated",
		zap.String("session_id", req.Id),
		zap.Int("answer_length", len(answer)),
		zap.Float64("cache_hit_rate", c.GetCacheHitRate()))

	return &v1.ChatRes{
		Answer: answer,
	}, nil
}

func (c *ControllerV1) buildLLMCacheKey(sessionID string, historyMsgs []*schema.Message, question string) *cache.LLMCacheKey {
	cacheMessages := make([]*schema.Message, 0, len(historyMsgs)+1)
	cacheMessages = append(cacheMessages, historyMsgs...)
	cacheMessages = append(cacheMessages, schema.UserMessage(question))

	return &cache.LLMCacheKey{
		AgentID:   "supervisor",
		SessionID: sessionID,
		Messages:  cacheMessages,
		Model:     "supervisor",
	}
}

func (c *ControllerV1) GetCacheHitRate() float64 {
	hits := atomic.LoadInt64(&c.cacheHits)
	misses := atomic.LoadInt64(&c.cacheMisses)
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

func (c *ControllerV1) ChatStream(ctx context.Context, req *v1.ChatStreamReq) (res *v1.ChatStreamRes, err error) {
	// TODO: Implement streaming chat logic
	return &v1.ChatStreamRes{}, nil
}

func (c *ControllerV1) FileUpload(ctx context.Context, req *v1.FileUploadReq) (res *v1.FileUploadRes, err error) {
	// TODO: Implement file upload logic
	return &v1.FileUploadRes{}, nil
}

func (c *ControllerV1) Monitoring(ctx context.Context, req *v1.MonitoringReq) (res *v1.MonitoringRes, err error) {
	hits := atomic.LoadInt64(&c.cacheHits)
	misses := atomic.LoadInt64(&c.cacheMisses)

	breakers := []v1.CircuitBreakerStatus{}
	if c.cbManager != nil {
		for _, name := range c.cbManager.List() {
			cb, err := c.cbManager.Get(name)
			if err != nil {
				continue
			}

			state := cb.State()
			requests, successes, failures := cb.Counts()

			breakers = append(breakers, v1.CircuitBreakerStatus{
				Name:      name,
				State:     state.String(),
				Requests:  requests,
				Successes: successes,
				Failures:  failures,
			})
		}
	}

	return &v1.MonitoringRes{
		CacheHitRate:    c.GetCacheHitRate(),
		CacheHits:       hits,
		CacheMisses:     misses,
		CircuitBreakers: breakers,
	}, nil
}

func (c *ControllerV1) AIOps(ctx context.Context, req *v1.AIOpsReq) (res *v1.AIOpsRes, err error) {
	if c.opsExecutor == nil {
		return &v1.AIOpsRes{
			Result: "AI Ops integration executor is unavailable",
			Detail: []string{"ops integration not initialized in bootstrap"},
		}, nil
	}

	result, execErr := c.opsExecutor.QueryAllSources(ctx, ops.QueryAllSourcesInput{
		SessionID: "ai-ops-default",
		Namespace: "default",
		PromQuery: `sum(rate(container_cpu_usage_seconds_total{container!="",pod!=""}[5m])) by (pod)`,
		TimeRange: "5m",
		ESIndex:   "logs-*",
		ESQuery:   "error OR exception",
		ESLevel:   "error",
		ESSize:    100,
	})
	if execErr != nil {
		return nil, fmt.Errorf("ai ops execution failed: %w", execErr)
	}

	details := make([]string, 0, len(result.Data)+len(result.CacheHits)+len(result.Errors)+1)
	keys := make([]string, 0, len(result.Data))
	for k := range result.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		details = append(details, fmt.Sprintf("source=%s payload=%s", k, result.Data[k]))
	}

	cacheHitKeys := make([]string, 0, len(result.CacheHits))
	for k, hit := range result.CacheHits {
		if hit {
			cacheHitKeys = append(cacheHitKeys, k)
		}
	}
	sort.Strings(cacheHitKeys)
	for _, k := range cacheHitKeys {
		details = append(details, fmt.Sprintf("cache_hit source=%s", k))
	}

	errKeys := make([]string, 0, len(result.Errors))
	for k := range result.Errors {
		errKeys = append(errKeys, k)
	}
	sort.Strings(errKeys)
	for _, k := range errKeys {
		details = append(details, fmt.Sprintf("source=%s error=%s", k, result.Errors[k]))
	}

	return &v1.AIOpsRes{
		Result: "AI Ops multi-source aggregation completed",
		Detail: details,
	}, nil
}

// HealingTrigger 触发自愈
func (c *ControllerV1) HealingTrigger(ctx context.Context, req *v1.HealingTriggerReq) (res *v1.HealingTriggerRes, err error) {
	if c.healingManager == nil {
		return nil, fmt.Errorf("healing manager not initialized")
	}

	c.logger.Info("healing trigger request",
		zap.String("incident_id", req.IncidentID),
		zap.String("type", req.Type),
		zap.String("severity", req.Severity))

	// 创建故障事件
	incident := &healing.Incident{
		ID:          req.IncidentID,
		Timestamp:   time.Now(),
		Severity:    healing.Severity(req.Severity),
		Type:        healing.IncidentType(req.Type),
		Title:       req.Title,
		Description: req.Description,
	}

	// 触发自愈
	session, err := c.healingManager.TriggerHealing(ctx, incident)
	if err != nil {
		c.logger.Error("failed to trigger healing", zap.Error(err))
		return nil, fmt.Errorf("failed to trigger healing: %w", err)
	}

	return &v1.HealingTriggerRes{
		SessionID: session.ID,
		Message:   "Healing triggered successfully",
	}, nil
}

// HealingStatus 查询自愈状态
func (c *ControllerV1) HealingStatus(ctx context.Context, req *v1.HealingStatusReq) (res *v1.HealingStatusRes, err error) {
	if c.healingManager == nil {
		return nil, fmt.Errorf("healing manager not initialized")
	}

	var sessions []*healing.HealingSession
	if req.SessionID != "" {
		// 查询特定会话
		session, err := c.healingManager.GetSession(req.SessionID)
		if err != nil {
			return nil, fmt.Errorf("session not found: %w", err)
		}
		sessions = []*healing.HealingSession{session}
	} else {
		// 查询所有活跃会话
		sessions = c.healingManager.GetActiveSessions()
	}

	// 转换为响应格式
	infos := make([]v1.HealingSessionInfo, 0, len(sessions))
	for _, session := range sessions {
		info := v1.HealingSessionInfo{
			SessionID:    session.ID,
			State:        string(session.State),
			IncidentID:   session.Incident.ID,
			IncidentType: string(session.Incident.Type),
			Severity:     string(session.Incident.Severity),
			StartTime:    session.StartTime.Format(time.RFC3339),
			RetryCount:   session.RetryCount,
		}

		// 添加诊断信息（如果有）
		if session.Diagnosis != nil {
			info.RootCause = session.Diagnosis.RootCause
		}

		// 添加决策信息（如果有）
		if session.Decision != nil && session.Decision.Strategy != nil {
			info.Strategy = session.Decision.Strategy.Name
		}

		infos = append(infos, info)
	}

	return &v1.HealingStatusRes{
		Sessions: infos,
	}, nil
}
