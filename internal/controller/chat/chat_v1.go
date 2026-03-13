package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	v1 "go_agent/api/chat/v1"
	appcontext "go_agent/internal/context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	defaultSessionID    = "default-session"
	opsDiagnosticPrompt = "请执行系统健康检查，分析当前系统状态，识别潜在问题并给出分步骤的诊断和解决方案。重点关注：1) Kubernetes Pod状态 2) 关键指标异常 3) 错误日志。命名空间检查要求：必须优先检查 infra 命名空间；若需要全局对比，再补充 default/staging/production/kube-system。"
)

type ControllerV1 struct {
	dialogueAgent    adk.ResumableAgent
	chatStreamRunner *adk.Runner
	opsStreamRunner  *adk.Runner
	rootAgentName    string
	opsRootAgentName string
	sessionMemory    *appcontext.SessionMemory
	logger           *zap.Logger
	opsAgent         adk.Agent
	knowledgeAgent   adk.Agent
}

func NewV1(
	dialogueAgent adk.ResumableAgent,
	logger *zap.Logger,
	redisClient *redis.Client,
	opsAgent adk.Agent,
	knowledgeAgent adk.Agent,
) *ControllerV1 {
	ctrl := &ControllerV1{
		dialogueAgent:    dialogueAgent,
		rootAgentName:    "dialogue_agent",
		opsRootAgentName: "ops_agent",
		sessionMemory:    appcontext.NewSessionMemory(nil, logger),
		logger:           logger,
		opsAgent:         opsAgent,
		knowledgeAgent:   knowledgeAgent,
	}

	var checkpointStore compose.CheckPointStore
	if redisClient != nil {
		checkpointStore = appcontext.NewRedisCheckPointStore(redisClient, "oncall", 24*time.Hour)
	} else {
		checkpointStore = newInMemoryCheckPointStore()
	}

	if dialogueAgent != nil {
		if agentName := strings.TrimSpace(dialogueAgent.Name(context.Background())); agentName != "" {
			ctrl.rootAgentName = agentName
		}
		ctrl.chatStreamRunner = adk.NewRunner(context.Background(), adk.RunnerConfig{
			Agent:           dialogueAgent,
			EnableStreaming: true,
			CheckPointStore: checkpointStore,
		})
	}

	if opsAgent != nil {
		if agentName := strings.TrimSpace(opsAgent.Name(context.Background())); agentName != "" {
			ctrl.opsRootAgentName = agentName
		}
		ctrl.opsStreamRunner = adk.NewRunner(context.Background(), adk.RunnerConfig{
			Agent:           opsAgent,
			EnableStreaming: true,
			CheckPointStore: checkpointStore,
		})
	}

	return ctrl
}

