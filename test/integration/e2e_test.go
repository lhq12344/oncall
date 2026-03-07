package integration

import (
	"context"
	"testing"

	"go_agent/internal/bootstrap"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// TestEndToEnd_SupervisorAgent 端到端测试：Supervisor Agent
func TestEndToEnd_SupervisorAgent(t *testing.T) {
	// 跳过测试（需要 Redis）
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// 创建应用
	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr:     "localhost:30379", // K8s NodePort
		RedisDB:       1,
		LogLevel:      "info",
		PrometheusURL: "http://localhost:30090", // K8s NodePort
		KubeConfig:    "/home/lihaoqian/.kube/config",
	})
	if err != nil {
		t.Fatalf("failed to create application: %v", err)
	}
	defer app.Close()

	ctx := context.Background()

	// 测试用户输入
	userInput := "查看 pod 状态"

	// 构造 Agent 输入
	input := &adk.AgentInput{
		Messages: []*schema.Message{
			schema.UserMessage(userInput),
		},
	}

	// 调用 Supervisor Agent
	iter := app.SupervisorAgent.Run(ctx, input)

	// 读取结果
	var lastMessage *schema.Message
	eventCount := 0
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		eventCount++
		t.Logf("Event %d: %+v", eventCount, event)

		if event != nil && event.Output != nil && event.Output.MessageOutput != nil {
			msg, err := event.Output.MessageOutput.GetMessage()
			if err != nil {
				t.Logf("Error getting message: %v", err)
			} else if msg != nil {
				lastMessage = msg
				t.Logf("Got message: %s", msg.Content)
			}
		}
	}

	t.Logf("Total events: %d", eventCount)

	// 验证输出
	if lastMessage == nil {
		t.Fatal("expected non-nil message")
	}

	t.Logf("User: %s", userInput)
	t.Logf("Assistant: %s", lastMessage.Content)

	t.Log("✓ Supervisor agent test completed")
}

// TestEndToEnd_MultiRound 端到端测试：多轮对话
func TestEndToEnd_MultiRound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr:     "localhost:30379", // K8s NodePort
		RedisDB:       1,
		LogLevel:      "info",
		PrometheusURL: "http://localhost:30090", // K8s NodePort
		KubeConfig:    "/home/lihaoqian/.kube/config",
	})
	if err != nil {
		t.Fatalf("failed to create application: %v", err)
	}
	defer app.Close()

	ctx := context.Background()

	// 多轮对话
	inputs := []string{
		"有问题",
		"服务报错了",
		"nginx 服务一直重启",
	}

	var messages []*schema.Message

	for i, userInput := range inputs {
		t.Logf("Round %d: %s", i+1, userInput)

		// 添加用户消息
		messages = append(messages, schema.UserMessage(userInput))

		// 调用 Agent
		iter := app.SupervisorAgent.Run(ctx, &adk.AgentInput{
			Messages: messages,
		})
		
		// 读取结果
		var lastMessage *schema.Message
		for {
			event, ok := iter.Next()
			if !ok {
				break
			}
			if event != nil && event.Output != nil && event.Output.MessageOutput != nil {
				msg, err := event.Output.MessageOutput.GetMessage()
				if err == nil && msg != nil {
					lastMessage = msg
				}
			}
		}

		// 添加助手回复到历史
		if lastMessage != nil {
			messages = append(messages, lastMessage)
			t.Logf("Assistant: %s", lastMessage.Content)
		}
	}

	t.Log("✓ Multi-round conversation test completed")
}

// TestEndToEnd_KnowledgeSearch 端到端测试：知识检索
func TestEndToEnd_KnowledgeSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr:     "localhost:30379", // K8s NodePort
		RedisDB:       1,
		LogLevel:      "info",
		PrometheusURL: "http://localhost:30090", // K8s NodePort
		KubeConfig:    "/home/lihaoqian/.kube/config",
	})
	if err != nil {
		t.Fatalf("failed to create application: %v", err)
	}
	defer app.Close()

	ctx := context.Background()

	userInput := "之前遇到过 pod 启动失败的问题吗？"

	iter := app.SupervisorAgent.Run(ctx, &adk.AgentInput{
		Messages: []*schema.Message{
			schema.UserMessage(userInput),
		},
	})
	
	// 读取结果
	var lastMessage *schema.Message
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event != nil && event.Output != nil && event.Output.MessageOutput != nil {
			msg, err := event.Output.MessageOutput.GetMessage()
			if err == nil && msg != nil {
				lastMessage = msg
			}
		}
	}

	if lastMessage == nil {
		t.Fatal("expected non-nil message")
	}

	t.Logf("User: %s", userInput)
	t.Logf("Assistant: %s", lastMessage.Content)

	t.Log("✓ Knowledge search test completed")
}
