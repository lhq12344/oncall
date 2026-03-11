package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	v1 "go_agent/api/chat/v1"
	"go_agent/internal/agent/ops"
	"go_agent/internal/cache"
	"go_agent/internal/concurrent"
	appcontext "go_agent/internal/context"
	"go_agent/internal/healing"
	"go_agent/utility/mem"
	"go_agent/utility/tokenizer"

	milvusIndexer "github.com/cloudwego/eino-ext/components/indexer/milvus"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type ControllerV1 struct {
	chatAgent        adk.ResumableAgent
	chatRunner       *adk.Runner
	chatStreamRunner *adk.Runner
	rootAgentName    string
	checkPointStore  compose.CheckPointStore
	logger           *zap.Logger

	cacheManager   *cache.Manager
	llmCache       *cache.LLMCache
	cbManager      *concurrent.CircuitBreakerManager
	opsExecutor    *ops.IntegratedOpsExecutor
	opsAgent       adk.Agent   // Plan-Execute-Replan Ops Agent
	milvusIndexer  interface{} // Milvus Indexer for direct document indexing
	healingManager *healing.HealingLoopManager
	redisClient    *redis.Client

	localInterruptState sync.Map // key: session_id:checkpoint_id -> []v1.InterruptContext

	checkpointTTL time.Duration
	cacheHits     int64
	cacheMisses   int64
}

