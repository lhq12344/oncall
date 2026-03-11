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
	"go_agent/utility/mem"
	"go_agent/utility/tokenizer"

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
	defaultSessionID       = "default-session"
	defaultReserveToolCost = 5
	opsDiagnosticPrompt    = "请执行系统健康检查，分析当前系统状态，识别潜在问题并给出分步骤的诊断和解决方案。重点关注：1) Kubernetes Pod状态 2) 关键指标异常 3) 错误日志"
)

type ControllerV1 struct {
	chatAgent        adk.ResumableAgent
	chatStreamRunner *adk.Runner
	opsStreamRunner  *adk.Runner
	rootAgentName    string
	logger           *zap.Logger
	opsAgent         adk.Agent
	knowledgeAgent   adk.Agent
}

func NewV1(
	chatAgent adk.ResumableAgent,
	logger *zap.Logger,
	redisClient *redis.Client,
	opsAgent adk.Agent,
	knowledgeAgent adk.Agent,
) *ControllerV1 {
	ctrl := &ControllerV1{
		chatAgent:      chatAgent,
		rootAgentName:  "chat_agent",
		logger:         logger,
		opsAgent:       opsAgent,
		knowledgeAgent: knowledgeAgent,
	}

	var checkpointStore compose.CheckPointStore
	if redisClient != nil {
		checkpointStore = appcontext.NewRedisCheckPointStore(redisClient, "oncall", 24*time.Hour)
	} else {
		checkpointStore = newInMemoryCheckPointStore()
	}

	if chatAgent != nil {
		if agentName := strings.TrimSpace(chatAgent.Name(context.Background())); agentName != "" {
			ctrl.rootAgentName = agentName
		}
		ctrl.chatStreamRunner = adk.NewRunner(context.Background(), adk.RunnerConfig{
			Agent:           chatAgent,
			EnableStreaming: true,
			CheckPointStore: checkpointStore,
		})
	}

	if opsAgent != nil {
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

	memory := mem.GetSimpleMemory(sessionID)
	messages, err := c.buildHistoryMessages(ctx, sessionID, question)
	if err != nil {
		return nil, err
	}

	checkpointID := generateCheckpointID(sessionID)
	iter := c.chatStreamRunner.Run(ctx, messages, adk.WithCheckPointID(checkpointID))

	var fullAnswer strings.Builder
	interrupted := false

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

		chunk, ok := c.extractAssistantContent(event)
		if !ok {
			continue
		}
		fullAnswer.WriteString(chunk)
		writeSSEData(r, chunk)
	}

	writeSSEData(r, "[DONE]")

	answer := strings.TrimSpace(fullAnswer.String())
	if answer != "" && !interrupted {
		c.saveDialogueHistory(ctx, memory, question, answer, messages, sessionID)
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

	iter, err := c.resumeAgent(ctx, c.chatStreamRunner, req.CheckpointID, req.InterruptIDs, req.Approved, req.Resolved, req.Comment)
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
	}, adk.WithCheckPointID(checkpointID))

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

		if event.Output != nil && event.Output.MessageOutput != nil && event.Output.MessageOutput.Message != nil {
			msg := event.Output.MessageOutput.Message
			for _, call := range msg.ToolCalls {
				writeSSEData(r, fmt.Sprintf("{\"type\":\"step\",\"step\":%d,\"content\":%q}", stepNum, "调用工具: "+call.Function.Name))
				stepNum++
			}

			content := sanitizeUserFacingContent(msg.Content)
			if content != "" {
				writeSSEData(r, fmt.Sprintf("{\"type\":\"content\",\"content\":%q}", content))
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

	iter, err := c.resumeAgent(ctx, c.opsStreamRunner, req.CheckpointID, req.InterruptIDs, req.Approved, req.Resolved, req.Comment)
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

		if event.Output != nil && event.Output.MessageOutput != nil && event.Output.MessageOutput.Message != nil {
			msg := event.Output.MessageOutput.Message
			for _, call := range msg.ToolCalls {
				writeSSEData(r, fmt.Sprintf("{\"type\":\"step\",\"step\":%d,\"content\":%q}", stepNum, "调用工具: "+call.Function.Name))
				stepNum++
			}

			content := sanitizeUserFacingContent(msg.Content)
			if content != "" {
				writeSSEData(r, fmt.Sprintf("{\"type\":\"content\",\"content\":%q}", content))
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
) (*adk.AsyncIterator[*adk.AgentEvent], error) {
	if runner == nil {
		return nil, fmt.Errorf("runner is not initialized")
	}

	targetIDs := normalizeIDList(interruptIDs)
	if len(targetIDs) == 0 {
		return runner.Resume(ctx, checkpointID)
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

	return runner.ResumeWithParams(ctx, checkpointID, &adk.ResumeParams{Targets: targets})
}

func (c *ControllerV1) buildHistoryMessages(ctx context.Context, sessionID, question string) ([]*schema.Message, error) {
	messages, err := mem.GetMessagesForRequest(ctx, sessionID, schema.UserMessage(question), defaultReserveToolCost)
	if err != nil {
		if c.logger != nil {
			c.logger.Warn("failed to get history, fallback to current question",
				zap.String("session_id", sessionID),
				zap.Error(err))
		}
		return []*schema.Message{schema.UserMessage(question)}, nil
	}
	if len(messages) == 0 {
		return []*schema.Message{schema.UserMessage(question)}, nil
	}
	return messages, nil
}

func (c *ControllerV1) saveDialogueHistory(
	ctx context.Context,
	memory *mem.SimpleMemory,
	question string,
	answer string,
	promptMessages []*schema.Message,
	sessionID string,
) {
	if memory == nil || strings.TrimSpace(answer) == "" {
		return
	}

	userMsg := schema.UserMessage(question)
	assistantMsg := schema.AssistantMessage(answer, nil)

	promptTokens := len(question) / 4
	if precisePromptTokens, err := tokenizer.CountMessagesTokens(ctx, promptMessages, false); err == nil && precisePromptTokens > 0 {
		promptTokens = precisePromptTokens
	}

	completionTokens := len(answer) / 4
	if preciseCompletionTokens, err := tokenizer.CountMessageTokens(ctx, assistantMsg, false); err == nil && preciseCompletionTokens > 0 {
		completionTokens = preciseCompletionTokens
	}

	if err := memory.SetMessages(ctx, userMsg, assistantMsg, promptMessages, promptTokens, completionTokens); err != nil && c.logger != nil {
		c.logger.Warn("failed to save history",
			zap.String("session_id", sessionID),
			zap.Error(err))
	}
}

func (c *ControllerV1) extractAssistantContent(event *adk.AgentEvent) (string, bool) {
	if event == nil || event.Output == nil || event.Output.MessageOutput == nil || event.Output.MessageOutput.Message == nil {
		return "", false
	}
	msg := event.Output.MessageOutput.Message
	if msg.Role != schema.Assistant {
		return "", false
	}
	if event.AgentName != "" && event.AgentName != c.rootAgentName {
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
	r.Response.Write(fmt.Sprintf("data: %s\n\n", data))
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
