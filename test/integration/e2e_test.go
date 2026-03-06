package integration

import (
	"context"
	"testing"
	"time"

	"go_agent/internal/bootstrap"

	"github.com/redis/go-redis/v9"
)

// TestEndToEnd_KnowledgeSearch 端到端测试：知识检索
func TestEndToEnd_KnowledgeSearch(t *testing.T) {
	// 跳过测试（需要 Redis）
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// 创建应用
	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr: "localhost:6379",
		RedisDB:   1,
		LogLevel:  "info",
	})
	if err != nil {
		t.Fatalf("failed to create application: %v", err)
	}
	defer app.Close()

	ctx := context.Background()

	// 创建会话
	session, err := app.ContextManager.CreateSession(ctx, "test_user")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	t.Logf("Created session: %s", session.SessionID)

	// 测试知识检索流程
	userInput := "之前遇到过 pod 启动失败的问题吗？"

	// 1. 添加用户消息
	err = app.ContextManager.AddMessage(ctx, session.SessionID, "user", userInput)
	if err != nil {
		t.Fatalf("failed to add message: %v", err)
	}

	// 2. Supervisor 处理请求
	response, err := app.SupervisorAgent.HandleRequest(ctx, session.SessionID, userInput)
	if err != nil {
		t.Fatalf("failed to handle request: %v", err)
	}

	t.Logf("Response: %s", response)

	// 3. 验证会话状态
	updatedSession, err := app.ContextManager.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}

	if updatedSession.Intent == nil {
		t.Error("expected intent to be set")
	} else {
		t.Logf("Intent: type=%s, confidence=%.2f",
			updatedSession.Intent.Type, updatedSession.Intent.Confidence)
	}

	t.Log("✓ Knowledge search flow completed")
}

// TestEndToEnd_MonitoringFlow 端到端测试：监控查询流程
func TestEndToEnd_MonitoringFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr: "localhost:6379",
		RedisDB:   1,
		LogLevel:  "info",
	})
	if err != nil {
		t.Fatalf("failed to create application: %v", err)
	}
	defer app.Close()

	ctx := context.Background()

	// 创建会话
	session, err := app.ContextManager.CreateSession(ctx, "test_user")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// 测试监控查询流程
	inputs := []string{
		"查看 pod 状态",
		"有什么异常吗？",
		"CPU 使用率怎么样？",
	}

	for i, input := range inputs {
		t.Logf("Round %d: %s", i+1, input)

		// 处理请求
		response, err := app.SupervisorAgent.HandleRequest(ctx, session.SessionID, input)
		if err != nil {
			t.Fatalf("failed to handle request: %v", err)
		}

		t.Logf("Response: %s", response)

		// 短暂延迟
		time.Sleep(100 * time.Millisecond)
	}

	// 验证对话历史
	history, err := app.ContextManager.GetHistory(ctx, session.SessionID, 10)
	if err != nil {
		t.Fatalf("failed to get history: %v", err)
	}

	expectedCount := len(inputs) * 2 // user + assistant
	if len(history) != expectedCount {
		t.Errorf("expected %d messages, got %d", expectedCount, len(history))
	}

	t.Log("✓ Monitoring flow completed")
}

// TestEndToEnd_DialogueFlow 端到端测试：对话流程
func TestEndToEnd_DialogueFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr: "localhost:6379",
		RedisDB:   1,
		LogLevel:  "info",
	})
	if err != nil {
		t.Fatalf("failed to create application: %v", err)
	}
	defer app.Close()

	ctx := context.Background()

	// 创建会话
	session, err := app.ContextManager.CreateSession(ctx, "test_user")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// 测试对话流程（意图逐渐明确）
	inputs := []string{
		"有问题",                    // 意图模糊
		"服务报错了",                  // 意图逐渐明确
		"nginx 服务一直重启",           // 意图明确
	}

	for i, input := range inputs {
		t.Logf("Round %d: %s", i+1, input)

		// 分析意图
		analysis, err := app.DialogueAgent.AnalyzeIntent(ctx, session.SessionID, input)
		if err != nil {
			t.Fatalf("failed to analyze intent: %v", err)
		}

		t.Logf("Intent: type=%s, confidence=%.2f, entropy=%.2f, converged=%v",
			analysis.Intent.Type,
			analysis.Confidence,
			analysis.Entropy,
			analysis.Converged,
		)

		// 如果意图收敛，预测候选问题
		if analysis.Converged {
			questions, err := app.DialogueAgent.PredictNextQuestions(ctx, session.SessionID, 3)
			if err != nil {
				t.Fatalf("failed to predict questions: %v", err)
			}

			t.Logf("Predicted questions:")
			for j, q := range questions {
				t.Logf("  %d. %s", j+1, q)
			}
		}

		// 添加消息
		app.ContextManager.AddMessage(ctx, session.SessionID, "user", input)
		app.ContextManager.AddMessage(ctx, session.SessionID, "assistant", "正在处理...")

		time.Sleep(100 * time.Millisecond)
	}

	t.Log("✓ Dialogue flow completed")
}

