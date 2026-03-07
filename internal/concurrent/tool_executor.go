package concurrent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"go.uber.org/zap"
)

// ToolExecutorConfig 工具并行执行器配置
type ToolExecutorConfig struct {
	Executor              *Executor
	CircuitBreakerManager *CircuitBreakerManager
	Logger                *zap.Logger
}

// ToolExecutor 工具并行执行器
type ToolExecutor struct {
	executor *Executor
	cbMgr    *CircuitBreakerManager
	logger   *zap.Logger
}

// NewToolExecutor 创建工具并行执行器
func NewToolExecutor(config *ToolExecutorConfig) *ToolExecutor {
	if config == nil {
		config = &ToolExecutorConfig{}
	}

	if config.Executor == nil {
		config.Executor = NewExecutor(&ExecutorConfig{
			MaxConcurrency: 10,
			Logger:         config.Logger,
		})
	}

	if config.CircuitBreakerManager == nil {
		config.CircuitBreakerManager = NewCircuitBreakerManager(config.Logger)
	}

	return &ToolExecutor{
		executor: config.Executor,
		cbMgr:    config.CircuitBreakerManager,
		logger:   config.Logger,
	}
}

// ToolTask 工具任务
type ToolTask struct {
	Name      string
	Tool      tool.InvokableTool
	Arguments string
}

// ToolResult 工具执行结果
type ToolResult struct {
	Name   string
	Output string
	Error  error
}

// ExecuteToolsParallel 并行执行多个工具
func (te *ToolExecutor) ExecuteToolsParallel(ctx context.Context, tasks []ToolTask) []ToolResult {
	if len(tasks) == 0 {
		return []ToolResult{}
	}

	// 转换为通用任务
	taskFuncs := make([]TaskFunc, len(tasks))
	for i, task := range tasks {
		task := task // 捕获循环变量
		taskFuncs[i] = func(ctx context.Context) (interface{}, error) {
			// 获取或创建熔断器
			cb := te.cbMgr.GetOrCreate(fmt.Sprintf("tool_%s", task.Name), &CircuitBreakerConfig{
				Logger: te.logger,
			})

			// 使用熔断器执行
			result, err := cb.Execute(ctx, func(ctx context.Context) (interface{}, error) {
				return task.Tool.InvokableRun(ctx, task.Arguments)
			})

			if err != nil {
				return nil, err
			}

			return result, nil
		}
	}

	// 并行执行
	results := te.executor.ExecuteParallel(ctx, taskFuncs)

	// 转换结果
	toolResults := make([]ToolResult, len(results))
	for i, result := range results {
		toolResults[i] = ToolResult{
			Name:  tasks[i].Name,
			Error: result.Error,
		}

		if result.Error == nil && result.Result != nil {
			if output, ok := result.Result.(string); ok {
				toolResults[i].Output = output
			} else {
				toolResults[i].Output = fmt.Sprintf("%v", result.Result)
			}
		}
	}

	return toolResults
}

// ExecuteToolWithCircuitBreaker 使用熔断器执行单个工具
func (te *ToolExecutor) ExecuteToolWithCircuitBreaker(
	ctx context.Context,
	name string,
	tool tool.InvokableTool,
	arguments string,
) (string, error) {
	cb := te.cbMgr.GetOrCreate(fmt.Sprintf("tool_%s", name), &CircuitBreakerConfig{
		Logger: te.logger,
	})

	result, err := cb.Execute(ctx, func(ctx context.Context) (interface{}, error) {
		return tool.InvokableRun(ctx, arguments)
	})

	if err != nil {
		return "", err
	}

	if output, ok := result.(string); ok {
		return output, nil
	}

	return fmt.Sprintf("%v", result), nil
}
