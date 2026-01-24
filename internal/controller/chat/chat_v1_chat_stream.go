package chat

import (
	"context"
	"errors"
	"go_agent/api/chat/v1"
	"go_agent/internal/ai/agent/chat_pipeline"
	"go_agent/utility/log_call_back"
	"go_agent/utility/mem"
	"io"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

func (c *ControllerV1) ChatStream(ctx context.Context, req *v1.ChatStreamReq) (res *v1.ChatStreamRes, err error) {
	id := req.Id
	msg := req.Question

	ctx = context.WithValue(ctx, "client_id", req.Id)
	client, err := c.service.Create(ctx, g.RequestFromCtx(ctx))
	if err != nil {
		return nil, err
	}

	// 1) 构建 history（裁剪 + 去掉本轮 user，避免重复）
	history, promptMsgs, err := buildHistoryForRequest(ctx, id, msg, 0)
	if err != nil {
		client.SendToClient("error", err.Error())
		return nil, err
	}

	userMessage := &chat_pipeline.UserMessage{
		ID:      id,
		Query:   msg,
		History: history,
	}

	runner, err := chat_pipeline.BuildChatAgent(ctx)
	if err != nil {
		client.SendToClient("error", err.Error())
		return nil, err
	}

	sr, err := runner.Stream(ctx, userMessage, compose.WithCallbacks(log_call_back.LogCallback(nil)))
	if err != nil {
		client.SendToClient("error", err.Error())
		return nil, err
	}
	defer sr.Close()

	var fullResponse strings.Builder

	// 2) 流式过程中：尽量收集 toolcalls、usage（通常最后一个 chunk 才有 usage）
	var (
		collectedToolCalls []schema.ToolCall
		promptTokens       int
		completionTokens   int
	)

	mergeToolCalls := func(dst, src []schema.ToolCall) []schema.ToolCall {
		// 简单策略：只要 src 非空就覆盖/追加。你也可以按 ToolCallID 去重合并。
		if len(src) == 0 {
			return dst
		}
		// 如果你确定只会出现一轮 toolcalls，覆盖就够；否则用 append 再去重
		return src
	}

	for {
		chunk, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			client.SendToClient("error", err.Error())
			return &v1.ChatStreamRes{}, nil
		}

		// 2.1 聚合文本
		if chunk.Content != "" {
			fullResponse.WriteString(chunk.Content)
			client.SendToClient("message", chunk.Content)
		}

		// 2.2 收集 tool_calls（如果 stream 会吐 tool_calls delta，这里做个聚合）
		if len(chunk.ToolCalls) > 0 {
			collectedToolCalls = mergeToolCalls(collectedToolCalls, chunk.ToolCalls)
		}

		// 2.3 尝试获取 usage（很多实现只在最后/收尾 chunk 给）
		if chunk.ResponseMeta != nil && chunk.ResponseMeta.Usage != nil {
			if chunk.ResponseMeta.Usage.PromptTokens > 0 {
				promptTokens = chunk.ResponseMeta.Usage.PromptTokens
			}
			if chunk.ResponseMeta.Usage.CompletionTokens > 0 {
				completionTokens = chunk.ResponseMeta.Usage.CompletionTokens
			}
		}
	}

	// 3) EOF：一次性写回上下文（非常关键：assistant 不能写 SystemMessage）
	completeResponse := fullResponse.String()
	if completeResponse != "" {
		assistantMsg := schema.AssistantMessage(completeResponse, collectedToolCalls)

		// 混合策略：
		// - assistant tokens：优先用 completionTokens（更准）
		// - user tokens：请求前只能估算/预留；这里可用 promptTokens 做校准
		if promptTokens > 0 || completionTokens > 0 {
			// 使用“混合策略写回”
			if err := mem.SetMessages(ctx, id, schema.UserMessage(msg), assistantMsg, promptMsgs, promptTokens, completionTokens); err != nil {
				// 失败兜底：仍然写估算版，至少不丢上下文
				//TODO:后面补
			}
		}
	}

	client.SendToClient("done", "Stream completed")
	return &v1.ChatStreamRes{}, nil
}
