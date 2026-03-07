package main

import (
	"context"
	"fmt"
	"time"

	"go_agent/internal/concurrent"

	"go.uber.org/zap"
)

// 演示如何在 Supervisor 中使用并发功能

func main() {
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	// 1. 创建并发执行器
	executor := concurrent.NewExecutor(&concurrent.ExecutorConfig{
		MaxConcurrency: 5,
		Timeout:        30 * time.Second,
		Logger:         logger,
	})

	// 2. 创建熔断器管理器
	cbManager := concurrent.NewCircuitBreakerManager(logger)

	// 3. 创建 Agent 执行器
	agentExecutor := concurrent.NewAgentExecutor(&concurrent.AgentExecutorConfig{
		Executor:              executor,
		CircuitBreakerManager: cbManager,
		Logger:                logger,
	})

	// 4. 创建工具执行器
	toolExecutor := concurrent.NewToolExecutor(&concurrent.ToolExecutorConfig{
		Executor:              executor,
		CircuitBreakerManager: cbManager,
		Logger:                logger,
	})

	fmt.Println("并发执行器初始化完成")
	fmt.Printf("- 最大并发数: %d\n", 5)
	fmt.Printf("- 超时时间: %v\n", 30*time.Second)
	fmt.Println()

	// 示例 1: 并行执行多个通用任务
	fmt.Println("=== 示例 1: 并行执行通用任务 ===")
	tasks := []concurrent.TaskFunc{
		func(ctx context.Context) (interface{}, error) {
			time.Sleep(100 * time.Millisecond)
			return "任务 1 完成", nil
		},
		func(ctx context.Context) (interface{}, error) {
			time.Sleep(150 * time.Millisecond)
			return "任务 2 完成", nil
		},
		func(ctx context.Context) (interface{}, error) {
			time.Sleep(120 * time.Millisecond)
			return "任务 3 完成", nil
		},
	}

	start := time.Now()
	results := executor.ExecuteParallel(ctx, tasks)
	duration := time.Since(start)

	fmt.Printf("执行时间: %v (并行执行，取最长任务时间)\n", duration)
	for i, result := range results {
		if result.Error != nil {
			fmt.Printf("任务 %d 失败: %v\n", i+1, result.Error)
		} else {
			fmt.Printf("任务 %d 成功: %v\n", i+1, result.Result)
		}
	}
	fmt.Println()

	// 示例 2: 使用熔断器保护服务
	fmt.Println("=== 示例 2: 熔断器保护 ===")
	cb := cbManager.GetOrCreate("demo_service", &concurrent.CircuitBreakerConfig{
		Name:             "demo_service",
		FailureThreshold: 3,
		Timeout:          5 * time.Second,
		Logger:           logger,
	})

	// 模拟服务调用
	for i := 0; i < 5; i++ {
		result, err := cb.Execute(ctx, func(ctx context.Context) (interface{}, error) {
			// 模拟前 3 次失败
			if i < 3 {
				return nil, fmt.Errorf("服务调用失败")
			}
			return "服务调用成功", nil
		})

		if err != nil {
			fmt.Printf("第 %d 次调用: 失败 - %v (状态: %s)\n", i+1, err, cb.State())
		} else {
			fmt.Printf("第 %d 次调用: 成功 - %v (状态: %s)\n", i+1, result, cb.State())
		}
	}

	requests, successes, failures := cb.Counts()
	fmt.Printf("统计: 总请求=%d, 成功=%d, 失败=%d\n", requests, successes, failures)
	fmt.Println()

	// 示例 3: 超时控制
	fmt.Println("=== 示例 3: 超时控制 ===")
	shortExecutor := concurrent.NewExecutor(&concurrent.ExecutorConfig{
		MaxConcurrency: 2,
		Timeout:        200 * time.Millisecond,
		Logger:         logger,
	})

	timeoutTasks := []concurrent.TaskFunc{
		func(ctx context.Context) (interface{}, error) {
			select {
			case <-time.After(100 * time.Millisecond):
				return "快速任务完成", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		func(ctx context.Context) (interface{}, error) {
			select {
			case <-time.After(500 * time.Millisecond):
				return "慢速任务完成", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	timeoutResults := shortExecutor.ExecuteParallel(ctx, timeoutTasks)
	for i, result := range timeoutResults {
		if result.Error != nil {
			fmt.Printf("任务 %d: 超时或失败 - %v\n", i+1, result.Error)
		} else {
			fmt.Printf("任务 %d: 成功 - %v\n", i+1, result.Result)
		}
	}
	fmt.Println()

	// 示例 4: 实际应用场景 - 监控数据采集
	fmt.Println("=== 示例 4: 监控数据采集（模拟）===")
	monitoringTasks := []concurrent.TaskFunc{
		func(ctx context.Context) (interface{}, error) {
			time.Sleep(50 * time.Millisecond)
			return map[string]interface{}{
				"source": "K8s",
				"pods":   15,
				"status": "healthy",
			}, nil
		},
		func(ctx context.Context) (interface{}, error) {
			time.Sleep(80 * time.Millisecond)
			return map[string]interface{}{
				"source":     "Prometheus",
				"cpu_usage":  45.2,
				"mem_usage":  62.8,
			}, nil
		},
		func(ctx context.Context) (interface{}, error) {
			time.Sleep(60 * time.Millisecond)
			return map[string]interface{}{
				"source":      "Elasticsearch",
				"error_count": 3,
				"warn_count":  12,
			}, nil
		},
	}

	monitorStart := time.Now()
	monitorResults := executor.ExecuteParallel(ctx, monitoringTasks)
	monitorDuration := time.Since(monitorStart)

	fmt.Printf("数据采集完成，耗时: %v\n", monitorDuration)
	fmt.Println("采集结果:")
	for _, result := range monitorResults {
		if result.Error == nil {
			fmt.Printf("  %+v\n", result.Result)
		}
	}
	fmt.Println()

	fmt.Println("=== 演示完成 ===")
	fmt.Println("提示: 在实际使用中，可以将这些功能集成到 Supervisor Agent 中")
	fmt.Println("      实现多 Agent 并行调用和工具并行执行")

	// 清理
	_ = agentExecutor
	_ = toolExecutor
}
