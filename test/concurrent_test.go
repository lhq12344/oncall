package test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"go_agent/internal/concurrent"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestExecutorParallel 测试并行执行器
func TestExecutorParallel(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	t.Run("基本并行执行", func(t *testing.T) {
		executor := concurrent.NewExecutor(&concurrent.ExecutorConfig{
			MaxConcurrency: 3,
			Timeout:        5 * time.Second,
			Logger:         logger,
		})

		tasks := []concurrent.TaskFunc{
			func(ctx context.Context) (interface{}, error) {
				time.Sleep(100 * time.Millisecond)
				return "task1", nil
			},
			func(ctx context.Context) (interface{}, error) {
				time.Sleep(100 * time.Millisecond)
				return "task2", nil
			},
			func(ctx context.Context) (interface{}, error) {
				time.Sleep(100 * time.Millisecond)
				return "task3", nil
			},
		}

		start := time.Now()
		results := executor.ExecuteParallel(context.Background(), tasks)
		duration := time.Since(start)

		assert.Len(t, results, 3)
		assert.Less(t, duration, 500*time.Millisecond, "应该并行执行")

		for i, result := range results {
			assert.NoError(t, result.Error)
			assert.Equal(t, fmt.Sprintf("task%d", i+1), result.Result)
		}
	})

	t.Run("超时控制", func(t *testing.T) {
		executor := concurrent.NewExecutor(&concurrent.ExecutorConfig{
			MaxConcurrency: 2,
			Timeout:        200 * time.Millisecond,
			Logger:         logger,
		})

		tasks := []concurrent.TaskFunc{
			func(ctx context.Context) (interface{}, error) {
				select {
				case <-time.After(1 * time.Second):
					return "should timeout", nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		}

		results := executor.ExecuteParallel(context.Background(), tasks)
		assert.Len(t, results, 1)
		assert.Error(t, results[0].Error)
	})

	t.Run("部分失败", func(t *testing.T) {
		executor := concurrent.NewExecutor(&concurrent.ExecutorConfig{
			MaxConcurrency: 3,
			Timeout:        5 * time.Second,
			Logger:         logger,
		})

		tasks := []concurrent.TaskFunc{
			func(ctx context.Context) (interface{}, error) {
				return "success", nil
			},
			func(ctx context.Context) (interface{}, error) {
				return nil, errors.New("task failed")
			},
			func(ctx context.Context) (interface{}, error) {
				return "success", nil
			},
		}

		results := executor.ExecuteParallel(context.Background(), tasks)
		assert.Len(t, results, 3)
		assert.NoError(t, results[0].Error)
		assert.Error(t, results[1].Error)
		assert.NoError(t, results[2].Error)
	})

	t.Run("并发限制", func(t *testing.T) {
		executor := concurrent.NewExecutor(&concurrent.ExecutorConfig{
			MaxConcurrency: 2,
			Timeout:        10 * time.Second,
			Logger:         logger,
		})

		concurrentCount := 0
		maxConcurrent := 0
		var mu = make(chan struct{}, 1)

		tasks := make([]concurrent.TaskFunc, 5)
		for i := 0; i < 5; i++ {
			tasks[i] = func(ctx context.Context) (interface{}, error) {
				mu <- struct{}{}
				concurrentCount++
				if concurrentCount > maxConcurrent {
					maxConcurrent = concurrentCount
				}
				<-mu

				time.Sleep(100 * time.Millisecond)

				mu <- struct{}{}
				concurrentCount--
				<-mu

				return "done", nil
			}
		}

		results := executor.ExecuteParallel(context.Background(), tasks)
		assert.Len(t, results, 5)
		assert.LessOrEqual(t, maxConcurrent, 2, "并发数不应超过限制")
	})
}

// TestCircuitBreaker 测试熔断器
func TestCircuitBreaker(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	t.Run("正常执行", func(t *testing.T) {
		cb := concurrent.NewCircuitBreaker(&concurrent.CircuitBreakerConfig{
			Name:   "test",
			Logger: logger,
		})

		result, err := cb.Execute(context.Background(), func(ctx context.Context) (interface{}, error) {
			return "success", nil
		})

		assert.NoError(t, err)
		assert.Equal(t, "success", result)
		assert.Equal(t, concurrent.StateClosed, cb.State())
	})

	t.Run("连续失败触发熔断", func(t *testing.T) {
		cb := concurrent.NewCircuitBreaker(&concurrent.CircuitBreakerConfig{
			Name:             "test_failure",
			FailureThreshold: 3,
			MinRequestCount:  3,
			Logger:           logger,
		})

		// 执行 3 次失败
		for i := 0; i < 3; i++ {
			_, err := cb.Execute(context.Background(), func(ctx context.Context) (interface{}, error) {
				return nil, errors.New("failed")
			})
			assert.Error(t, err)
		}

		// 熔断器应该打开
		assert.Equal(t, concurrent.StateOpen, cb.State())

		// 再次执行应该直接返回熔断错误
		_, err := cb.Execute(context.Background(), func(ctx context.Context) (interface{}, error) {
			return "should not execute", nil
		})
		assert.ErrorIs(t, err, concurrent.ErrCircuitOpen)
	})

	t.Run("失败率触发熔断", func(t *testing.T) {
		cb := concurrent.NewCircuitBreaker(&concurrent.CircuitBreakerConfig{
			Name:            "test_ratio",
			FailureRatio:    0.5,
			MinRequestCount: 10,
			Logger:          logger,
		})

		// 执行 10 次，先 4 次成功，然后 6 次失败（60% 失败率）
		// 这样最后一次是失败，会触发熔断检查
		for i := 0; i < 10; i++ {
			_, _ = cb.Execute(context.Background(), func(ctx context.Context) (interface{}, error) {
				if i < 4 {
					return "success", nil
				}
				return nil, errors.New("failed")
			})
		}

		// 熔断器应该打开（60% > 50% 阈值）
		state := cb.State()
		assert.Equal(t, concurrent.StateOpen, state, "失败率超过阈值应该触发熔断")
	})

	t.Run("半开状态恢复", func(t *testing.T) {
		cb := concurrent.NewCircuitBreaker(&concurrent.CircuitBreakerConfig{
			Name:             "test_recovery",
			FailureThreshold: 2,
			MinRequestCount:  2,
			Timeout:          100 * time.Millisecond,
			MaxRequests:      2,
			Logger:           logger,
		})

		// 触发熔断
		for i := 0; i < 2; i++ {
			_, _ = cb.Execute(context.Background(), func(ctx context.Context) (interface{}, error) {
				return nil, errors.New("failed")
			})
		}
		assert.Equal(t, concurrent.StateOpen, cb.State())

		// 等待超时，进入半开状态
		time.Sleep(150 * time.Millisecond)

		// 执行成功请求
		for i := 0; i < 2; i++ {
			_, err := cb.Execute(context.Background(), func(ctx context.Context) (interface{}, error) {
				return "success", nil
			})
			assert.NoError(t, err)
		}

		// 应该恢复到关闭状态
		assert.Equal(t, concurrent.StateClosed, cb.State())
	})

	t.Run("统计信息", func(t *testing.T) {
		cb := concurrent.NewCircuitBreaker(&concurrent.CircuitBreakerConfig{
			Name:   "test_stats",
			Logger: logger,
		})

		// 执行一些请求
		for i := 0; i < 5; i++ {
			_, _ = cb.Execute(context.Background(), func(ctx context.Context) (interface{}, error) {
				if i%2 == 0 {
					return "success", nil
				}
				return nil, errors.New("failed")
			})
		}

		requests, successes, failures := cb.Counts()
		assert.Equal(t, uint32(5), requests)
		assert.Equal(t, uint32(3), successes)
		assert.Equal(t, uint32(2), failures)
	})
}