func (c *ControllerV1) ChatStream(ctx context.Context, req *v1.ChatStreamReq) (res *v1.ChatStreamRes, err error) {
	question, sessionID, err := validateChatStreamInput(req)
	if err != nil {
		return nil, err
	}
	if c.chatStreamRunner == nil {
		return nil, fmt.Errorf("chat stream runner is not initialized")
	}

	r, err := setupSSE(ctx)
	if err != nil {
		return nil, err
	}

	messages, err := c.sessionMemory.BuildMessages(ctx, sessionID, question)
	if err != nil {
		return nil, err
	}

	checkpointID := generateCheckpointID(sessionID)
	if c.logger != nil {
		c.logger.Info("chat_stream request received",
			zap.String("session_id", sessionID),
			zap.Int("question_len", len([]rune(question))),
			zap.String("checkpoint_id", checkpointID))
	}
	iter := c.chatStreamRunner.Run(ctx, messages,
		adk.WithCheckPointID(checkpointID),
		adk.WithSessionValues(map[string]any{
			"session_id": sessionID,
		}),
	)

	var fullAnswer strings.Builder
	interrupted := false
	eventCount := 0
	contentChunkCount := 0
	lastEventAgent := ""
	lastEventRole := ""
	lastEventContentLen := 0
	lastEventToolCalls := 0

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		eventCount++
		if event.Err != nil {
			writeSSEData(r, "[ERROR] "+event.Err.Error())
			return nil, nil
		}
		msg, hasMsg := c.resolveEventMessage(event)
		if hasMsg && msg != nil {
			lastEventAgent = event.AgentName
			lastEventRole = string(msg.Role)
			lastEventContentLen = len([]rune(strings.TrimSpace(msg.Content)))
			lastEventToolCalls = len(msg.ToolCalls)
		}

		if event.Action != nil && event.Action.Interrupted != nil {
			interrupted = true
			payload := map[string]any{
				"type":               "interrupt",
				"checkpoint_id":      checkpointID,
				"interrupt_contexts": convertInterruptContexts(event.Action.Interrupted.InterruptContexts),
				"message":            buildInterruptMessage(event.Action.Interrupted.Data),
			}
			payloadBytes, _ := json.Marshal(payload)
			writeSSEData(r, string(payloadBytes))
			continue
		}

		chunk, ok := c.extractAssistantContentFromResolved(event, msg)
		if !ok {
			continue
		}
		fullAnswer.WriteString(chunk)
		contentChunkCount++
		writeSSEData(r, chunk)
	}

	writeSSEData(r, "[DONE]")

	answer := strings.TrimSpace(fullAnswer.String())
	if c.logger != nil {
		c.logger.Info("chat_stream completed",
			zap.String("session_id", sessionID),
			zap.Bool("interrupted", interrupted),
			zap.Int("event_count", eventCount),
			zap.Int("content_chunks", contentChunkCount),
			zap.Int("answer_len", len([]rune(answer))))
		if !interrupted && strings.TrimSpace(answer) == "" {
			c.logger.Warn("chat_stream no displayable assistant content",
				zap.String("session_id", sessionID),
				zap.String("last_event_agent", strings.TrimSpace(lastEventAgent)),
				zap.String("last_event_role", strings.TrimSpace(lastEventRole)),
				zap.Int("last_event_content_len", lastEventContentLen),
				zap.Int("last_event_tool_calls", lastEventToolCalls))
		}
	}
	if answer != "" && !interrupted {
		c.sessionMemory.SaveTurn(context.Background(), sessionID, question, answer, messages)
	}

	return &v1.ChatStreamRes{}, nil
}

func (c *ControllerV1) ChatResumeStream(ctx context.Context, req *v1.ChatResumeStreamReq) (res *v1.ChatResumeStreamRes, err error) {
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}
	if strings.TrimSpace(req.CheckpointID) == "" {
		return nil, fmt.Errorf("checkpoint_id is required")
	}
	if strings.TrimSpace(req.Id) == "" {
		return nil, fmt.Errorf("id is required")
	}

	iter, err := c.resumeAgent(ctx, c.chatStreamRunner, req.CheckpointID, req.InterruptIDs, req.Approved, req.Resolved, req.Comment, map[string]any{
		"session_id": normalizeSessionID(req.Id),
	})
	if err != nil {
		return nil, err
	}

	r, err := setupSSE(ctx)
	if err != nil {
		return nil, err
	}

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			writeSSEData(r, "[ERROR] "+event.Err.Error())
			return nil, nil
		}

		if event.Action != nil && event.Action.Interrupted != nil {
			payload := map[string]any{
				"type":               "interrupt",
				"checkpoint_id":      req.CheckpointID,
				"interrupt_contexts": convertInterruptContexts(event.Action.Interrupted.InterruptContexts),
				"message":            buildInterruptMessage(event.Action.Interrupted.Data),
			}
			b, _ := json.Marshal(payload)
			writeSSEData(r, string(b))
			continue
		}

		chunk, ok := c.extractAssistantContent(event)
		if !ok {
			continue
		}
		writeSSEData(r, chunk)
	}

	writeSSEData(r, "[DONE]")
	return &v1.ChatResumeStreamRes{}, nil
}

