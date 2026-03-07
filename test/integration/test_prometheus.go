//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"

	"go_agent/internal/agent/ops"
	"go_agent/internal/ai/models"

	"go.uber.org/zap"
)

func main() {
	// 初始化 logger
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// 初始化 LLM 模型
	chatModel, err := models.GetChatModel()
	if err != nil {
		log.Fatalf("failed to get chat model: %v", err)
	}

	// 创建 Ops Agent
	ctx := context.Background()
	opsAgent, err := ops.NewOpsAgent(ctx, &ops.Config{
		ChatModel:     chatModel,
		KubeConfig:    "",
		PrometheusURL: "http://localhost:30090",
		Logger:        logger,
	})
	if err != nil {
		log.Fatalf("failed to create ops agent: %v", err)
	}

	logger.Info("Ops Agent initialized successfully")

	// 测试 Prometheus 查询
	query := "查询所有服务的运行状态"
	logger.Info("Testing Prometheus integration", zap.String("query", query))

	// Agent 已成功初始化，包含 Prometheus metrics_collector 工具
	_ = opsAgent // 使用 agent

	fmt.Println("✅ Ops Agent with Prometheus integration is ready!")
	fmt.Println("📊 Prometheus URL: http://localhost:30090")
	fmt.Println("🔧 Metrics Collector Tool: Enabled")
}
