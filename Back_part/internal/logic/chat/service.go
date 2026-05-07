package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	v1 "go_agent/api/chat/v1"
	appcontext "go_agent/internal/logic/session"
	"go_agent/internal/model"

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
	defaultSessionID = "default-session"
)

type Service struct {
	dialogueAgent    adk.ResumableAgent
	chatStreamRunner *adk.Runner
	rootAgentName    string
	sessionMemory    *appcontext.SessionMemory
	logger           *zap.Logger
	knowledgeAgent   adk.Agent
}

// NewService 创建聊天业务服务。
//
// 功能：
// 1. 初始化控制器实例，绑定各个 Agent
// 2. 创建检查点存储（Redis 或内存）
// 3. 初始化聊天流式 Runner
//
// 调用位置：
// - main.go:118 行，应用启动时调用
//
// 输入：
// - dialogueAgent: 对话代理（可选）
// - logger: 日志记录器
// - redisClient: Redis 客户端（可选，用于持久化检查点）
// - knowledgeAgent: 知识代理（可选）
//
// 输出：
// - *Service: 初始化完成的聊天业务服务
func NewService(
	dialogueAgent adk.ResumableAgent,
	logger *zap.Logger,
	redisClient *redis.Client,
	knowledgeAgent adk.Agent,
) *Service {
	ctrl := &Service{
		dialogueAgent:  dialogueAgent,
		rootAgentName:  "dialogue_agent",
		sessionMemory:  appcontext.NewSessionMemory(nil, logger),
		logger:         logger,
		knowledgeAgent: knowledgeAgent,
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

	return ctrl
}

// Stream handles streaming chat through the internal chat model input.
func (c *Service) Stream(ctx context.Context, input model.ChatStreamInput) error {
	_, err := c.ChatStream(ctx, &v1.ChatStreamReq{
		Id:       input.SessionID,
		Question: input.Question,
	})
	return err
}

// Resume resumes an interrupted streaming chat through the internal resume input.
func (c *Service) Resume(ctx context.Context, input model.ChatResumeInput) error {
	_, err := c.ChatResumeStream(ctx, &v1.ChatResumeStreamReq{
		Id:             input.SessionID,
		CheckpointID:   input.CheckpointID,
		InterruptIDs:   input.InterruptIDs,
		Approved:       input.Approved,
		Resolved:       input.Resolved,
		Comment:        input.Comment,
		SelectionValue: input.SelectionValue,
	})
	return err
}

// ChatStream 处理聊天流式请求。
//
// 功能：
// 1. 验证输入参数（问题和会话 ID）
// 2. 构建会话消息历史
// 3. 创建检查点 ID 并启动流式 Runner
// 4. 处理流式事件（content、interrupt、error、done）
// 5. 保存完整的对话历史
//
// 调用位置：
// - API 路由 `/api/v1/chat/stream` 的处理函数
//
// 输入：
// - ctx: 上下文
// - req: 聊天流式请求参数（包含问题和会话 ID）
//
// 输出：
// - *v1.ChatStreamRes: 响应对象（实际响应通过 SSE 流式输出）
// - error: 处理过程中的错误
//
// SSE 事件类型：
// - content: 助手回复内容块
// - interrupt: 中断请求（需要用户审批）
// - error: 错误信息
// - done: 流式结束标记
func (c *Service) ChatStream(ctx context.Context, req *v1.ChatStreamReq) (res *v1.ChatStreamRes, err error) {
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
	var intermediateMessages []*schema.Message
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
			// 收集工具调用/结果消息，用于记忆持久化
			if msg.Role == schema.Tool ||
				(msg.Role == schema.Assistant && len(msg.ToolCalls) > 0) {
				intermediateMessages = append(intermediateMessages, msg)
			}
		}

		if event.Action != nil && event.Action.Interrupted != nil {
			interrupted = true
			payload := buildInterruptPayload(checkpointID, event.Action.Interrupted)
			payloadBytes, _ := json.Marshal(payload)
			writeSSEData(r, string(payloadBytes))
			continue
		}
		//将工具调用信息传回前端
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
		c.sessionMemory.SaveTurnWithSource(context.Background(), sessionID, question, answer, intermediateMessages, messages, "chat_stream")
	}

	return &v1.ChatStreamRes{}, nil
}