func (c *ControllerV1) FileUpload(ctx context.Context, req *v1.FileUploadReq) (res *v1.FileUploadRes, err error) {
	r := g.RequestFromCtx(ctx)
	if r == nil {
		return nil, fmt.Errorf("failed to get request from context")
	}

	file := r.GetUploadFile("file")
	if file == nil {
		return nil, fmt.Errorf("no file uploaded")
	}
	if c.knowledgeAgent == nil {
		return nil, fmt.Errorf("knowledge upload agent not available")
	}

	if !isAllowedUploadFile(file.Filename) {
		return nil, fmt.Errorf("unsupported file type, only .txt, .md, .markdown are allowed")
	}

	content, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer content.Close()

	body, err := io.ReadAll(content)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	uploadPayload := map[string]any{
		"title":   file.Filename,
		"content": string(body),
		"meta": map[string]any{
			"filename":    file.Filename,
			"upload_time": time.Now().Format(time.RFC3339),
			"size":        file.Size,
		},
	}
	payloadBytes, err := json.Marshal(uploadPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal upload payload: %w", err)
	}

	iter := c.knowledgeAgent.Run(ctx, &adk.AgentInput{
		Messages: []adk.Message{
			{
				Role:    schema.User,
				Content: string(payloadBytes),
			},
		},
		EnableStreaming: false,
	})

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			return nil, fmt.Errorf("knowledge upload failed: %w", event.Err)
		}
	}

	return &v1.FileUploadRes{
		FileName: file.Filename,
		FilePath: fmt.Sprintf("/knowledge/%s", file.Filename),
		FileSize: file.Size,
	}, nil
}

func (c *ControllerV1) AIOpsStream(ctx context.Context, req *v1.AIOpsStreamReq) (res *v1.AIOpsStreamRes, err error) {
	if c.opsAgent == nil {
		return nil, fmt.Errorf("ops agent not initialized in bootstrap")
	}
	if c.opsStreamRunner == nil {
		return nil, fmt.Errorf("ops stream runner is not initialized")
	}

	r, err := setupSSE(ctx)
	if err != nil {
		return nil, err
	}

	checkpointID := generateCheckpointID("aiops")
	iter := c.opsStreamRunner.Run(ctx, []adk.Message{
		schema.UserMessage(opsDiagnosticPrompt),
	}, adk.WithCheckPointID(checkpointID), adk.WithSessionValues(map[string]any{
		"session_id": "aiops",
	}))

	stepNum := 1
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			writeSSEData(r, fmt.Sprintf("{\"type\":\"error\",\"content\":%q}", event.Err.Error()))
			return nil, nil
		}

		if event.Action != nil && event.Action.Interrupted != nil {
			payload := map[string]any{
				"type":               "interrupt",
				"checkpoint_id":      checkpointID,
				"interrupt_contexts": convertInterruptContexts(event.Action.Interrupted.InterruptContexts),
				"message":            buildInterruptMessage(event.Action.Interrupted.Data),
			}
			payloadBytes, _ := json.Marshal(payload)
			writeSSEData(r, string(payloadBytes))
			continue
		}

		msg, hasMsg := c.resolveEventMessage(event)
		if hasMsg && msg != nil {
			for _, call := range msg.ToolCalls {
				writeSSEData(r, fmt.Sprintf("{\"type\":\"step\",\"step\":%d,\"content\":%q}", stepNum, "调用工具: "+call.Function.Name))
				stepNum++
			}

			if content, ok := c.extractAgentContentByMessage(event.AgentName, msg, ""); ok {
				content = formatAIOpsContent(event.AgentName, c.opsRootAgentName, content)
				if strings.TrimSpace(content) != "" {
					writeSSEData(r, fmt.Sprintf("{\"type\":\"content\",\"content\":%q}", content))
				}
			}
		}
	}

	writeSSEData(r, "{\"type\":\"done\"}")
	return &v1.AIOpsStreamRes{}, nil
}