func NewV1(
	chatAgent adk.ResumableAgent,
	logger *zap.Logger,
	redisClient *redis.Client,
	opsExecutor *ops.IntegratedOpsExecutor,
	opsAgent adk.Agent,
	milvusIndexer interface{},
	healingManager *healing.HealingLoopManager,
) *ControllerV1 {
	ctrl := &ControllerV1{
		chatAgent:      chatAgent,
		rootAgentName:  "chat_agent",
		logger:         logger,
		opsExecutor:    opsExecutor,
		opsAgent:       opsAgent,
		milvusIndexer:  milvusIndexer,
		healingManager: healingManager,
		redisClient:    redisClient,
		checkpointTTL:  24 * time.Hour,
	}

	if chatAgent != nil {
		if agentName := strings.TrimSpace(chatAgent.Name(context.Background())); agentName != "" {
			ctrl.rootAgentName = agentName
		}
		if redisClient != nil {
			ctrl.checkPointStore = appcontext.NewRedisCheckPointStore(redisClient, "oncall", ctrl.checkpointTTL)
		} else {
			ctrl.checkPointStore = newInMemoryCheckPointStore()
		}
		ctrl.chatRunner = adk.NewRunner(context.Background(), adk.RunnerConfig{
			Agent:           chatAgent,
			EnableStreaming: false,
			CheckPointStore: ctrl.checkPointStore,
		})
		ctrl.chatStreamRunner = adk.NewRunner(context.Background(), adk.RunnerConfig{
			Agent:           chatAgent,
			EnableStreaming: true,
			CheckPointStore: ctrl.checkPointStore,
		})
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

	// 默认走 SSE，对话请求可通过 sse=false 显式关闭。
	if req.SSE == nil || *req.SSE {
		_, err = c.ChatStream(ctx, &v1.ChatStreamReq{
			Id:       req.Id,
			Question: req.Question,
		})
		if err != nil {
			return nil, err
		}
		return &v1.ChatRes{}, nil
	}

	c.logger.Info("chat request received",
		zap.String("session_id", req.Id),
		zap.String("question", req.Question))

	// 获取会话历史（限制最近 5 轮对话，避免上下文过长）
	memory := mem.GetSimpleMemory(req.Id)
	historyMsgs, err := mem.GetMessagesForRequest(ctx, req.Id, schema.UserMessage(req.Question), 5) // 限制为最近 5 轮
	if err != nil {
		c.logger.Warn("failed to get history, using empty history",
			zap.String("session_id", req.Id),
			zap.Error(err))
		historyMsgs = []*schema.Message{}
	}

	// 检测是否是简单问候或新话题（不需要历史上下文）
	isSimpleGreeting := isGreetingOrSimpleQuery(req.Question)
	if isSimpleGreeting {
		c.logger.Info("detected simple greeting, clearing history context",
			zap.String("question", req.Question))
		historyMsgs = []*schema.Message{} // 清空历史，避免回答无关内容
	}

	cacheKey := c.buildLLMCacheKey(req.Id, historyMsgs, req.Question)
	if c.llmCache != nil {
		cached, cacheErr := c.llmCache.Get(ctx, cacheKey)
		if cacheErr == nil && cached != nil && cached.Response != nil {
			atomic.AddInt64(&c.cacheHits, 1)
			c.logger.Info("llm cache hit",
				zap.String("session_id", req.Id),
				zap.Float64("cache_hit_rate", c.getCacheHitRate()))
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

	cb := c.cbManager.GetOrCreate(c.rootAgentName+"_chat", &concurrent.CircuitBreakerConfig{
		FailureThreshold: 3,
		MinRequestCount:  3,
		Timeout:          30 * time.Second,
	})

	checkpointID := c.generateCheckpointID(req.Id)
	result, execErr := cb.Execute(ctx, func(execCtx context.Context) (interface{}, error) {
		if c.chatRunner == nil {
			return nil, fmt.Errorf("chat runner is not initialized")
		}
		iterator := c.chatRunner.Run(execCtx, input.Messages, adk.WithCheckPointID(checkpointID))
		return c.collectChatResult(iterator, checkpointID)
	})
	if execErr != nil {
		c.logger.Error("chat agent execution failed",
			zap.String("session_id", req.Id),
			zap.Error(execErr))
		return nil, fmt.Errorf("agent error: %w", execErr)
	}

	runResult, _ := result.(*chatRunResult)
	answer := ""
	if runResult != nil {
		answer = runResult.Answer
		if runResult.Interrupted {
			c.saveInterruptState(ctx, req.Id, checkpointID, runResult.InterruptContexts)
		}
	}

	if answer == "" {
		answer = "抱歉，我无法生成回答。"
	}

	if c.llmCache != nil && (runResult == nil || !runResult.Interrupted) {
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

	if runResult == nil || !runResult.Interrupted {
		err = memory.SetMessages(ctx, userMsg, assistantMsg, historyMsgs, promptTokens, completionTokens)
		if err != nil {
			c.logger.Warn("failed to save history",
				zap.String("session_id", req.Id),
				zap.Error(err))
		}
	}

	c.logger.Info("chat response generated",
		zap.String("session_id", req.Id),
		zap.Int("answer_length", len(answer)),
		zap.Float64("cache_hit_rate", c.getCacheHitRate()))

	response := &v1.ChatRes{
		Answer: answer,
	}
	if runResult != nil && runResult.Interrupted {
		response.Interrupted = true
		response.CheckpointID = runResult.CheckpointID
		response.InterruptContexts = runResult.InterruptContexts
	}

	return response, nil
}

func (c *ControllerV1) buildLLMCacheKey(sessionID string, historyMsgs []*schema.Message, question string) *cache.LLMCacheKey {
	cacheMessages := make([]*schema.Message, 0, len(historyMsgs)+1)
	cacheMessages = append(cacheMessages, historyMsgs...)
	cacheMessages = append(cacheMessages, schema.UserMessage(question))

	return &cache.LLMCacheKey{
		AgentID:   c.rootAgentName,
		SessionID: sessionID,
		Messages:  cacheMessages,
		Model:     c.rootAgentName,
	}
}

func (c *ControllerV1) getCacheHitRate() float64 {
	hits := atomic.LoadInt64(&c.cacheHits)
	misses := atomic.LoadInt64(&c.cacheMisses)
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

type chatRunResult struct {
	Answer            string
	Interrupted       bool
	CheckpointID      string
	InterruptContexts []v1.InterruptContext
}

type interruptStateRecord struct {
	CheckpointID      string                `json:"checkpoint_id"`
	InterruptContexts []v1.InterruptContext `json:"interrupt_contexts"`
}

func (c *ControllerV1) collectChatResult(iterator *adk.AsyncIterator[*adk.AgentEvent], checkpointID string) (*chatRunResult, error) {
	result := &chatRunResult{
		CheckpointID: checkpointID,
	}
	if iterator == nil {
		return result, fmt.Errorf("agent iterator is nil")
	}

	var lastAssistant string
	var lastRootAgent string

	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			return nil, event.Err
		}

		if event.Action != nil && event.Action.Interrupted != nil {
			result.Interrupted = true
			result.InterruptContexts = convertInterruptContexts(event.Action.Interrupted.InterruptContexts)

			msg := buildInterruptMessage(event.Action.Interrupted.Data)
			if msg != "" {
				if event.AgentName == c.rootAgentName {
					lastRootAgent = msg
				} else {
					lastAssistant = msg
				}
			}
			continue
		}

		if event.Output == nil || event.Output.MessageOutput == nil || event.Output.MessageOutput.Message == nil {
			continue
		}
		msg := event.Output.MessageOutput.Message
		if msg.Role != schema.Assistant {
			continue
		}
		content := sanitizeUserFacingContent(msg.Content)
		if content == "" {
			continue
		}
		if event.AgentName == c.rootAgentName {
			lastRootAgent = content
		} else {
			lastAssistant = content
		}
	}

	if lastRootAgent != "" {
		result.Answer = lastRootAgent
	} else {
		result.Answer = lastAssistant
	}
	return result, nil
}

func (c *ControllerV1) generateCheckpointID(sessionID string) string {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		sid = "default-session"
	}
	return fmt.Sprintf("%s:%s", sid, uuid.NewString())
}

func convertInterruptContexts(contexts []*adk.InterruptCtx) []v1.InterruptContext {
	result := make([]v1.InterruptContext, 0, len(contexts))
	for _, item := range contexts {
		if item == nil {
			continue
		}
		result = append(result, v1.InterruptContext{
			ID:          item.ID,
			Address:     item.Address.String(),
			Info:        strings.TrimSpace(fmt.Sprintf("%v", item.Info)),
			IsRootCause: item.IsRootCause,
		})
	}
	return result
}

func (c *ControllerV1) resolveInterruptTargets(ctx context.Context, sessionID, checkpointID string, provided []string) ([]string, error) {
	normalized := normalizeIDList(provided)
	if len(normalized) > 0 {
		return normalized, nil
	}

	contexts, ok := c.loadInterruptState(ctx, sessionID, checkpointID)
	if !ok || len(contexts) == 0 {
		return nil, fmt.Errorf("interrupt ids are required: no saved interrupt context found for checkpoint %s", checkpointID)
	}

	root := make([]string, 0, len(contexts))
	all := make([]string, 0, len(contexts))
	for _, item := range contexts {
		if item.ID == "" {
			continue
		}
		all = append(all, item.ID)
		if item.IsRootCause {
			root = append(root, item.ID)
		}
	}

	if len(root) > 0 {
		return normalizeIDList(root), nil
	}
	return normalizeIDList(all), nil
}

func normalizeIDList(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	uniq := make(map[string]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := uniq[id]; exists {
			continue
		}
		uniq[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func (c *ControllerV1) resumeAgent(
	ctx context.Context,
	checkpointID string,
	targetIDs []string,
	approved *bool,
	resolved *bool,
	comment string,
	streaming bool,
) (*adk.AsyncIterator[*adk.AgentEvent], error) {
	targetIDs = normalizeIDList(targetIDs)
	if len(targetIDs) == 0 {
		return nil, fmt.Errorf("interrupt ids cannot be empty when resuming")
	}

	payload := map[string]any{}
	if approved != nil {
		payload["approved"] = *approved
	}
	if resolved != nil {
		payload["resolved"] = *resolved
	}
	if strings.TrimSpace(comment) != "" {
		payload["comment"] = strings.TrimSpace(comment)
	}
	if len(payload) == 0 {
		payload["comment"] = "继续执行"
	}

	targets := make(map[string]any, len(targetIDs))
	for _, id := range targetIDs {
		targets[id] = payload
	}

	var runner *adk.Runner
	if streaming {
		runner = c.chatStreamRunner
	} else {
		runner = c.chatRunner
	}
	if runner == nil {
		return nil, fmt.Errorf("chat runner is not initialized")
	}

	return runner.ResumeWithParams(ctx, checkpointID, &adk.ResumeParams{Targets: targets})
}

func (c *ControllerV1) saveInterruptState(ctx context.Context, sessionID, checkpointID string, contexts []v1.InterruptContext) {
	if len(contexts) == 0 {
		return
	}

	record := interruptStateRecord{
		CheckpointID:      checkpointID,
		InterruptContexts: contexts,
	}
	key := c.interruptStateKey(sessionID, checkpointID)

	if c.redisClient != nil {
		data, err := json.Marshal(record)
		if err == nil {
			if err = c.redisClient.Set(ctx, key, data, c.checkpointTTL).Err(); err == nil {
				return
			}
		}
		if c.logger != nil {
			c.logger.Warn("failed to save interrupt state to redis", zap.String("key", key), zap.Error(err))
		}
	}

	c.localInterruptState.Store(key, contexts)
}

func (c *ControllerV1) loadInterruptState(ctx context.Context, sessionID, checkpointID string) ([]v1.InterruptContext, bool) {
	key := c.interruptStateKey(sessionID, checkpointID)

	if c.redisClient != nil {
		data, err := c.redisClient.Get(ctx, key).Bytes()
		if err == nil {
			record := interruptStateRecord{}
			if unmarshalErr := json.Unmarshal(data, &record); unmarshalErr == nil {
				return record.InterruptContexts, len(record.InterruptContexts) > 0
			}
		}
	}

	if value, ok := c.localInterruptState.Load(key); ok {
		contexts, castOK := value.([]v1.InterruptContext)
		if castOK {
			return contexts, len(contexts) > 0
		}
	}

	return nil, false
}

func (c *ControllerV1) interruptStateKey(sessionID, checkpointID string) string {
	return fmt.Sprintf("oncall:interrupt:%s:%s", sessionID, checkpointID)
}

type inMemoryCheckPointStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newInMemoryCheckPointStore() compose.CheckPointStore {
	return &inMemoryCheckPointStore{data: make(map[string][]byte)}
}

func (s *inMemoryCheckPointStore) Get(_ context.Context, checkPointID string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.data[checkPointID]
	if !ok {
		return nil, false, nil
	}
	copied := make([]byte, len(data))
	copy(copied, data)
	return copied, true, nil
}

func (s *inMemoryCheckPointStore) Set(_ context.Context, checkPointID string, checkPoint []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := make([]byte, len(checkPoint))
	copy(copied, checkPoint)
	s.data[checkPointID] = copied
	return nil
}

func (c *ControllerV1) ChatStream(ctx context.Context, req *v1.ChatStreamReq) (res *v1.ChatStreamRes, err error) {
	// 参数验证
	if req.Question == "" {
		return nil, fmt.Errorf("question is required")
	}
	if req.Id == "" {
		req.Id = "default-session"
	}

	c.logger.Info("chat stream request received",
		zap.String("session_id", req.Id),
		zap.String("question", req.Question))

	// 获取 GoFrame 请求对象
	r := g.RequestFromCtx(ctx)
	if r == nil {
		return nil, fmt.Errorf("failed to get request from context")
	}

	// 设置 SSE 响应头
	r.Response.Header().Set("Content-Type", "text/event-stream")
	r.Response.Header().Set("Cache-Control", "no-cache")
	r.Response.Header().Set("Connection", "keep-alive")
	r.Response.Header().Set("X-Accel-Buffering", "no")

	// 立即写入响应头，确保连接建立
	r.Response.WriteHeader(200)
	r.Response.Flush()

	// 获取会话历史
	memory := mem.GetSimpleMemory(req.Id)
	historyMsgs, err := mem.GetMessagesForRequest(ctx, req.Id, schema.UserMessage(req.Question), 5)
	if err != nil {
		c.logger.Error("failed to get history messages",
			zap.String("session_id", req.Id),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get history: %w", err)
	}

	// 构建输入消息
	input := &adk.AgentInput{
		Messages:        historyMsgs,
		EnableStreaming: true,
	}

	// 流式执行 chat agent
	var fullAnswer strings.Builder

	// 使用独立的 goroutine 执行 agent，避免阻塞
	resultChan := make(chan struct {
		event   *adk.AgentEvent
		hasNext bool
	}, 10) // 缓冲通道，避免阻塞

	checkpointID := c.generateCheckpointID(req.Id)
	go func() {
		defer close(resultChan)
		if c.chatStreamRunner == nil {
			resultChan <- struct {
				event   *adk.AgentEvent
				hasNext bool
			}{
				event:   &adk.AgentEvent{Err: fmt.Errorf("chat stream runner is not initialized")},
				hasNext: true,
			}
			return
		}
		iter := c.chatStreamRunner.Run(ctx, input.Messages, adk.WithCheckPointID(checkpointID))
		for {
			event, hasNext := iter.Next()
			resultChan <- struct {
				event   *adk.AgentEvent
				hasNext bool
			}{event, hasNext}
			if !hasNext {
				break
			}
		}
	}()

	for result := range resultChan {
		if !result.hasNext {
			break
		}

		event := result.event
		if event == nil {
			continue
		}

		// 检查错误
		if event.Err != nil {
			c.logger.Error("chat agent stream error",
				zap.String("session_id", req.Id),
				zap.Error(event.Err))

			// 发送错误信息
			errorData := fmt.Sprintf("data: [ERROR] %s\n\n", event.Err.Error())
			r.Response.Write(errorData)
			r.Response.Flush()
			return nil, nil
		}

		// 提取消息内容
		if event.Action != nil && event.Action.Interrupted != nil {
			contexts := convertInterruptContexts(event.Action.Interrupted.InterruptContexts)
			c.saveInterruptState(ctx, req.Id, checkpointID, contexts)

			interruptPayload := map[string]any{
				"type":               "interrupt",
				"checkpoint_id":      checkpointID,
				"interrupt_contexts": contexts,
				"message":            buildInterruptMessage(event.Action.Interrupted.Data),
			}
			payloadBytes, _ := json.Marshal(interruptPayload)

			interruptMsg := buildInterruptMessage(event.Action.Interrupted.Data)
			if interruptMsg != "" {
				fullAnswer.WriteString(interruptMsg)
				data := fmt.Sprintf("data: %s\n\n", string(payloadBytes))
				r.Response.Write(data)
				r.Response.Flush()
			}
			continue
		}

		if event.Output != nil && event.Output.MessageOutput != nil && event.Output.MessageOutput.Message != nil {
			msg := event.Output.MessageOutput.Message
			if msg.Role != schema.Assistant {
				continue
			}

			// 仅对外流式输出根 agent 的回答，避免子 agent 原始结果泄漏。
			if event.AgentName != "" && event.AgentName != c.rootAgentName {
				continue
			}

			chunk := sanitizeUserFacingContent(msg.Content)
			if chunk == "" {
				continue
			}

			fullAnswer.WriteString(chunk)

			// 发送 SSE 数据
			data := fmt.Sprintf("data: %s\n\n", chunk)
			r.Response.Write(data)
			r.Response.Flush()
		}
	}

	// 发送结束标记
	r.Response.Write("data: [DONE]\n\n")
	r.Response.Flush()

	// 保存对话历史
	answer := fullAnswer.String()
	if answer != "" {
		userMsg := schema.UserMessage(req.Question)
		assistantMsg := schema.AssistantMessage(answer, nil)

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
	}

	c.logger.Info("chat stream completed",
		zap.String("session_id", req.Id),
		zap.Int("answer_length", fullAnswer.Len()))

	return &v1.ChatStreamRes{}, nil
}

func (c *ControllerV1) ChatResume(ctx context.Context, req *v1.ChatResumeReq) (res *v1.ChatResumeRes, err error) {
	if req.CheckpointID == "" {
		return nil, fmt.Errorf("checkpoint_id is required")
	}
	if req.Id == "" {
		return nil, fmt.Errorf("id is required")
	}

	if req.SSE == nil || *req.SSE {
		_, err = c.ChatResumeStream(ctx, &v1.ChatResumeStreamReq{
			Id:           req.Id,
			CheckpointID: req.CheckpointID,
			InterruptIDs: req.InterruptIDs,
			Approved:     req.Approved,
			Resolved:     req.Resolved,
			Comment:      req.Comment,
		})
		if err != nil {
			return nil, err
		}
		return &v1.ChatResumeRes{}, nil
	}

	targetIDs, err := c.resolveInterruptTargets(ctx, req.Id, req.CheckpointID, req.InterruptIDs)
	if err != nil {
		return nil, err
	}

	iter, err := c.resumeAgent(ctx, req.CheckpointID, targetIDs, req.Approved, req.Resolved, req.Comment, false)
	if err != nil {
		return nil, err
	}

	result, err := c.collectChatResult(iter, req.CheckpointID)
	if err != nil {
		return nil, err
	}

	response := &v1.ChatResumeRes{
		Answer:       result.Answer,
		Interrupted:  result.Interrupted,
		CheckpointID: req.CheckpointID,
	}
	if response.Answer == "" {
		response.Answer = "恢复执行完成。"
	}
	if result.Interrupted {
		response.InterruptContexts = result.InterruptContexts
		c.saveInterruptState(ctx, req.Id, req.CheckpointID, result.InterruptContexts)
	}
	return response, nil
}

func (c *ControllerV1) ChatResumeStream(ctx context.Context, req *v1.ChatResumeStreamReq) (res *v1.ChatResumeStreamRes, err error) {
	if req.CheckpointID == "" {
		return nil, fmt.Errorf("checkpoint_id is required")
	}
	if req.Id == "" {
		return nil, fmt.Errorf("id is required")
	}

	targetIDs, err := c.resolveInterruptTargets(ctx, req.Id, req.CheckpointID, req.InterruptIDs)
	if err != nil {
		return nil, err
	}

	iter, err := c.resumeAgent(ctx, req.CheckpointID, targetIDs, req.Approved, req.Resolved, req.Comment, true)
	if err != nil {
		return nil, err
	}

	r := g.RequestFromCtx(ctx)
	if r == nil {
		return nil, fmt.Errorf("failed to get request from context")
	}

	r.Response.Header().Set("Content-Type", "text/event-stream")
	r.Response.Header().Set("Cache-Control", "no-cache")
	r.Response.Header().Set("Connection", "keep-alive")
	r.Response.Header().Set("X-Accel-Buffering", "no")
	r.Response.WriteHeader(200)
	r.Response.Flush()

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}

		if event.Err != nil {
			errorData := fmt.Sprintf("data: [ERROR] %s\n\n", event.Err.Error())
			r.Response.Write(errorData)
			r.Response.Flush()
			return nil, nil
		}

		if event.Action != nil && event.Action.Interrupted != nil {
			contexts := convertInterruptContexts(event.Action.Interrupted.InterruptContexts)
			c.saveInterruptState(ctx, req.Id, req.CheckpointID, contexts)

			payload := map[string]any{
				"type":               "interrupt",
				"checkpoint_id":      req.CheckpointID,
				"interrupt_contexts": contexts,
				"message":            buildInterruptMessage(event.Action.Interrupted.Data),
			}
			b, _ := json.Marshal(payload)
			r.Response.Write(fmt.Sprintf("data: %s\n\n", string(b)))
			r.Response.Flush()
			continue
		}

		if event.Output != nil && event.Output.MessageOutput != nil && event.Output.MessageOutput.Message != nil {
			msg := event.Output.MessageOutput.Message
			if msg.Role != schema.Assistant {
				continue
			}
			if event.AgentName != "" && event.AgentName != c.rootAgentName {
				continue
			}
			chunk := sanitizeUserFacingContent(msg.Content)
			if chunk == "" {
				continue
			}
			r.Response.Write(fmt.Sprintf("data: %s\n\n", chunk))
			r.Response.Flush()
		}
	}

	r.Response.Write("data: [DONE]\n\n")
	r.Response.Flush()
	return &v1.ChatResumeStreamRes{}, nil
}

func (c *ControllerV1) FileUpload(ctx context.Context, req *v1.FileUploadReq) (res *v1.FileUploadRes, err error) {
	// 从 GoFrame 请求中获取文件
	r := g.RequestFromCtx(ctx)
	if r == nil {
		return nil, fmt.Errorf("failed to get request from context")
	}

	file := r.GetUploadFile("file")
	if file == nil {
		return nil, fmt.Errorf("no file uploaded")
	}

	c.logger.Info("file upload started",
		zap.String("filename", file.Filename),
		zap.Int64("size", file.Size))

	// 验证文件类型
	allowedExts := []string{".txt", ".md", ".markdown"}
	ext := ""
	for _, allowed := range allowedExts {
		if len(file.Filename) > len(allowed) && file.Filename[len(file.Filename)-len(allowed):] == allowed {
			ext = allowed
			break
		}
	}
	if ext == "" {
		return nil, fmt.Errorf("unsupported file type, only .txt, .md, .markdown are allowed")
	}

	// 读取文件内容
	content, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer content.Close()

	fileBytes := make([]byte, file.Size)
	n, err := content.Read(fileBytes)
	if err != nil && err.Error() != "EOF" {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	fileContent := string(fileBytes[:n])

	c.logger.Info("file content read",
		zap.String("filename", file.Filename),
		zap.Int("bytes_read", n))

	// 直接使用 Milvus Indexer 进行索引，避免通过 Agent 的复杂流程
	if c.milvusIndexer == nil {
		return nil, fmt.Errorf("milvus indexer not available")
	}

	// 类型断言获取 indexer
	indexer, ok := c.milvusIndexer.(*milvusIndexer.Indexer)
	if !ok {
		c.logger.Error("invalid indexer type",
			zap.String("type", fmt.Sprintf("%T", c.milvusIndexer)))
		return nil, fmt.Errorf("invalid indexer type")
	}

	// 在向量化前优先按 Markdown 标题切分，再对超长分片做二次切块
	const chunkSize = 1000
	chunks := splitText(fileContent, chunkSize)
	if len(chunks) == 0 {
		return nil, fmt.Errorf("file content is empty after splitting")
	}

	uploadAt := time.Now()
	docs := make([]*schema.Document, 0, len(chunks))
	for i, chunkContent := range chunks {
		chunkNum := i + 1
		doc := &schema.Document{
			ID:      fmt.Sprintf("doc_%d_%s_chunk_%d", uploadAt.Unix(), file.Filename, chunkNum),
			Content: chunkContent,
			MetaData: map[string]interface{}{
				"filename":     file.Filename,
				"upload_time":  uploadAt.Format(time.RFC3339),
				"chunk":        chunkNum,
				"total_chunks": len(chunks),
				"size":         len(chunkContent),
				"split_type":   "markdown_heading",
			},
		}
		docs = append(docs, doc)
	}

	c.logger.Info("document split into chunks",
		zap.String("filename", file.Filename),
		zap.Int("total_chunks", len(docs)))

	// 批量索引到 Milvus
	ids, err := indexer.Store(ctx, docs)
	if err != nil {
		c.logger.Error("failed to index document",
			zap.Error(err),
			zap.String("filename", file.Filename))
		return nil, fmt.Errorf("failed to index document: %w", err)
	}

	c.logger.Info("file uploaded and indexed successfully",
		zap.String("filename", file.Filename),
		zap.Int64("size", file.Size),
		zap.Strings("doc_ids", ids))

	return &v1.FileUploadRes{
		FileName: file.Filename,
		FilePath: fmt.Sprintf("/knowledge/%s", file.Filename),
		FileSize: file.Size,
	}, nil
}

// splitText 在向量化前执行文档分片：
// 1. 优先按 Markdown 一级标题（# 开头）分段
// 2. 每段若过长，再按固定长度切块
func splitText(content string, maxChunkSize int) []string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}
	if maxChunkSize <= 0 {
		maxChunkSize = 1000
	}

	// 按行扫描，遇到 "# " 开头的新章节就切分
	lines := strings.Split(trimmed, "\n")
	sections := make([]string, 0)
	current := make([]string, 0)
	foundHeading := false

	for _, line := range lines {
		lineTrimmed := strings.TrimSpace(line)
		isHeading := strings.HasPrefix(lineTrimmed, "#")
		if isHeading {
			if len(current) > 0 {
				sections = append(sections, strings.Join(current, "\n"))
				current = current[:0]
			}
			foundHeading = true
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		sections = append(sections, strings.Join(current, "\n"))
	}

	// 没有标题时，整体作为一个 section 走后续二次切块
	if !foundHeading {
		sections = []string{trimmed}
	}

	result := make([]string, 0)
	for _, sec := range sections {
		sec = strings.TrimSpace(sec)
		if sec == "" {
			continue
		}

		runes := []rune(sec)
		if len(runes) <= maxChunkSize {
			result = append(result, sec)
			continue
		}

		for i := 0; i < len(runes); i += maxChunkSize {
			end := i + maxChunkSize
			if end > len(runes) {
				end = len(runes)
			}
			part := strings.TrimSpace(string(runes[i:end]))
			if part != "" {
				result = append(result, part)
			}
		}
	}

	return result
}

func sanitizeUserFacingContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}

	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "successfully transferred to agent") {
		return ""
	}

	// 过滤工具原始 JSON 结果，避免直接回显给用户。
	if strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, "\"status\"") && strings.Contains(trimmed, "\"results\"") && strings.Contains(trimmed, "\"count\"") {
		return ""
	}

	return trimmed
}

