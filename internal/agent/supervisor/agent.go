package supervisor

import (
	"context"
	"fmt"

	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/supervisor"
	"go.uber.org/zap"
)

// Config Supervisor Agent 配置
type Config struct {
	ChatModel       *models.ChatModel
	KnowledgeAgent  adk.Agent
	DialogueAgent   adk.Agent
	OpsAgent        adk.Agent
	ExecutionAgent  adk.Agent
	RCAAgent        adk.Agent
	StrategyAgent   adk.Agent
	Logger          *zap.Logger
}

// NewSupervisorAgent 创建 Supervisor Agent（使用 Eino ADK prebuilt supervisor）
func NewSupervisorAgent(ctx context.Context, cfg *Config) (adk.ResumableAgent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	// 创建 Supervisor ChatModelAgent
	supervisorAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "supervisor",
		Description: "总控代理，协调多个子 Agent 完成复杂的运维任务",
		Model:       cfg.ChatModel.Client,
		Instruction: `你是一个总控代理（Supervisor），负责协调多个子 Agent 完成复杂的运维任务。

你可以调用以下子 Agent（它们已经作为工具提供给你）：
1. knowledge_agent - 知识库代理：检索历史故障案例和最佳实践，索引新文档
2. dialogue_agent - 对话代理：分析用户意图、预测问题、引导对话
3. ops_agent - 运维代理：监控系统状态、采集指标、分析日志
4. execution_agent - 执行代理：生成执行计划、安全执行操作、回滚机制
5. rca_agent - 根因分析代理：构建依赖图、信号关联、根因推理、影响分析
6. strategy_agent - 策略代理：评估策略质量、优化执行策略、管理知识库

重要：当用户询问历史案例、最佳实践、故障处理方案时，你必须调用 knowledge_agent 来检索知识库。

工作流程示例：
- 知识检索（用户问"有没有xxx的处理方案"）：
  → 调用 knowledge_agent 检索相关案例
  → 返回检索结果给用户

- 监控查询：
  → 调用 dialogue_agent 分析意图
  → 调用 ops_agent 采集数据
  → 返回结果

- 故障诊断：
  → 调用 dialogue_agent 澄清问题
  → 调用 ops_agent 收集监控
  → 调用 knowledge_agent 检索案例
  → 综合分析并返回

- 文档索引（用户上传文档）：
  → 调用 knowledge_agent 索引文档
  → 返回索引结果

注意：
- 根据任务类型选择合适的子 Agent
- 涉及知识检索的任务必须调用 knowledge_agent
- 简单问候可以直接回答
- 复杂任务需要多个 Agent 协作`,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create supervisor agent: %w", err)
	}

	// 收集所有子 Agent
	subAgents := []adk.Agent{}
	if cfg.KnowledgeAgent != nil {
		subAgents = append(subAgents, cfg.KnowledgeAgent)
	}
	if cfg.DialogueAgent != nil {
		subAgents = append(subAgents, cfg.DialogueAgent)
	}
	if cfg.OpsAgent != nil {
		subAgents = append(subAgents, cfg.OpsAgent)
	}
	if cfg.ExecutionAgent != nil {
		subAgents = append(subAgents, cfg.ExecutionAgent)
	}
	if cfg.RCAAgent != nil {
		subAgents = append(subAgents, cfg.RCAAgent)
	}
	if cfg.StrategyAgent != nil {
		subAgents = append(subAgents, cfg.StrategyAgent)
	}
	if len(subAgents) == 0 {
		return nil, fmt.Errorf("at least one sub agent is required")
	}

	// 使用 Eino ADK prebuilt supervisor 创建多 Agent 系统
	multiAgent, err := supervisor.New(ctx, &supervisor.Config{
		Supervisor: supervisorAgent,
		SubAgents:  subAgents,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create supervisor multi-agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("supervisor agent created with sub-agents",
			zap.Int("sub_agent_count", len(subAgents)))
	}

	return multiAgent, nil
}