func (c *ControllerV1) AIOpsResumeStream(ctx context.Context, req *v1.AIOpsResumeStreamReq) (res *v1.AIOpsResumeStreamRes, err error) {
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}
	if strings.TrimSpace(req.CheckpointID) == "" {
		return nil, fmt.Errorf("checkpoint_id is required")
	}

	iter, err := c.resumeAgent(ctx, c.opsStreamRunner, req.CheckpointID, req.InterruptIDs, req.Approved, req.Resolved, req.Comment, map[string]any{
		"session_id": "aiops",
	})
	if err != nil {
		return nil, err
	}

	r, err := setupSSE(ctx)
	if err != nil {
		return nil, err
	}

	stepNum := 1
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			writeSSEData(r, fmt.Sprintf("{\"type\":\"error\",\"content\":%q}", event.Err.Error()))
			return nil, nil
		}

		if event.Action != nil && event.Action.Interrupted != nil {
			payload := map[string]any{
				"type":               "interrupt",
				"checkpoint_id":      req.CheckpointID,
				"interrupt_contexts": convertInterruptContexts(event.Action.Interrupted.InterruptContexts),
				"message":            buildInterruptMessage(event.Action.Interrupted.Data),
			}
			payloadBytes, _ := json.Marshal(payload)
			writeSSEData(r, string(payloadBytes))
			continue
		}

		msg, hasMsg := c.resolveEventMessage(event)
		if hasMsg && msg != nil {
			for _, call := range msg.ToolCalls {
				writeSSEData(r, fmt.Sprintf("{\"type\":\"step\",\"step\":%d,\"content\":%q}", stepNum, "调用工具: "+call.Function.Name))
				stepNum++
			}

			if content, ok := c.extractAgentContentByMessage(event.AgentName, msg, ""); ok {
				content = formatAIOpsContent(event.AgentName, c.opsRootAgentName, content)
				if strings.TrimSpace(content) != "" {
					writeSSEData(r, fmt.Sprintf("{\"type\":\"content\",\"content\":%q}", content))
				}
			}
		}
	}

	writeSSEData(r, "{\"type\":\"done\"}")
	return &v1.AIOpsResumeStreamRes{}, nil
}

func (c *ControllerV1) Monitoring(ctx context.Context, req *v1.MonitoringReq) (res *v1.MonitoringRes, err error) {
	return &v1.MonitoringRes{
		CacheHitRate:    0,
		CacheHits:       0,
		CacheMisses:     0,
		CircuitBreakers: []v1.CircuitBreakerStatus{},
	}, nil
}

func (c *ControllerV1) resumeAgent(
	ctx context.Context,
	runner *adk.Runner,
	checkpointID string,
	interruptIDs []string,
	approved *bool,
	resolved *bool,
	comment string,
	sessionValues map[string]any,
) (*adk.AsyncIterator[*adk.AgentEvent], error) {
	if runner == nil {
		return nil, fmt.Errorf("runner is not initialized")
	}

	targetIDs := normalizeIDList(interruptIDs)
	baseOpts := make([]adk.AgentRunOption, 0, 1)
	if len(sessionValues) > 0 {
		baseOpts = append(baseOpts, adk.WithSessionValues(sessionValues))
	}
	if len(targetIDs) == 0 {
		return runner.Resume(ctx, checkpointID, baseOpts...)
	}

	targetPayload := map[string]any{}
	if approved != nil {
		targetPayload["approved"] = *approved
	}
	if resolved != nil {
		targetPayload["resolved"] = *resolved
	}
	if text := strings.TrimSpace(comment); text != "" {
		targetPayload["comment"] = text
	}
	if len(targetPayload) == 0 {
		targetPayload["comment"] = "继续执行"
	}

	targets := make(map[string]any, len(targetIDs))
	for _, id := range targetIDs {
		targets[id] = targetPayload
	}

	return runner.ResumeWithParams(ctx, checkpointID, &adk.ResumeParams{Targets: targets}, baseOpts...)
}

func (c *ControllerV1) extractAssistantContent(event *adk.AgentEvent) (string, bool) {
	msg, ok := c.resolveEventMessage(event)
	if !ok {
		return "", false
	}
	return c.extractAssistantContentFromResolved(event, msg)
}

func (c *ControllerV1) extractAssistantContentFromResolved(event *adk.AgentEvent, msg *schema.Message) (string, bool) {
	if event == nil || msg == nil {
		return "", false
	}
	content, ok := c.extractAgentContentByMessage(event.AgentName, msg, c.rootAgentName)
	if ok {
		return content, true
	}
	// 对话流放宽一次：若 AgentName 与 rootName 不一致，仍允许非工具 assistant 消息透出。
	return c.extractAgentContentByMessage(event.AgentName, msg, "")
}

func (c *ControllerV1) resolveEventMessage(event *adk.AgentEvent) (*schema.Message, bool) {
	if event == nil || event.Output == nil || event.Output.MessageOutput == nil {
		return nil, false
	}
	variant := event.Output.MessageOutput
	if variant.Message != nil {
		return variant.Message, true
	}
	if variant.MessageStream == nil {
		return nil, false
	}

	msg, err := variant.GetMessage()
	if err != nil {
		if c.logger != nil {
			c.logger.Warn("failed to resolve event message stream",
				zap.String("agent_name", event.AgentName),
				zap.Error(err))
		}
		return nil, false
	}
	if msg == nil {
		return nil, false
	}
	return msg, true
}

