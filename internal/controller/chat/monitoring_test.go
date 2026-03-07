package chat

import (
	"context"
	"testing"

	v1 "go_agent/api/chat/v1"
	"go_agent/internal/concurrent"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestMonitoringEndpoint(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// 创建 mock Redis 客户端（可选）
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	// 创建 controller（不需要真实的 supervisor agent）
	ctrl := NewV1(nil, logger, redisClient, nil, nil, nil, nil)

	// 测试监控端点
	req := &v1.MonitoringReq{}
	res, err := ctrl.Monitoring(context.Background(), req)

	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, float64(0), res.CacheHitRate) // 初始命中率为 0
	assert.Equal(t, int64(0), res.CacheHits)
	assert.Equal(t, int64(0), res.CacheMisses)
	assert.NotNil(t, res.CircuitBreakers)
}

func TestMonitoringWithCircuitBreakers(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	ctrl := NewV1(nil, logger, nil, nil, nil, nil, nil)

	// 创建一些熔断器
	cb1 := ctrl.cbManager.GetOrCreate("test-cb-1", &concurrent.CircuitBreakerConfig{
		FailureThreshold: 3,
		Logger:           logger,
	})
	cb2 := ctrl.cbManager.GetOrCreate("test-cb-2", &concurrent.CircuitBreakerConfig{
		FailureThreshold: 5,
		Logger:           logger,
	})

	// 模拟一些请求
	ctx := context.Background()
	_, _ = cb1.Execute(ctx, func(ctx context.Context) (interface{}, error) {
		return "success", nil
	})
	_, _ = cb2.Execute(ctx, func(ctx context.Context) (interface{}, error) {
		return "success", nil
	})

	// 测试监控端点
	req := &v1.MonitoringReq{}
	res, err := ctrl.Monitoring(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.Len(t, res.CircuitBreakers, 2)

	// 验证熔断器状态
	for _, cb := range res.CircuitBreakers {
		assert.Contains(t, []string{"test-cb-1", "test-cb-2"}, cb.Name)
		assert.Equal(t, "closed", cb.State)
		assert.Greater(t, cb.Requests, uint32(0))
	}
}
