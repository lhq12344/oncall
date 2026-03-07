package concurrent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// AgentExecutorConfig Agent 并行执行器配置
type AgentExecutorConfig struct {
	Executor              *Executor
	CircuitBreakerManager *CircuitBreakerManager
	Logger                *zap.Logger
}

// AgentExecutor Agent 并行执行器
type AgentExecutor struct {
	executor *Executor
	cbMgr    *CircuitBreakerManager
	logger   *zap.Logger
}

// NewAgentExecutor 创建 Agent 并行执行器
func NewAgentExecutor(config *AgentExecutorConfig) *AgentExecutor {
	if config == nil {
		config = &AgentExecutorConfig{}
	}

	if config.Executor == nil {
		config.Executor = NewExecutor(&ExecutorConfig{
			MaxConcurrency: 5,
			Logger:         config.Logger,
		})
	}

	if config.CircuitBreakerManager == nil {
		config.CircuitBreakerManager = NewCircuitBreakerManager(config.Logger)
	}

	return &AgentExecutor{
		executor: config.Executor,
		cbMgr:    config.CircuitBreakerManager,
		logger:   config.Logger,
	}
}

// AgentTask Agent 任务
type AgentTask struct {
	Name  string
	Agent adk.Agent
	Input *schema.Message
}

// AgentResult Agent 执行结果
type AgentResult struct {
	Name   string
	Output *schema.Message
	Error  error
}

// ExecuteAgentsParallel 并行执行多个 Agent
func (ae *AgentExecutor) ExecuteAgentsParallel(ctx context.Context, tasks []AgentTask) []AgentResult {
	if len(tasks) == 0 {
		return []AgentResult{}
	}

	// 转换为通用任务
	taskFuncs := make([]TaskFunc, len(tasks))
	for i, task := range tasks {
		task := task // 捕获循环变量
		taskFuncs[i] = func(ctx context.Context) (interface{}, error) {
			// 获取或创建熔断器
			cb := ae.cbMgr.GetOrCreate(fmt.Sprintf("agent_%s", task.Name), &CircuitBreakerConfig{
				Logger: ae.logger,
			})

			// 使用熔断器执行
			result, err := cb.Execute(ctx, func(ctx context.Context) (interface{}, error) {
				// 使用 Agent.Run 方法
				iter := task.Agent.Run(ctx, &adk.AgentInput{
					Messages: []*schema.Message{task.Input},
				})

				// 收集所有事件
				var lastMessage *schema.Message
				for {
					event, hasNext := iter.Next()
					if !hasNext {
						break
					}

					// 检查错误
					if event.Err != nil {
						return nil, event.Err
					}

					// 提取输出消息
					if event.Output != nil && event.Output.MessageOutput != nil {
						if msg := event.Output.MessageOutput.Message; msg != nil {
							lastMessage = msg
						}
					}
				}

				return lastMessage, nil
			})

			if err != nil {
				return nil, err
			}

			return result, nil
		}
	}

	// 并行执行
	results := ae.executor.ExecuteParallel(ctx, taskFuncs)

	// 转换结果
	agentResults := make([]AgentResult, len(results))
	for i, result := range results {
		agentResults[i] = AgentResult{
			Name:  tasks[i].Name,
			Error: result.Error,
		}

		if result.Error == nil && result.Result != nil {
			if msg, ok := result.Result.(*schema.Message); ok {
				agentResults[i].Output = msg
			}
		}
	}

	return agentResults
}

// ExecuteAgentWithCircuitBreaker 使用熔断器执行单个 Agent
func (ae *AgentExecutor) ExecuteAgentWithCircuitBreaker(
	ctx context.Context,
	name string,
	agent adk.Agent,
	input *schema.Message,
) (*schema.Message, error) {
	cb := ae.cbMgr.GetOrCreate(fmt.Sprintf("agent_%s", name), &CircuitBreakerConfig{
		Logger: ae.logger,
	})

	result, err := cb.Execute(ctx, func(ctx context.Context) (interface{}, error) {
		// 使用 Agent.Run 方法
		iter := agent.Run(ctx, &adk.AgentInput{
			Messages: []*schema.Message{input},
		})

		// 收集所有事件
		var lastMessage *schema.Message
		for {
			event, hasNext := iter.Next()
			if !hasNext {
				break
			}

			// 检查错误
			if event.Err != nil {
				return nil, event.Err
			}

			// 提取输出消息
			if event.Output != nil && event.Output.MessageOutput != nil {
				if msg := event.Output.MessageOutput.Message; msg != nil {
					lastMessage = msg
				}
			}
		}

		return lastMessage, nil
	})

	if err != nil {
		return nil, err
	}

	if msg, ok := result.(*schema.Message); ok {
		return msg, nil
	}

	return nil, fmt.Errorf("unexpected result type: %T", result)
}
