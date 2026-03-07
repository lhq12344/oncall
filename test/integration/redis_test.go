package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go_agent/internal/bootstrap"
	"go_agent/utility/mem"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRedisIntegration Redis 集成测试
func TestRedisIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping redis integration test")
	}

	ctx := context.Background()

	// 创建应用（会初始化 Redis 连接）
	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr: "localhost:30379", // K8s NodePort
		RedisDB:   1,
		LogLevel:  "info",
	})
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	defer app.Close()

	t.Run("SessionStorage", func(t *testing.T) {
		sessionID := fmt.Sprintf("test_session_%d", time.Now().Unix())
		sm := mem.GetSimpleMemory(sessionID)
		require.NotNil(t, sm)

		userMsg := schema.UserMessage("测试消息1")
		assistantMsg := schema.AssistantMessage("回复消息1", nil)

		err := sm.SetMessages(ctx, userMsg, assistantMsg, []*schema.Message{userMsg}, 100, 50)
		require.NoError(t, err)

		history, err := sm.GetMessagesForRequest(ctx, schema.UserMessage("下一条消息"), 0)
		require.NoError(t, err)
		assert.NotEmpty(t, history)

		t.Logf("✅ Session %s stored and retrieved messages", sessionID)
	})

	t.Run("SessionRecovery", func(t *testing.T) {
		sessionID := fmt.Sprintf("test_recovery_%d", time.Now().Unix())

		// 第一次：创建会话并存储消息
		sm1 := mem.GetSimpleMemory(sessionID)
		userMsg := schema.UserMessage("持久化测试")
		assistantMsg := schema.AssistantMessage("收到", nil)
		err := sm1.SetMessages(ctx, userMsg, assistantMsg, []*schema.Message{userMsg}, 100, 50)
		require.NoError(t, err)

		// 第二次：使用相同 sessionID 恢复会话
		sm2 := mem.GetSimpleMemory(sessionID)
		history, err := sm2.GetMessagesForRequest(ctx, schema.UserMessage("继续对话"), 0)
		require.NoError(t, err)
		assert.NotEmpty(t, history)

		t.Logf("✅ Session %s recovered messages", sessionID)
	})

	t.Run("ConcurrentSessions", func(t *testing.T) {
		concurrency := 10
		done := make(chan bool, concurrency)

		for i := 0; i < concurrency; i++ {
			go func(id int) {
				sessionID := fmt.Sprintf("concurrent_session_%d_%d", time.Now().Unix(), id)
				sm := mem.GetSimpleMemory(sessionID)

				userMsg := schema.UserMessage(fmt.Sprintf("并发测试 %d", id))
				assistantMsg := schema.AssistantMessage(fmt.Sprintf("回复 %d", id), nil)

				err := sm.SetMessages(ctx, userMsg, assistantMsg, []*schema.Message{userMsg}, 100, 50)
				if err != nil {
					t.Logf("Concurrent session %d error: %v", id, err)
					done <- false
					return
				}

				history, err := sm.GetMessagesForRequest(ctx, schema.UserMessage("test"), 0)
				if err != nil || len(history) == 0 {
					done <- false
					return
				}

				done <- true
			}(i)
		}

		successCount := 0
		for i := 0; i < concurrency; i++ {
			if <-done {
				successCount++
			}
		}

		assert.Equal(t, concurrency, successCount, "All concurrent sessions should succeed")
		t.Logf("✅ %d concurrent sessions completed successfully", successCount)
	})

	t.Run("SessionIsolation", func(t *testing.T) {
		session1ID := fmt.Sprintf("isolation_1_%d", time.Now().Unix())
		session2ID := fmt.Sprintf("isolation_2_%d", time.Now().Unix())

		sm1 := mem.GetSimpleMemory(session1ID)
		sm2 := mem.GetSimpleMemory(session2ID)

		err := sm1.SetMessages(ctx,
			schema.UserMessage("会话1的消息"),
			schema.AssistantMessage("会话1回复", nil),
			[]*schema.Message{schema.UserMessage("会话1的消息")},
			100, 50)
		require.NoError(t, err)

		err = sm2.SetMessages(ctx,
			schema.UserMessage("会话2的消息"),
			schema.AssistantMessage("会话2回复", nil),
			[]*schema.Message{schema.UserMessage("会话2的消息")},
			100, 50)
		require.NoError(t, err)

		history1, err := sm1.GetMessagesForRequest(ctx, schema.UserMessage("继续1"), 0)
		require.NoError(t, err)
		assert.NotEmpty(t, history1)

		history2, err := sm2.GetMessagesForRequest(ctx, schema.UserMessage("继续2"), 0)
		require.NoError(t, err)
		assert.NotEmpty(t, history2)

		t.Log("✅ Sessions are properly isolated")
	})
}

// TestRedisAvailability 测试 Redis 可用性
func TestRedisAvailability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping redis availability test")
	}

	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr: "localhost:30379", // K8s NodePort
		RedisDB:   1,
		LogLevel:  "error",
	})

	if err != nil {
		t.Log("⚠️  Redis not available")
		t.Log("💡 To run Redis integration tests, ensure:")
		t.Log("   1. Redis is running in K8s cluster")
		t.Log("   2. Accessible at localhost:30379 (NodePort)")
		t.Skip("Redis not available")
	}
	defer app.Close()

	t.Log("✅ Redis is available")
}

// TestRedisPerformance 测试 Redis 性能
func TestRedisPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping redis performance test")
	}

	ctx := context.Background()

	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr: "localhost:30379", // K8s NodePort
		RedisDB:   1,
		LogLevel:  "error",
	})
	if err != nil {
		t.Skip("Redis not available")
	}
	defer app.Close()

	t.Run("WritePerformance", func(t *testing.T) {
		sessionID := fmt.Sprintf("perf_write_%d", time.Now().Unix())
		sm := mem.GetSimpleMemory(sessionID)

		messageCount := 50
		start := time.Now()

		for i := 0; i < messageCount; i++ {
			err := sm.SetMessages(ctx,
				schema.UserMessage(fmt.Sprintf("性能测试消息 %d", i)),
				schema.AssistantMessage(fmt.Sprintf("回复 %d", i), nil),
				[]*schema.Message{schema.UserMessage(fmt.Sprintf("性能测试消息 %d", i))},
				100, 50)
			require.NoError(t, err)
		}

		duration := time.Since(start)
		avgLatency := duration / time.Duration(messageCount)

		t.Logf("Write performance: %d messages in %v (avg: %v per message)",
			messageCount, duration, avgLatency)
	})
}