// ChatResumeStream 处理聊天中断恢复请求。
//
// 功能：
// 1. 验证输入参数（会话 ID、检查点 ID、中断 ID、审批结果）
// 2. 恢复流式 Runner，从中断点继续执行
// 3. 处理恢复后的流式事件
// 4. 保存恢复操作的历史记录
//
// 调用位置：
// - API 路由 `/api/v1/chat/resume` 的处理函数
//
// 输入：
// - ctx: 上下文
// - req: 中断恢复请求参数（包含会话 ID、检查点 ID、中断 ID、审批结果等）
//
// 输出：
// - *v1.ChatResumeStreamRes: 响应对象（实际响应通过 SSE 流式输出）
// - error: 处理过程中的错误
//
// 中断恢复流程：
// 1. 用户收到中断请求（需要审批高风险命令）
// 2. 用户通过前端提交审批结果（approved/resolved/comment）
// 3. 调用此方法恢复执行
// 4. 继续执行被中断的流程
func (c *Service) ChatResumeStream(ctx context.Context, req *v1.ChatResumeStreamReq) (res *v1.ChatResumeStreamRes, err error) {
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}
	if strings.TrimSpace(req.CheckpointID) == "" {
		return nil, fmt.Errorf("checkpoint_id is required")
	}
	if strings.TrimSpace(req.Id) == "" {
		return nil, fmt.Errorf("id is required")
	}
	sessionID := normalizeSessionID(req.Id)

	iter, err := c.resumeAgent(ctx, c.chatStreamRunner, req.CheckpointID, req.InterruptIDs, req.Approved, req.Resolved, req.Comment, req.SelectionValue, map[string]any{
		"session_id": sessionID,
	})
	if err != nil {
		return nil, err
	}

	r, err := setupSSE(ctx)
	if err != nil {
		return nil, err
	}

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
			payload := buildInterruptPayload(req.CheckpointID, event.Action.Interrupted)
			b, _ := json.Marshal(payload)
			writeSSEData(r, string(b))
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
		approvedValue := "nil"
		if req.Approved != nil {
			approvedValue = fmt.Sprintf("%v", *req.Approved)
		}
		resolvedValue := "nil"
		if req.Resolved != nil {
			resolvedValue = fmt.Sprintf("%v", *req.Resolved)
		}
		comment := strings.TrimSpace(req.Comment)
		if comment == "" {
			comment = "(empty)"
		}
		selectionValue := strings.TrimSpace(req.SelectionValue)
		if selectionValue == "" {
			selectionValue = "(empty)"
		}
		interruptIDs := normalizeIDList(req.InterruptIDs)
		if len(interruptIDs) == 0 {
			interruptIDs = []string{"(all or checkpoint-level resume)"}
		}
		resumeInput := fmt.Sprintf(
			"恢复执行确认：checkpoint_id=%s; interrupt_ids=%s; approved=%s; resolved=%s; comment=%s; selection_value=%s",
			strings.TrimSpace(req.CheckpointID),
			strings.Join(interruptIDs, ","),
			approvedValue,
			resolvedValue,
			comment,
			selectionValue,
		)
		c.sessionMemory.SaveTurnWithSource(context.Background(), sessionID, resumeInput, answer, nil, nil, "chat_resume_stream")
	}

	return &v1.ChatResumeStreamRes{}, nil
}

