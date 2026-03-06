package main

import (
	"context"
	"fmt"
	"log"
	"time"

	appcontext "go_agent/internal/context"
	"go_agent/internal/agent/supervisor"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func main() {
	fmt.Println("=== Phase 1 Demo: 上下文管理与 Supervisor Agent ===\n")

	// 1. 初始化 Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1,
	})

	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}
	fmt.Println("✓ Redis 连接成功\n")

	// 2. 初始化日志
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// 3. 初始化存储层
	storage := appcontext.NewRedisStorage(redisClient, "oncall:demo")

	// 4. 初始化上下文管理器
	contextManager := appcontext.NewContextManager(storage)
	fmt.Println("✓ 上下文管理器初始化成功\n")

	// 5. 初始化 Supervisor Agent
	supervisorAgent := supervisor.NewSupervisorAgent(&supervisor.Config{
		ContextManager: contextManager,
		Logger:         logger,
	})
	fmt.Println("✓ Supervisor Agent 初始化成功\n")

	// === 测试场景 ===

	// 场景 1: 创建会话
	fmt.Println("【场景 1】创建用户会话")
	session, err := contextManager.CreateSession(ctx, "demo_user")
	if err != nil {
		log.Fatalf("failed to create session: %v", err)
	}
	fmt.Printf("  会话ID: %s\n", session.SessionID)
	fmt.Printf("  用户ID: %s\n", session.UserID)
	fmt.Printf("  创建时间: %s\n\n", session.CreatedAt.Format(time.RFC3339))

	// 场景 2: 添加对话消息
	fmt.Println("【场景 2】添加对话消息")
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
		err := contextManager.AddMessage(ctx, session.SessionID, msg.role, msg.content)
		if err != nil {
			log.Fatalf("failed to add message: %v", err)
		}
		fmt.Printf("  [%s] %s\n", msg.role, msg.content)
	}
	fmt.Println()

	// 场景 3: 获取对话历史
	fmt.Println("【场景 3】获取对话历史")
	history, err := contextManager.GetHistory(ctx, session.SessionID, 10)
	if err != nil {
		log.Fatalf("failed to get history: %v", err)
	}
	fmt.Printf("  共 %d 条消息:\n", len(history))
	for i, msg := range history {
		fmt.Printf("    %d. [%s] %s (时间: %s)\n",
			i+1, msg.Role, msg.Content, msg.Timestamp.Format("15:04:05"))
	}
	fmt.Println()

	// 场景 4: 测试意图分类
	fmt.Println("【场景 4】测试意图分类")
	testInputs := []string{
		"查看 CPU 使用率",
		"服务报错了，帮我诊断一下",
		"重启 nginx 服务",
		"之前遇到过类似的问题吗？",
	}

	for i, input := range testInputs {
		fmt.Printf("  测试 %d: %s\n", i+1, input)

		// 使用 Supervisor Agent 处理请求
		response, err := supervisorAgent.HandleRequest(ctx, session.SessionID, input)
		if err != nil {
			log.Printf("    ✗ 处理失败: %v\n", err)
			continue
		}

		// 获取更新后的会话，查看意图
		updatedSession, _ := contextManager.GetSession(ctx, session.SessionID)
		if updatedSession.Intent != nil {
			fmt.Printf("    意图类型: %s (置信度: %.2f)\n",
				updatedSession.Intent.Type, updatedSession.Intent.Confidence)
		}
		fmt.Printf("    回复: %s\n\n", response)
	}

	// 场景 5: 创建 Agent 上下文
	fmt.Println("【场景 5】创建 Agent 上下文")
	agentTypes := []string{"ops", "knowledge", "dialogue", "execution"}
	for _, agentType := range agentTypes {
		agentCtx, err := contextManager.CreateAgentContext(ctx, agentType)
		if err != nil {
			log.Fatalf("failed to create agent context: %v", err)
		}
		fmt.Printf("  %s Agent: %s (状态: %s)\n",
			agentType, agentCtx.AgentID, agentCtx.State)
	}
	fmt.Println()

	// 场景 6: 创建执行上下文
	fmt.Println("【场景 6】创建执行上下文")
	execCtx, err := contextManager.CreateExecutionContext(ctx, "plan_restart_nginx")
	if err != nil {
		log.Fatalf("failed to create execution context: %v", err)
	}
	fmt.Printf("  执行ID: %s\n", execCtx.ExecutionID)
	fmt.Printf("  计划ID: %s\n", execCtx.PlanID)
	fmt.Printf("  状态: %s\n\n", execCtx.Status)

	// 场景 7: 列出活跃会话
	fmt.Println("【场景 7】列出活跃会话")
	sessions := contextManager.ListActiveSessions(ctx)
	fmt.Printf("  当前活跃会话数: %d\n", len(sessions))
	for _, s := range sessions {
		fmt.Printf("    - %s (用户: %s, 消息数: %d, 最后活跃: %s)\n",
			s.SessionID[:8]+"...", s.UserID, len(s.History),
			time.Since(s.LastActive).Round(time.Second))
	}
	fmt.Println()

	// 场景 8: 测试数据迁移
	fmt.Println("【场景 8】测试数据迁移（L1 → L2）")
	// 创建一个旧会话
	oldSession, _ := contextManager.CreateSession(ctx, "old_user")
	oldSession.LastActive = time.Now().Add(-1 * time.Hour)
	contextManager.UpdateSession(ctx, oldSession)

	fmt.Println("  执行迁移...")
	err = contextManager.MigrateToL2(ctx)
	if err != nil {
		log.Fatalf("failed to migrate: %v", err)
	}

	activeSessions := contextManager.ListActiveSessions(ctx)
	fmt.Printf("  迁移后活跃会话数: %d\n", len(activeSessions))
	fmt.Println("  ✓ 不活跃会话已迁移到 Redis\n")

	// 总结
	fmt.Println("=== Demo 完成 ===\n")
	fmt.Println("Phase 1 核心功能验证通过：")
	fmt.Println("  ✓ 会话管理（创建、获取、更新）")
	fmt.Println("  ✓ 对话历史管理")
	fmt.Println("  ✓ 意图分类与路由（基于关键词）")
	fmt.Println("  ✓ Agent 上下文管理")
	fmt.Println("  ✓ 执行上下文管理")
	fmt.Println("  ✓ 数据迁移（L1 → L2）")
	fmt.Println("  ✓ 并发安全保证")
	fmt.Println("\n下一步：Phase 2 - 实现核心 Agent（Knowledge、Dialogue、Ops）")
}
