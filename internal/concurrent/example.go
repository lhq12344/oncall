package concurrent

import (
	"context"
	"time"

	"go_agent/internal/agent/dialogue"
	"go_agent/internal/agent/execution"
	"go_agent/internal/agent/knowledge"
	"go_agent/internal/agent/ops"
	"go_agent/internal/agent/rca"
	"go_agent/internal/agent/strategy"
	"go_agent/internal/agent/supervisor"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// SupervisorWithConcurrency Supervisor 配置（支持并发）
type SupervisorWithConcurrency struct {
	supervisor    *supervisor.Config
	agentExecutor *AgentExecutor
	logger        *zap.Logger
}

// NewSupervisorWithConcurrency 创建支持并发的 Supervisor
func NewSupervisorWithConcurrency(
	ctx context.Context,
	chatModel *models.ChatModel,
	logger *zap.Logger,
) (*SupervisorWithConcurrency, error) {
	// 创建并发执行器
	agentExecutor := NewAgentExecutor(&AgentExecutorConfig{
		Executor: NewExecutor(&ExecutorConfig{
			MaxConcurrency: 3,
			Timeout:        60 * time.Second,
			Logger:         logger,
		}),
		CircuitBreakerManager: NewCircuitBreakerManager(logger),
		Logger:                logger,
	})

	// 创建各个子 Agent（这里简化，实际需要完整配置）
	knowledgeAgent, err := knowledge.NewKnowledgeAgent(ctx, &knowledge.Config{
		ChatModel: chatModel,
		Logger:    logger,
	})
	if err != nil {
		return nil, err
	}

	dialogueAgent, err := dialogue.NewDialogueAgent(ctx, &dialogue.Config{
		ChatModel: chatModel,
		Logger:    logger,
	})
	if err != nil {
		return nil, err
	}

	opsAgent, err := ops.NewOpsAgent(ctx, &ops.Config{
		ChatModel: chatModel,
		Logger:    logger,
	})
	if err != nil {
		return nil, err
	}

	executionAgent, err := execution.NewExecutionAgent(ctx, &execution.Config{
		ChatModel: chatModel,
		Logger:    logger,
	})
	if err != nil {
		return nil, err
	}

	rcaAgent, err := rca.NewRCAAgent(ctx, &rca.Config{
		ChatModel: chatModel,
		Logger:    logger,
	})
	if err != nil {
		return nil, err
	}

	strategyAgent, err := strategy.NewStrategyAgent(ctx, &strategy.Config{
		ChatModel: chatModel,
		Logger:    logger,
	})
	if err != nil {
		return nil, err
	}

	supervisorConfig := &supervisor.Config{
		ChatModel:      chatModel,
		KnowledgeAgent: knowledgeAgent,
		DialogueAgent:  dialogueAgent,
		OpsAgent:       opsAgent,
		ExecutionAgent: executionAgent,
		RCAAgent:       rcaAgent,
		StrategyAgent:  strategyAgent,
		Logger:         logger,
	}

	return &SupervisorWithConcurrency{
		supervisor:    supervisorConfig,
		agentExecutor: agentExecutor,
		logger:        logger,
	}, nil
}

// ExecuteAgentsInParallel 并行执行多个 Agent（示例方法）
func (s *SupervisorWithConcurrency) ExecuteAgentsInParallel(
	ctx context.Context,
	tasks []AgentTask,
) []AgentResult {
	return s.agentExecutor.ExecuteAgentsParallel(ctx, tasks)
}

// Example 使用示例
func Example() {
	ctx := context.Background()
	logger, _ := zap.NewDevelopment()

	// 假设已经创建了 chatModel
	var chatModel *models.ChatModel

	// 创建支持并发的 Supervisor
	supervisor, err := NewSupervisorWithConcurrency(ctx, chatModel, logger)
	if err != nil {
		logger.Fatal("failed to create supervisor", zap.Error(err))
	}

	// 并行执行多个 Agent
	tasks := []AgentTask{
		{
			Name:  "knowledge",
			Agent: supervisor.supervisor.KnowledgeAgent,
			Input: &schema.Message{
				Role:    schema.User,
				Content: "查询历史故障案例",
			},
		},
		{
			Name:  "ops",
			Agent: supervisor.supervisor.OpsAgent,
			Input: &schema.Message{
				Role:    schema.User,
				Content: "检查系统状态",
			},
		},
	}

	results := supervisor.ExecuteAgentsInParallel(ctx, tasks)

	// 处理结果
	for _, result := range results {
		if result.Error != nil {
			logger.Error("agent execution failed",
				zap.String("agent", result.Name),
				zap.Error(result.Error))
		} else {
			logger.Info("agent execution succeeded",
				zap.String("agent", result.Name),
				zap.String("output", result.Output.Content))
		}
	}
}
