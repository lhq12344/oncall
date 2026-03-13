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

// NewIncidentWorkflowAgent 创建一个 Observation -> RCA -> Ops -> Validator -> Execution -> Strategy 的工作流 Agent。
// 工作流形态：Sequential(Observation, RCA, Loop(Ops, Validator, Execution, Gate), Strategy, FinalReport)。
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
		ChatModel:  cfg.ChatModel,
		KubeConfig: cfg.KubeConfig,
		Logger:     cfg.Logger,
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

	validator := newPlanValidatorAgent(cfg.Logger)
	gate := newExecutionGateAgent(cfg.Logger)
	reporter := newFinalReportAgent(cfg.Logger)

	maxLoops := cfg.MaxExecutionLoops
	if maxLoops <= 0 {
		maxLoops = 3
	}

	executeLoop, err := adk.NewLoopAgent(ctx, &adk.LoopAgentConfig{
		Name:          "incident_execute_loop",
		Description:   "Ops 规划 -> 安全校验 -> 执行 -> 决策 的循环工作流",
		SubAgents:     []adk.Agent{opsAgent, validator, executionAgent, gate},
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
