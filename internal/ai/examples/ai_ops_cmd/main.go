package main

import (
	"context"
	"fmt"

	"go_agent/internal/agent/ops"
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

	agent, err := ops.NewOpsAgent(ctx, &ops.Config{
		ChatModel:     chatModel,
		KubeConfig:    "",
		PrometheusURL: "http://localhost:30090",
		Logger:        logger,
	})
	if err != nil {
		panic(err)
	}

	iter := agent.Run(ctx, &adk.AgentInput{
		Messages: []*schema.Message{
			schema.UserMessage("查询所有服务的运行状态"),
		},
	})

	fmt.Println("----- Ops Agent Response -----")
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event != nil && event.Output != nil && event.Output.MessageOutput != nil {
			msg, e := event.Output.MessageOutput.GetMessage()
			if e == nil && msg != nil {
				fmt.Println(msg.Content)
			}
		}
	}
}
