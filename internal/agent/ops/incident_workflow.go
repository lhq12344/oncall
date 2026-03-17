package ops

import (
	"context"
	"fmt"

	"go_agent/internal/agent/execution"
	"go_agent/internal/agent/rca"
	"go_agent/internal/agent/strategy"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
)

// IncidentWorkflowConfig 四阶段故障处置工作流配置。
type IncidentWorkflowConfig struct {
	ChatModel *models.ChatModel

	KubeConfig    string
	PrometheusURL string

	// MaxExecutionLoops 执行重规划最大轮次，默认 3。
	MaxExecutionLoops int

	Logger *zap.Logger
}

// NewIncidentWorkflowAgent 创建故障处置工作流 Agent。
//
// 功能：
// 1. 创建各个子 Agent（观察、RCA、运维、执行、策略）
// 2. 构建执行循环（Ops -> Execution -> Gate，最多循环 MaxExecutionLoops 次）
// 3. 构建顺序工作流（Observation -> RCA -> Loop -> Strategy -> FinalReport）
// 4. 绑定历史重写器，优化 token 消耗
//
// 工作流形态：
// Sequential(
//
//	Observation,
//	RCA,
//	Loop(Ops, Execution, Gate),  // 最多循环 3 次
//	Strategy,
//	FinalReport
//
// )
//
// 调用位置：
// - bootstrap/app.go:132 行，应用启动时调用
//
// 输入：
// - ctx: 上下文
// - cfg: 故障处置工作流配置
//
// 输出：
// - adk.ResumableAgent: 可恢复的工作流 Agent
// - error: 创建过程中的错误
//
// 使用示例：
//
//	opsAgent, err := ops.NewIncidentWorkflowAgent(ctx, &ops.IncidentWorkflowConfig{
//	    ChatModel:     chatModel,
//	    KubeConfig:    cfg.KubeConfig,
//	    PrometheusURL: cfg.PrometheusURL,
//	    Logger:        logger,
//	})
func NewIncidentWorkflowAgent(ctx context.Context, cfg *IncidentWorkflowConfig) (adk.ResumableAgent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	opsCfg := &Config{
		ChatModel:     cfg.ChatModel,
		KubeConfig:    cfg.KubeConfig,
		PrometheusURL: cfg.PrometheusURL,
		Logger:        cfg.Logger,
	}

	rcaAgent, err := rca.NewRCAAgent(ctx, &rca.Config{
		ChatModel:     cfg.ChatModel,
		KubeConfig:    cfg.KubeConfig,
		PrometheusURL: cfg.PrometheusURL,
		Logger:        cfg.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("create rca agent failed: %w", err)
	}

	opsAgent, err := NewOpsAgent(ctx, opsCfg)
	if err != nil {
		return nil, fmt.Errorf("create ops agent failed: %w", err)
	}

	executionAgent, err := execution.NewExecutionAgent(ctx, &execution.Config{
		ChatModel: cfg.ChatModel,
		Logger:    cfg.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("create execution agent failed: %w", err)
	}

	strategyAgent, err := strategy.NewStrategyAgent(ctx, &strategy.Config{
		ChatModel: cfg.ChatModel,
		Logger:    cfg.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("create strategy agent failed: %w", err)
	}

	// 将结构化结果写入 Graph State（session values），避免把大段日志作为聊天历史反复回灌。
	observerAgent := newObservationCollectorAgent(ctx, cfg)
	rcaAgent = wrapWithIncidentState("rca", rcaAgent, cfg.Logger)
	opsAgent = wrapWithIncidentState("ops", opsAgent, cfg.Logger)
	executionAgent = wrapWithIncidentState("execution", executionAgent, cfg.Logger)
	strategyAgent = wrapWithIncidentState("strategy", strategyAgent, cfg.Logger)

	gate := newExecutionGateAgent(cfg.Logger)
	reporter := newFinalReportAgent(cfg.Logger)

	maxLoops := cfg.MaxExecutionLoops
	if maxLoops <= 0 {
		maxLoops = 3
	}

	executeLoop, err := adk.NewLoopAgent(ctx, &adk.LoopAgentConfig{
		Name:          "incident_execute_loop",
		Description:   "Ops 修复提案 -> Execution 命令计划与执行 -> 决策 的循环工作流",
		SubAgents:     []adk.Agent{opsAgent, executionAgent, gate},
		MaxIterations: maxLoops,
	})
	if err != nil {
		return nil, fmt.Errorf("create execution loop failed: %w", err)
	}

	workflow, err := adk.NewSequentialAgent(ctx, &adk.SequentialAgentConfig{
		Name:        "incident_workflow_agent",
		Description: "统一故障处置代理：观测采集、RCA 分析、Ops 计划、Execution 执行、Strategy 复盘",
		SubAgents:   []adk.Agent{observerAgent, rcaAgent, executeLoop, strategyAgent, reporter},
	})
	if err != nil {
		return nil, fmt.Errorf("create incident workflow failed: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("incident workflow agent initialized",
			zap.Int("max_execution_loops", maxLoops))
	}

	withState := adk.AgentWithOptions(ctx, workflow, adk.WithHistoryRewriter(incidentHistoryRewriter))
	resumable, ok := withState.(adk.ResumableAgent)
	if !ok {
		return nil, fmt.Errorf("incident workflow agent is not resumable after state binding")
	}
	return resumable, nil
}
