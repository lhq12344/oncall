package chat

import (
	"context"
	"fmt"

	"go_agent/api/chat/v1"
	"go_agent/utility/mem"
	"go_agent/utility/tokenizer"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type ControllerV1 struct {
	supervisorAgent adk.ResumableAgent
	logger          *zap.Logger
}

func NewV1(supervisorAgent adk.ResumableAgent, logger *zap.Logger) *ControllerV1 {
	return &ControllerV1{
		supervisorAgent: supervisorAgent,
		logger:          logger,
	}
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

	// 调用 supervisor agent
	iterator := c.supervisorAgent.Run(ctx, input)

	// 收集响应
	var answer string
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}

		// 检查错误
		if event.Err != nil {
			c.logger.Error("supervisor agent error",
				zap.String("session_id", req.Id),
				zap.Error(event.Err))
			return nil, fmt.Errorf("agent error: %w", event.Err)
		}

		// 提取消息内容
		if event.Output != nil && event.Output.MessageOutput != nil {
			if event.Output.MessageOutput.Message != nil {
				answer += event.Output.MessageOutput.Message.Content
			}
		}
	}

	if answer == "" {
		answer = "抱歉，我无法生成回答。"
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
		zap.Int("answer_length", len(answer)))

	return &v1.ChatRes{
		Answer: answer,
	}, nil
}

func (c *ControllerV1) ChatStream(ctx context.Context, req *v1.ChatStreamReq) (res *v1.ChatStreamRes, err error) {
	// TODO: Implement streaming chat logic
	return &v1.ChatStreamRes{}, nil
}

func (c *ControllerV1) FileUpload(ctx context.Context, req *v1.FileUploadReq) (res *v1.FileUploadRes, err error) {
	// TODO: Implement file upload logic
	return &v1.FileUploadRes{}, nil
}

func (c *ControllerV1) AIOps(ctx context.Context, req *v1.AIOpsReq) (res *v1.AIOpsRes, err error) {
	// TODO: Implement AI ops logic
	return &v1.AIOpsRes{
		Result: "AI Ops endpoint not yet implemented",
		Detail: []string{},
	}, nil
}
