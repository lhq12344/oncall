package concurrent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// TaskFunc 任务执行函数
type TaskFunc func(ctx context.Context) (interface{}, error)

// TaskResult 任务执行结果
type TaskResult struct {
	Index  int         // 任务索引
	Result interface{} // 执行结果
	Error  error       // 错误信息
}

// ExecutorConfig 执行器配置
type ExecutorConfig struct {
	MaxConcurrency int           // 最大并发数
	Timeout        time.Duration // 超时时间
	Logger         *zap.Logger   // 日志记录器
}

// Executor 并发执行器
type Executor struct {
	config *ExecutorConfig
	logger *zap.Logger
}

// NewExecutor 创建并发执行器
func NewExecutor(config *ExecutorConfig) *Executor {
	if config == nil {
		config = &ExecutorConfig{
			MaxConcurrency: 10,
			Timeout:        30 * time.Second,
		}
	}

	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = 10
	}

	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}

	return &Executor{
		config: config,
		logger: config.Logger,
	}
}

// ExecuteParallel 并行执行多个任务
func (e *Executor) ExecuteParallel(ctx context.Context, tasks []TaskFunc) []TaskResult {
	if len(tasks) == 0 {
		return []TaskResult{}
	}

	// 创建带超时的上下文
	timeoutCtx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()

	results := make([]TaskResult, len(tasks))
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, e.config.MaxConcurrency)

	for i, task := range tasks {
		wg.Add(1)
		go func(index int, taskFunc TaskFunc) {
			defer wg.Done()

			// 获取信号量
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-timeoutCtx.Done():
				results[index] = TaskResult{
					Index: index,
					Error: fmt.Errorf("task %d: timeout acquiring semaphore", index),
				}
				return
			}

			// 执行任务
			result, err := taskFunc(timeoutCtx)
			results[index] = TaskResult{
				Index:  index,
				Result: result,
				Error:  err,
			}

			if err != nil && e.logger != nil {
				e.logger.Warn("task execution failed",
					zap.Int("task_index", index),
					zap.Error(err))
			}
		}(i, task)
	}

	wg.Wait()
	return results
}