// TestEndToEnd_MultiAgentCollaboration 端到端测试：多 Agent 协作
func TestEndToEnd_MultiAgentCollaboration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr: "localhost:6379",
		RedisDB:   1,
		LogLevel:  "info",
	})
	if err != nil {
		t.Fatalf("failed to create application: %v", err)
	}
	defer app.Close()

	ctx := context.Background()

	// 创建会话
	session, err := app.ContextManager.CreateSession(ctx, "test_user")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// 模拟完整的故障诊断流程
	t.Log("=== 故障诊断流程 ===")

	// 1. 用户报告问题
	userInput := "nginx 服务响应很慢"
	t.Logf("1. 用户输入: %s", userInput)

	// 2. Dialogue Agent 分析意图
	analysis, err := app.DialogueAgent.AnalyzeIntent(ctx, session.SessionID, userInput)
	if err != nil {
		t.Fatalf("failed to analyze intent: %v", err)
	}
	t.Logf("2. 意图分析: type=%s, confidence=%.2f", analysis.Intent.Type, analysis.Confidence)

	// 3. Dialogue Agent 提取实体
	entities, err := app.DialogueAgent.ExtractEntities(ctx, userInput)
	if err != nil {
		t.Fatalf("failed to extract entities: %v", err)
	}
	t.Logf("3. 实体提取: %+v", entities)

	// 4. Ops Agent 监控 K8s
	k8sResult, err := app.OpsAgent.MonitorK8s(ctx, "default", "deployment", "nginx")
	if err != nil {
		t.Logf("4. K8s 监控失败 (expected in test): %v", err)
	} else {
		t.Logf("4. K8s 监控: healthy=%v", k8sResult.Healthy)
	}

	// 5. Ops Agent 采集指标
	metricsResult, err := app.OpsAgent.CollectMetrics(ctx, "rate(cpu[5m])", "5m")
	if err != nil {
		t.Logf("5. 指标采集失败 (expected in test): %v", err)
	} else {
		t.Logf("5. 指标采集: data_points=%d, anomalies=%d",
			len(metricsResult.Data), len(metricsResult.Anomalies))
	}

	// 6. Knowledge Agent 检索历史案例
	// (需要 Milvus，这里跳过)
	t.Log("6. 知识检索: (skipped - requires Milvus)")

	// 7. Dialogue Agent 生成候选问题
	if analysis.Converged {
		questions, err := app.DialogueAgent.PredictNextQuestions(ctx, session.SessionID, 3)
		if err != nil {
			t.Fatalf("failed to predict questions: %v", err)
		}
		t.Logf("7. 候选问题:")
		for i, q := range questions {
			t.Logf("   %d. %s", i+1, q)
		}
	}

	t.Log("✓ Multi-agent collaboration completed")
}

// TestEndToEnd_ContextIsolation 端到端测试：上下文隔离
func TestEndToEnd_ContextIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr: "localhost:6379",
		RedisDB:   1,
		LogLevel:  "info",
	})
	if err != nil {
		t.Fatalf("failed to create application: %v", err)
	}
	defer app.Close()

	ctx := context.Background()

	// 创建两个独立的会话
	session1, _ := app.ContextManager.CreateSession(ctx, "user1")
	session2, _ := app.ContextManager.CreateSession(ctx, "user2")

	t.Logf("Session 1: %s", session1.SessionID)
	t.Logf("Session 2: %s", session2.SessionID)

	// 会话 1：监控查询
	app.ContextManager.AddMessage(ctx, session1.SessionID, "user", "查看 pod 状态")
	app.DialogueAgent.AnalyzeIntent(ctx, session1.SessionID, "查看 pod 状态")

	// 会话 2：故障诊断
	app.ContextManager.AddMessage(ctx, session2.SessionID, "user", "服务报错了")
	app.DialogueAgent.AnalyzeIntent(ctx, session2.SessionID, "服务报错了")

	// 验证会话隔离
	s1, _ := app.ContextManager.GetSession(ctx, session1.SessionID)
	s2, _ := app.ContextManager.GetSession(ctx, session2.SessionID)

	if s1.Intent.Type == s2.Intent.Type {
		t.Error("expected different intent types for isolated sessions")
	}

	t.Logf("Session 1 intent: %s", s1.Intent.Type)
	t.Logf("Session 2 intent: %s", s2.Intent.Type)

	if len(s1.History) != 1 || len(s2.History) != 1 {
		t.Error("expected 1 message in each session")
	}

	t.Log("✓ Context isolation verified")
}

// TestEndToEnd_ToolRegistration 端到端测试：工具注册
func TestEndToEnd_ToolRegistration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr: "localhost:6379",
		RedisDB:   1,
		LogLevel:  "info",
	})
	if err != nil {
		t.Fatalf("failed to create application: %v", err)
	}
	defer app.Close()

	// 验证工具注册
	tools := app.SupervisorAgent.GetTools()

	expectedToolCount := 13 // 3 + 5 + 5
	if len(tools) != expectedToolCount {
		t.Errorf("expected %d tools, got %d", expectedToolCount, len(tools))
	}

	t.Logf("Registered tools: %d", len(tools))

	// 列出所有工具
	ctx := context.Background()
	for i, tool := range tools {
		info, err := tool.Info(ctx)
		if err != nil {
			t.Logf("  %d. (error getting info: %v)", i+1, err)
		} else {
			t.Logf("  %d. %s - %s", i+1, info.Name, info.Desc)
		}
	}

	t.Log("✓ Tool registration verified")
}

// BenchmarkEndToEnd_HandleRequest 性能测试：请求处理
func BenchmarkEndToEnd_HandleRequest(b *testing.B) {
	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr: "localhost:6379",
		RedisDB:   1,
		LogLevel:  "error",
	})
	if err != nil {
		b.Fatalf("failed to create application: %v", err)
	}
	defer app.Close()

	ctx := context.Background()
	session, _ := app.ContextManager.CreateSession(ctx, "bench_user")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := app.SupervisorAgent.HandleRequest(ctx, session.SessionID, "查看 pod 状态")
		if err != nil {
			b.Fatalf("failed to handle request: %v", err)
		}
	}
}

// TestRedisConnection 测试 Redis 连接
func TestRedisConnection(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1,
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Ping(ctx).Err()
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	t.Log("✓ Redis connection successful")
}