func buildInterruptMessage(data any) string {
	base := "流程已暂停，等待你的确认。请回复“确认已修复”或“继续重试”。"
	raw := extractInterruptDetail(data)
	if raw == "" {
		return base
	}

	if len([]rune(raw)) > 400 {
		raw = string([]rune(raw)[:400]) + "..."
	}

	return fmt.Sprintf("%s\n中断信息：%s", base, raw)
}

func extractInterruptDetail(data any) string {
	if data == nil {
		return ""
	}

	switch value := data.(type) {
	case *adk.ChatModelAgentInterruptInfo:
		if value.Info != nil {
			if detail := extractInterruptContextsDetail(value.Info.InterruptContexts); detail != "" {
				return detail
			}
		}
		return ""
	case []byte:
		return ""
	case map[string]any:
		if reason, ok := value["reason"].(string); ok && strings.TrimSpace(reason) != "" {
			return strings.TrimSpace(reason)
		}
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
}

func extractInterruptContextsDetail(contexts []*adk.InterruptCtx) string {
	for i := len(contexts) - 1; i >= 0; i-- {
		ctx := contexts[i]
		if ctx == nil || ctx.Info == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprintf("%v", ctx.Info))
		if text != "" {
			return text
		}
	}
	return ""
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
		CacheHitRate:    c.getCacheHitRate(),
		CacheHits:       hits,
		CacheMisses:     misses,
		CircuitBreakers: breakers,
	}, nil
}

