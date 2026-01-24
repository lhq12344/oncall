package chat_pipeline

import (
	"context"
	"time"
)

// newInputToRagLambda component initialization function of node 'InputToQuery' in graph 'EinoAgent'
func newInputToRagLambda(ctx context.Context, input *UserMessage, opts ...any) (output string, err error) {
	return input.Query, nil
}

// newInputToChatLambda component initialization function of node 'InputToHistory' in graph 'EinoAgent'
func newInputToChatLambda(ctx context.Context, input *UserMessage, opts ...any) (output map[string]any, err error) {
	return map[string]any{
		"content": input.Query,
		"history": input.History,
		"date":    time.Now().Format("2006-01-02 15:04:05"),
	}, nil
}

// newMergeLambda 合并InputToChat和MilvusRetriever的输出为ChatTemplate的输入
func newMergeLambda(ctx context.Context, input map[string]any, opts ...any) (output map[string]any, err error) {
	result := make(map[string]any)

	// 复制所有输入的键值对
	for k, v := range input {
		result[k] = v
	}

	return result, nil
}