func (c *ControllerV1) extractAgentContent(event *adk.AgentEvent, rootName string) (string, bool) {
	msg, ok := c.resolveEventMessage(event)
	if !ok {
		return "", false
	}
	return c.extractAgentContentByMessage(event.AgentName, msg, rootName)
}

func (c *ControllerV1) extractAgentContentByMessage(agentName string, msg *schema.Message, rootName string) (string, bool) {
	if msg == nil {
		return "", false
	}
	if msg.Role != schema.Assistant {
		return "", false
	}
	if rootName = strings.TrimSpace(rootName); rootName != "" {
		if agentName != "" && agentName != rootName {
			return "", false
		}
	}
	if strings.TrimSpace(msg.ToolCallID) != "" {
		return "", false
	}
	content := sanitizeUserFacingContent(msg.Content)
	if content == "" {
		return "", false
	}
	return content, true
}

func setupSSE(ctx context.Context) (*ghttp.Request, error) {
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
	return r, nil
}

func writeSSEData(r *ghttp.Request, data string) {
	if r == nil {
		return
	}
	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\r", "\n")
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		r.Response.Write(fmt.Sprintf("data: %s\n", line))
	}
	r.Response.Write("\n")
	r.Response.Flush()
}

func validateChatStreamInput(req *v1.ChatStreamReq) (question string, sessionID string, err error) {
	if req == nil {
		return "", "", fmt.Errorf("request is required")
	}
	question = strings.TrimSpace(req.Question)
	if question == "" {
		return "", "", fmt.Errorf("question is required")
	}
	sessionID = normalizeSessionID(req.Id)
	return question, sessionID, nil
}

func normalizeSessionID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return defaultSessionID
	}
	return id
}

func generateCheckpointID(sessionID string) string {
	return fmt.Sprintf("%s:%s", normalizeSessionID(sessionID), uuid.NewString())
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

func normalizeIDList(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	uniq := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := uniq[id]; exists {
			continue
		}
		uniq[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func isAllowedUploadFile(fileName string) bool {
	fileName = strings.ToLower(strings.TrimSpace(fileName))
	return strings.HasSuffix(fileName, ".txt") ||
		strings.HasSuffix(fileName, ".md") ||
		strings.HasSuffix(fileName, ".markdown")
}

func sanitizeUserFacingContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "successfully transferred to agent") {
		return ""
	}
	return trimmed
}

// formatAIOpsContent 格式化 AIOps 流中的可展示内容。
// 输入：事件 agentName、根 agentName、原始文本内容。
// 输出：可展示文本；当内容为空时返回空字符串。
func formatAIOpsContent(agentName, rootName, content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	agentName = strings.TrimSpace(agentName)
	rootName = strings.TrimSpace(rootName)
	if agentName == "" || agentName == rootName {
		return content
	}
	return fmt.Sprintf("[%s]\n%s", agentName, content)
}

func buildInterruptMessage(data any) string {
	base := "流程已暂停，等待你的确认。"
	if data == nil {
		return base
	}
	detail := strings.TrimSpace(fmt.Sprintf("%v", data))
	if detail == "" {
		return base
	}
	if len([]rune(detail)) > 300 {
		detail = string([]rune(detail)[:300]) + "..."
	}
	return base + "\n中断信息：" + detail
}

type inMemoryCheckPointStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newInMemoryCheckPointStore() compose.CheckPointStore {
	return &inMemoryCheckPointStore{
		data: make(map[string][]byte),
	}
}

func (s *inMemoryCheckPointStore) Get(_ context.Context, checkpointID string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	value, exists := s.data[checkpointID]
	if !exists {
		return nil, false, nil
	}
	copied := make([]byte, len(value))
	copy(copied, value)
	return copied, true, nil
}

func (s *inMemoryCheckPointStore) Set(_ context.Context, checkpointID string, checkpoint []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	copied := make([]byte, len(checkpoint))
	copy(copied, checkpoint)
	s.data[checkpointID] = copied
	return nil
}