func (c *ControllerV1) AIOps(ctx context.Context, req *v1.AIOpsReq) (res *v1.AIOpsRes, err error) {
	if c.opsAgent == nil {
		return &v1.AIOpsRes{
			Result: "AI Ops agent is unavailable",
			Detail: []string{"ops agent not initialized in bootstrap"},
		}, nil
	}

	c.logger.Info("AI Ops request received")

	// 获取 GoFrame 请求对象
	r := g.RequestFromCtx(ctx)
	if r == nil {
		return nil, fmt.Errorf("failed to get request from context")
	}

	// 设置 SSE 响应头
	r.Response.Header().Set("Content-Type", "text/event-stream")
	r.Response.Header().Set("Cache-Control", "no-cache")
	r.Response.Header().Set("Connection", "keep-alive")
	r.Response.Header().Set("X-Accel-Buffering", "no")

	// 调用 Plan-Execute-Replan Ops Agent（启用流式）
	iter := c.opsAgent.Run(ctx, &adk.AgentInput{
		Messages: []adk.Message{
			{
				Role:    schema.User,
				Content: "请执行系统健康检查，分析当前系统状态，识别潜在问题并给出分步骤的诊断和解决方案。重点关注：1) Kubernetes Pod状态 2) 关键指标异常 3) 错误日志",
			},
		},
		EnableStreaming: true,
	})

	// 流式输出事件和步骤
	var fullAnswer strings.Builder
	stepNum := 1

	for {
		event, hasNext := iter.Next()
		if !hasNext {
			break
		}

		if event == nil {
			continue
		}

		// 检查是否有错误
		if event.Err != nil {
			c.logger.Error("ops agent event error", zap.Error(event.Err))
			errorData := fmt.Sprintf("data: {\"type\":\"error\",\"content\":\"执行失败: %s\"}\n\n", event.Err.Error())
			r.Response.Write(errorData)
			r.Response.Flush()
			return nil, nil
		}

		// 提取消息内容和工具调用
		if event.Output != nil && event.Output.MessageOutput != nil && event.Output.MessageOutput.Message != nil {
			msg := event.Output.MessageOutput.Message

			// 发送工具调用信息
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					stepData := fmt.Sprintf("data: {\"type\":\"step\",\"step\":%d,\"content\":\"调用工具: %s\"}\n\n", stepNum, tc.Function.Name)
					r.Response.Write(stepData)
					r.Response.Flush()
					stepNum++
				}
			}

			// 发送 AI 的分析内容
			if msg.Content != "" {
				fullAnswer.WriteString(msg.Content)

				// 流式发送内容
				contentData := fmt.Sprintf("data: {\"type\":\"content\",\"content\":%q}\n\n", msg.Content)
				r.Response.Write(contentData)
				r.Response.Flush()
			}
		}

		// 发送 Action 信息（plan-execute-replan 的计划步骤）
		if event.Action != nil {
			actionData := fmt.Sprintf("data: {\"type\":\"step\",\"step\":%d,\"content\":\"执行动作: %s\"}\n\n", stepNum, event.AgentName)
			r.Response.Write(actionData)
			r.Response.Flush()
			stepNum++
		}
	}

	// 发送结束标记
	r.Response.Write("data: {\"type\":\"done\"}\n\n")
	r.Response.Flush()

	c.logger.Info("AI Ops completed",
		zap.Int("total_steps", stepNum-1),
		zap.Int("answer_length", fullAnswer.Len()))

	return &v1.AIOpsRes{}, nil
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

// isGreetingOrSimpleQuery 检测是否是简单问候或不需要历史上下文的查询
func isGreetingOrSimpleQuery(question string) bool {
	q := strings.TrimSpace(strings.ToLower(question))

	// 简单问候
	greetings := []string{
		"你好", "您好", "hi", "hello", "hey",
		"早上好", "下午好", "晚上好",
		"在吗", "在不在",
	}

	for _, greeting := range greetings {
		if q == greeting || q == greeting+"!" || q == greeting+"？" || q == greeting+"?" {
			return true
		}
	}

	// 非常短的查询（少于 5 个字符）通常不需要历史上下文
	if len([]rune(q)) <= 5 && !strings.Contains(q, "为什么") && !strings.Contains(q, "怎么") {
		return true
	}

	return false
}
