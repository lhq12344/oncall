package chat

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	v1 "go_agent/api/chat/v1"
	"go_agent/internal/agent/ops"
	"go_agent/internal/cache"
	"go_agent/internal/concurrent"
	"go_agent/internal/healing"
	"go_agent/utility/mem"
	"go_agent/utility/tokenizer"

	milvusIndexer "github.com/cloudwego/eino-ext/components/indexer/milvus"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
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
	opsAgent       adk.Agent   // Plan-Execute-Replan Ops Agent
	milvusIndexer  interface{} // Milvus Indexer for direct document indexing
	healingManager *healing.HealingLoopManager

	cacheHits   int64
	cacheMisses int64
}

func NewV1(
	supervisorAgent adk.ResumableAgent,
	logger *zap.Logger,
	redisClient *redis.Client,
	opsExecutor *ops.IntegratedOpsExecutor,
	opsAgent adk.Agent,
	milvusIndexer interface{},
	healingManager *healing.HealingLoopManager,
) *ControllerV1 {
	ctrl := &ControllerV1{
		supervisorAgent: supervisorAgent,
		logger:          logger,
		opsExecutor:     opsExecutor,
		opsAgent:        opsAgent,
		milvusIndexer:   milvusIndexer,
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
		zap.Float64("cache_hit_rate", c.getCacheHitRate()))

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

func (c *ControllerV1) getCacheHitRate() float64 {
	hits := atomic.LoadInt64(&c.cacheHits)
	misses := atomic.LoadInt64(&c.cacheMisses)
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
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

	// 流式执行 supervisor agent
	var fullAnswer strings.Builder
	iter := c.supervisorAgent.Run(ctx, input)

	for {
		event, hasNext := iter.Next()
		if !hasNext {
			break
		}

		if event == nil {
			continue
		}

		// 检查错误
		if event.Err != nil {
			c.logger.Error("supervisor agent stream error",
				zap.String("session_id", req.Id),
				zap.Error(event.Err))

			// 发送错误信息
			errorData := fmt.Sprintf("data: [ERROR] %s\n\n", event.Err.Error())
			r.Response.Write(errorData)
			r.Response.Flush()
			return nil, nil
		}

		// 提取消息内容
		if event.Output != nil && event.Output.MessageOutput != nil {
			if event.Output.MessageOutput.Message != nil {
				chunk := event.Output.MessageOutput.Message.Content
				if chunk != "" {
					fullAnswer.WriteString(chunk)

					// 发送 SSE 数据
					data := fmt.Sprintf("data: %s\n\n", chunk)
					r.Response.Write(data)
					r.Response.Flush()
				}
			}
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
