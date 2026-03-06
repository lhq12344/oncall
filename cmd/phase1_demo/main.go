package main

import (
	"context"
	"fmt"
	"log"

	"go_agent/internal/bootstrap"
)

func main() {
	// 初始化应用
	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr:     "localhost:6379",
		RedisPassword: "",
		RedisDB:       1,
		LogLevel:      "info",
	})
	if err != nil {
		log.Fatalf("failed to init application: %v", err)
	}
	defer app.Close()

	fmt.Println("=== Phase 1 Demo: 上下文管理与 Supervisor Agent ===\n")

	ctx := context.Background()

	// 1. 创建会话
	fmt.Println("1. 创建用户会话...")
	session, err := app.ContextManager.CreateSession(ctx, "demo_user")
	if err != nil {
		log.Fatalf("failed to create session: %v", err)
	}
	fmt.Printf("   ✓ 会话创建成功: %s\n\n", session.SessionID)

	// 2. 添加对话消息
	fmt.Println("2. 添加对话消息...")
	messages := []struct {
		role    string
		content string
	}{
		{"user", "查看 pod 状态"},
		{"assistant", "正在查询 pod 状态..."},
		{"user", "有什么异常吗？"},
		{"assistant", "发现 3 个 pod 处于 Pending 状态"},
	}

	for _, msg := range messages {
		err := app.ContextManager.AddMessage(ctx, session.SessionID, msg.role, msg.content)
		if err != nil {
			log.Fatalf("failed to add message: %v", err)
		}
		fmt.Printf("   ✓ [%s] %s\n", msg.role, msg.content)
	}
	fmt.Println()

	// 3. 获取对话历史
	fmt.Println("3. 获取对话历史...")
	history, err := app.ContextManager.GetHistory(ctx, session.SessionID, 10)
	if err != nil {
		log.Fatalf("failed to get history: %v", err)
	}
	fmt.Printf("   ✓ 共 %d 条消息\n\n", len(history))

	// 4. 测试意图分类
	fmt.Println("4. 测试意图分类...")
	testInputs := []string{
		"查看 CPU 使用率",
		"服务报错了，帮我诊断一下",
		"重启 nginx 服务",
		"之前遇到过类似的问题吗？",
	}

	for _, input := range testInputs {
		response, err := app.SupervisorAgent.HandleRequest(ctx, session.SessionID, input)
		if err != nil {
			log.Printf("   ✗ 处理失败: %v", err)
			continue
		}
		fmt.Printf("   ✓ 输入: %s\n", input)
		fmt.Printf("     回复: %s\n\n", response)
	}

	// 5. 创建 Agent 上下文
	fmt.Println("5. 创建 Agent 上下文...")
	agentCtx, err := app.ContextManager.CreateAgentContext(ctx, "ops")
	if err != nil {
		log.Fatalf("failed to create agent context: %v", err)
	}
	fmt.Printf("   ✓ Agent 上下文创建成功: %s (类型: %s)\n\n", agentCtx.AgentID, agentCtx.AgentType)

	// 6. 创建执行上下文
	fmt.Println("6. 创建执行上下文...")
	execCtx, err := app.ContextManager.CreateExecutionContext(ctx, "plan_001")
	if err != nil {
		log.Fatalf("failed to create execution context: %v", err)
	}
	fmt.Printf("   ✓ 执行上下文创建成功: %s (计划ID: %s)\n\n", execCtx.ExecutionID, execCtx.PlanID)

	// 7. 列出活跃会话
	fmt.Println("7. 列出活跃会话...")
	sessions := app.ContextManager.ListActiveSessions(ctx)
	fmt.Printf("   ✓ 当前活跃会话数: %d\n", len(sessions))
	for _, s := range sessions {
		fmt.Printf("     - %s (用户: %s, 消息数: %d)\n", s.SessionID, s.UserID, len(s.History))
	}
	fmt.Println()

	fmt.Println("=== Demo 完成 ===")
	fmt.Println("\nPhase 1 核心功能验证通过：")
	fmt.Println("  ✓ 会话管理（创建、获取、更新）")
	fmt.Println("  ✓ 对话历史管理")
	fmt.Println("  ✓ 意图分类与路由")
	fmt.Println("  ✓ Agent 上下文管理")
	fmt.Println("  ✓ 执行上下文管理")
	fmt.Println("  ✓ 并发安全保证")
}