func (c *Service) FileUpload(ctx context.Context, req *v1.FileUploadReq) (res *v1.FileUploadRes, err error) {
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

func (c *Service) resumeAgent(
	ctx context.Context,
	runner *adk.Runner,
	checkpointID string,
	interruptIDs []string,
	approved *bool,
	resolved *bool,
	comment string,
	selectionValue string,
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

	targetPayload := buildResumeTargetPayload(approved, resolved, comment, selectionValue)
	if len(targetPayload) == 0 {
		targetPayload["comment"] = "继续执行"
	}

	targets := make(map[string]any, len(targetIDs))
	for _, id := range targetIDs {
		targets[id] = targetPayload
	}

	return runner.ResumeWithParams(ctx, checkpointID, &adk.ResumeParams{Targets: targets}, baseOpts...)
}

func (c *Service) extractAssistantContent(event *adk.AgentEvent) (string, bool) {
	msg, ok := c.resolveEventMessage(event)
	if !ok {
		return "", false
	}
	return c.extractAssistantContentFromResolved(event, msg)
}

// extractAssistantContentFromResolved 从已解析的事件和消息中提取助手回复内容。
//
// 功能：
// 1. 首先尝试从主 Agent（rootAgentName）的消息中提取内容
// 2. 如果失败，放宽条件，允许任何 Agent 的 assistant 消息透出
//
// 调用位置：
// - ChatStream:207 行，提取聊天流式响应的内容块
// - extractAssistantContent:627 行，辅助函数调用
//
// 输入：
// - event: ADK Agent 事件（包含 Agent 名称和输出）
// - msg: schema.Message 消息（可能包含助手回复内容）
//
// 输出：
// - string: 提取的助手回复内容（可能为空）
// - bool: 是否成功提取内容
//
// 提取逻辑：
// 1. 检查消息角色是否为 assistant
// 2. 检查 Agent 名称是否匹配（优先主 Agent）
// 3. 检查是否包含工具调用 ID（工具调用消息不提取）
// 4. 清理内容中的特殊字符和格式
func (c *Service) extractAssistantContentFromResolved(event *adk.AgentEvent, msg *schema.Message) (string, bool) {
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

func (c *Service) resolveEventMessage(event *adk.AgentEvent) (*schema.Message, bool) {
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

func (c *Service) extractAgentContent(event *adk.AgentEvent, rootName string) (string, bool) {
	msg, ok := c.resolveEventMessage(event)
	if !ok {
		return "", false
	}
	return c.extractAgentContentByMessage(event.AgentName, msg, rootName)
}

func (c *Service) extractAgentContentByMessage(agentName string, msg *schema.Message, rootName string) (string, bool) {
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

// setupSSE 初始化服务器发送事件（Server-Sent Events）响应。
//
// 功能：
// 1. 从上下文中获取 HTTP 请求对象
// 2. 设置 SSE 响应头（Content-Type、Cache-Control、Connection 等）
// 3. 写入 HTTP 200 状态码并刷新响应头
// 4. 返回请求对象，用于后续写入 SSE 数据
//
// 调用位置：
// - ChatStream:100 行，聊天流式请求开始时调用
// - ChatResumeStream:260 行，中断恢复请求开始时调用
//
// 输入：
// - ctx: 上下文（包含 HTTP 请求）
//
// 输出：
// - *ghttp.Request: HTTP 请求对象（用于后续写入 SSE 数据）
// - error: 获取请求失败时返回错误
//
// SSE 响应头说明：
// - Content-Type: text/event-stream - 指定响应类型为 SSE
// - Cache-Control: no-cache - 禁止缓存
// - Connection: keep-alive - 保持连接
// - X-Accel-Buffering: no - 禁用 Nginx 缓冲（用于流式响应）
//
// 使用示例：
//
//	r, err := setupSSE(ctx)
//	if err != nil {
//	    return nil, err
//	}
//	writeSSEData(r, "data: hello\n\n")
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

// writeSSEData 向 SSE 响应中写入数据。
//
// 功能：
// 1. 将数据包装为 SSE 格式并写入响应流
// 2. 刷新响应缓冲区，立即发送给客户端
//
// 输入：
// - r: HTTP 请求对象（来自 setupSSE）
// - data: 要发送的数据内容
//
// SSE 数据格式：
//
//	data: <内容行1>
//	data: <内容行2>
//	[空行]
func writeSSEData(r *ghttp.Request, data string) {
	if r == nil {
		return
	}
	_ = writeSSEPayload(sseResponseWriter{resp: r.Response}, data)
	r.Response.Flush()
}

type sseResponseWriter struct {
	resp interface {
		Write(content ...interface{})
	}
}

func (w sseResponseWriter) Write(p []byte) (int, error) {
	if w.resp == nil {
		return 0, nil
	}
	w.resp.Write(p)
	return len(p), nil
}

// writeSSEPayload 将数据格式化为 SSE 协议格式并写入 writer。
//
// 功能：
// 1. 使用自定义扫描器分割数据行（支持 \n 和 \r\n）
// 2. 每行数据前添加 "data: " 前缀
// 3. 写入空行作为事件结束标记
// 4. 处理空数据和边界情况
//
// 输入：
// - w: io.Writer 接口（通常是 HTTP 响应流）
// - data: 要发送的原始数据
//
// 输出：
// - error: 写入过程中的错误
//
// SSE 协议格式：
//
//	data: <line1>
//	data: <line2>
//	[空行]
func writeSSEPayload(w io.Writer, data string) error {
	if w == nil {
		return nil
	}

	scanner := bufio.NewScanner(strings.NewReader(data))
	scanner.Split(scanSSELines)

	maxTokenSize := len(data) + 1
	if maxTokenSize < bufio.MaxScanTokenSize {
		maxTokenSize = bufio.MaxScanTokenSize
	}
	scanner.Buffer(make([]byte, 0, 1024), maxTokenSize)

	wroteAny := false
	for scanner.Scan() {
		wroteAny = true
		if _, err := io.WriteString(w, "data: "); err != nil {
			return err
		}
		if _, err := w.Write(scanner.Bytes()); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	if !wroteAny || strings.HasSuffix(data, "\n") || strings.HasSuffix(data, "\r") {
		if _, err := io.WriteString(w, "data: \n"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "\n")
	return err
}

// scanSSELines 自定义扫描函数，用于分割 SSE 数据行。
//
// 功能：
// 1. 按 \n 或 \r\n 分割数据
// 2. 处理 \r 单独出现的情况
// 3. 处理 EOF 边界情况
//
// 输入：
// - data: 待扫描的字节数据
// - atEOF: 是否到达数据流末尾
//
// 输出：
// - advance: 前进的字节数
// - token: 提取的令牌（一行数据）
// - err: 错误信息
func scanSSELines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\n':
			return i + 1, data[:i], nil
		case '\r':
			if i+1 < len(data) {
				if data[i+1] == '\n' {
					return i + 2, data[:i], nil
				}
				return i + 1, data[:i], nil
			}
			if atEOF {
				return i + 1, data[:i], nil
			}
			return 0, nil, nil
		}
	}

	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
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

// buildInterruptPayload 构造统一的 SSE 中断载荷。
// 输入：checkpointID、中断信息。
// 输出：可直接序列化的中断 payload。
func buildInterruptPayload(checkpointID string, info *adk.InterruptInfo) map[string]any {
	payload := map[string]any{
		"type":          "interrupt",
		"checkpoint_id": strings.TrimSpace(checkpointID),
	}
	if info == nil {
		payload["interrupt_contexts"] = []v1.InterruptContext{}
		payload["message"] = buildInterruptMessage(nil)
		return payload
	}

	payload["interrupt_contexts"] = convertInterruptContexts(info.InterruptContexts)
	payload["message"] = buildInterruptMessage(info.Data)

	if structured := normalizeInterruptData(info.Data); structured != nil {
		payload["interrupt_data"] = structured
		if detailRequest := extractDetailSelectionPayload(structured); detailRequest != nil {
			payload["detail_request"] = detailRequest
		}
	}
	return payload
}

// normalizeInterruptData 将中断数据归一化为可 JSON 传输的对象。
// 输入：任意中断数据。
// 输出：归一化后的 JSON 兼容对象。
func normalizeInterruptData(data any) any {
	if data == nil {
		return nil
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return nil
	}

	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil
	}
	return normalized
}

func extractDetailSelectionPayload(data any) map[string]any {
	value, ok := data.(map[string]any)
	if !ok || value == nil {
		return nil
	}

	field, _ := value["field"].(string)
	question, _ := value["question"].(string)
	options, ok := value["options"].([]any)
	if strings.TrimSpace(field) == "" || strings.TrimSpace(question) == "" || !ok || len(options) == 0 {
		return nil
	}

	normalizedOptions := make([]map[string]any, 0, len(options))
	for _, item := range options {
		optionValue, ok := item.(map[string]any)
		if !ok {
			continue
		}
		label, _ := optionValue["label"].(string)
		rawValue, _ := optionValue["value"].(string)
		if strings.TrimSpace(label) == "" || strings.TrimSpace(rawValue) == "" {
			continue
		}
		optionPayload := map[string]any{
			"label": strings.TrimSpace(label),
			"value": strings.TrimSpace(rawValue),
		}
		if description, ok := optionValue["description"].(string); ok && strings.TrimSpace(description) != "" {
			optionPayload["description"] = strings.TrimSpace(description)
		}
		normalizedOptions = append(normalizedOptions, optionPayload)
	}
	if len(normalizedOptions) == 0 {
		return nil
	}

	payload := map[string]any{
		"field":    strings.TrimSpace(field),
		"question": strings.TrimSpace(question),
		"options":  normalizedOptions,
	}
	if reason, ok := value["reason"].(string); ok && strings.TrimSpace(reason) != "" {
		payload["reason"] = strings.TrimSpace(reason)
	}
	return payload
}

func buildResumeTargetPayload(approved *bool, resolved *bool, comment string, selectionValue string) map[string]any {
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
	if value := strings.TrimSpace(selectionValue); value != "" {
		targetPayload["selection_value"] = value
	}
	return targetPayload
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
