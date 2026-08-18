package context

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestContextManager(t *testing.T) {
	// 创建 Redis 客户端（测试环境）
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // 使用测试数据库
	})

	// 创建存储层
	storage := NewRedisStorage(redisClient, "oncall:test")

	// 创建上下文管理器
	cm := NewContextManager(storage)

	ctx := context.Background()

	// 测试创建会话
	t.Run("CreateSession", func(t *testing.T) {
		session, err := cm.CreateSession(ctx, "user123")
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}

		if session.SessionID == "" {
			t.Error("session ID is empty")
		}

		if session.UserID != "user123" {
			t.Errorf("expected user ID 'user123', got '%s'", session.UserID)
		}

		t.Logf("created session: %s", session.SessionID)
	})

	// 测试获取会话
	t.Run("GetSession", func(t *testing.T) {
		session, err := cm.CreateSession(ctx, "user456")
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}

		retrieved, err := cm.GetSession(ctx, session.SessionID)
		if err != nil {
			t.Fatalf("failed to get session: %v", err)
		}

		if retrieved.SessionID != session.SessionID {
			t.Errorf("expected session ID '%s', got '%s'", session.SessionID, retrieved.SessionID)
		}

		t.Logf("retrieved session: %s", retrieved.SessionID)
	})

	// 测试添加消息
	t.Run("AddMessage", func(t *testing.T) {
		session, err := cm.CreateSession(ctx, "user789")
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}

		err = cm.AddMessage(ctx, session.SessionID, "user", "Hello, world!")
		if err != nil {
			t.Fatalf("failed to add message: %v", err)
		}

		history, err := cm.GetHistory(ctx, session.SessionID, 10)
		if err != nil {
			t.Fatalf("failed to get history: %v", err)
		}

		if len(history) != 1 {
			t.Errorf("expected 1 message, got %d", len(history))
		}

		if history[0].Content != "Hello, world!" {
			t.Errorf("expected content 'Hello, world!', got '%s'", history[0].Content)
		}

		t.Logf("added message to session: %s", session.SessionID)
	})

	// 测试 Agent 上下文
	t.Run("CreateAgentContext", func(t *testing.T) {
		agentCtx, err := cm.CreateAgentContext(ctx, "supervisor")
		if err != nil {
			t.Fatalf("failed to create agent context: %v", err)
		}

		if agentCtx.AgentID == "" {
			t.Error("agent ID is empty")
		}

		if agentCtx.AgentType != "supervisor" {
			t.Errorf("expected agent type 'supervisor', got '%s'", agentCtx.AgentType)
		}

		t.Logf("created agent context: %s", agentCtx.AgentID)
	})

	// 测试执行上下文
	t.Run("CreateExecutionContext", func(t *testing.T) {
		execCtx, err := cm.CreateExecutionContext(ctx, "plan123")
		if err != nil {
			t.Fatalf("failed to create execution context: %v", err)
		}

		if execCtx.ExecutionID == "" {
			t.Error("execution ID is empty")
		}

		if execCtx.PlanID != "plan123" {
			t.Errorf("expected plan ID 'plan123', got '%s'", execCtx.PlanID)
		}

		t.Logf("created execution context: %s", execCtx.ExecutionID)
	})

	// 测试数据迁移
	t.Run("MigrateToL2", func(t *testing.T) {
		// 创建一个旧会话
		session, err := cm.CreateSession(ctx, "olduser")
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}

		// 手动设置为不活跃
		session.LastActive = time.Now().Add(-1 * time.Hour)
		cm.UpdateSession(ctx, session)

		// 执行迁移
		err = cm.MigrateToL2(ctx)
		if err != nil {
			t.Fatalf("failed to migrate: %v", err)
		}

		t.Logf("migrated inactive sessions to L2")
	})
}
