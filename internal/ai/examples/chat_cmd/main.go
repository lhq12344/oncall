package main

import (
	"context"
	"fmt"

	"go_agent/internal/agent/dialogue"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

func main() {
	ctx := context.Background()

	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	chatModel, err := models.GetChatModel()
	if err != nil {
		panic(err)
	}

	agent, err := dialogue.NewDialogueAgent(ctx, &dialogue.Config{
		ChatModel: chatModel,
		Embedder:  nil,
		Logger:    logger,
	})
	if err != nil {
		panic(err)
	}

	questions := []string{"错误类型数量", "现在是几点"}
	for _, q := range questions {
		iter := agent.Run(ctx, &adk.AgentInput{
			Messages: []*schema.Message{schema.UserMessage(q)},
		})

		fmt.Println("Q:", q)
		for {
			event, ok := iter.Next()
			if !ok {
				break
			}
			if event != nil && event.Output != nil && event.Output.MessageOutput != nil {
				msg, e := event.Output.MessageOutput.GetMessage()
				if e == nil && msg != nil {
					fmt.Println("A:", msg.Content)
				}
			}
		}
		fmt.Println("----------------")
	}
}
