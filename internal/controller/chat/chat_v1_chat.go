package chat

import (
	"context"
	"go_agent/api/chat/v1"
	"go_agent/internal/ai/agent/chat_pipeline"
	"go_agent/utility/log_call_back"
	"go_agent/utility/mem"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func buildHistoryForRequest(
	ctx context.Context,
	id string,
	query string,
	reserveToolsTokens int,
) (history []*schema.Message, promptMsgs []*schema.Message, err error) {

	userMsg := schema.UserMessage(query)

	// promptMsgs = sys + turns + userMsg（并会在 mem 内按预算裁剪 turns）
	promptMsgs, err = mem.GetMessagesForRequest(ctx, id, userMsg, reserveToolsTokens)
	if err != nil {
		return nil, nil, err
	}

	// 你的 runner.Invoke 会再把 Query 当作本轮 user 输入拼一次
	// 所以传给 pipeline 的 history 必须去掉最后这条 userMsg，避免重复
	history = promptMsgs
	if n := len(promptMsgs); n > 0 {
		last := promptMsgs[n-1]
		if string(last.Role) == "user" && last.Content == query {
			history = promptMsgs[:n-1]
		}
	}
	return history, promptMsgs, nil
}

func (c *ControllerV1) Chat(ctx context.Context, req *v1.ChatReq) (res *v1.ChatRes, err error) {
	id := req.Id
	msg := req.Question
	history, prompt1, err := buildHistoryForRequest(ctx, id, msg, 0) // reserveTools=0 表示走默认 ReserveToolsDefault
	if err != nil {
		panic(err)
	}
	userMessage := &chat_pipeline.UserMessage{
		ID:      id,
		Query:   msg,
		History: history,
	}

	runner, err := chat_pipeline.BuildChatAgent(ctx)
	if err != nil {
		return nil, err
	}

	out, err := runner.Invoke(ctx, userMessage, compose.WithCallbacks(log_call_back.LogCallback(nil)))
	if err != nil {
		return nil, err
	}
	res = &v1.ChatRes{
		Answer: out.Content,
	}
	pt, ct := 0, 0
	if out.ResponseMeta != nil && out.ResponseMeta.Usage != nil {
		pt = out.ResponseMeta.Usage.PromptTokens
		ct = out.ResponseMeta.Usage.CompletionTokens
	}

	assistantMsg := schema.AssistantMessage(out.Content, out.ToolCalls)
	if err := mem.SetMessages(ctx, id, schema.UserMessage(msg), assistantMsg, prompt1, pt, ct); err != nil {
		panic(err)
	}

	return res, nil
}
