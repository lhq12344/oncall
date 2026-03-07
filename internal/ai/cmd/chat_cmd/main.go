package main

import (
	"context"
	"fmt"
	"go_agent/internal/ai/agent/chat_pipeline"
	"go_agent/utility/mem"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"
)

// 根据 Query 获取“请求级裁剪后的 history（不包含本轮 userMsg，避免重复）”
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

func main() {
	ctx := context.Background()
	id := "112"

	// 1) 初始化 Redis（单机测试）
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:30379",
		DB:          0,
		DialTimeout: 2 * time.Second,
	})

	if err := mem.InitRedis(rdb, &mem.Config{
		MaxInputTokens:         96000,
		ReserveOutputTokens:    8192,
		ReserveToolsDefault:    20000, // 不用工具可降到 8000 或 0
		SafetyTokens:           2048,
		TTL:                    2 * time.Hour, // 滑动过期
		KeepReasoningInContext: false,         // 建议 false
	}); err != nil {
		panic(err)
	}

	// 2) 构建 agent runner
	runner, err := chat_pipeline.BuildChatAgent(ctx)
	if err != nil {
		panic(err)
	}

	// ---------------- 第一次对话 ----------------
	q1 := "错误类型数量"

	h1, prompt1, err := buildHistoryForRequest(ctx, id, q1, 0) // reserveTools=0 表示走默认 ReserveToolsDefault
	if err != nil {
		panic(err)
	}

	userMessage := &chat_pipeline.UserMessage{
		ID:      id,
		Query:   q1,
		History: h1,
	}

	out, err := runner.Invoke(ctx, userMessage)
	if err != nil {
		panic(err)
	}
	fmt.Println("Q:", q1)
	fmt.Println("A:", out.Content)

	pt, ct := 0, 0
	if out.ResponseMeta != nil && out.ResponseMeta.Usage != nil {
		pt = out.ResponseMeta.Usage.PromptTokens
		ct = out.ResponseMeta.Usage.CompletionTokens
	}

	assistantMsg := schema.AssistantMessage(out.Content, out.ToolCalls)
	if err := mem.SetMessages(ctx, id, schema.UserMessage(q1), assistantMsg, prompt1, pt, ct); err != nil {
		panic(err)
	}

	// ---------------- 第二次对话 ----------------
	q2 := "现在是几点"

	h2, prompt2, err := buildHistoryForRequest(ctx, id, q2, 0)
	if err != nil {
		panic(err)
	}

	userMessage = &chat_pipeline.UserMessage{
		ID:      id,
		Query:   q2,
		History: h2,
	}

	out, err = runner.Invoke(ctx, userMessage)
	if err != nil {
		panic(err)
	}

	fmt.Println("----------------")
	fmt.Println("Q:", q2)
	fmt.Println("A:", out.Content)

	pt, ct = 0, 0
	if out.ResponseMeta != nil && out.ResponseMeta.Usage != nil {
		pt = out.ResponseMeta.Usage.PromptTokens
		ct = out.ResponseMeta.Usage.CompletionTokens
	}

	assistantMsg = schema.AssistantMessage(out.Content, out.ToolCalls)
	if err := mem.SetMessages(ctx, id, schema.UserMessage(q2), assistantMsg, prompt2, pt, ct); err != nil {
		panic(err)
	}
}
