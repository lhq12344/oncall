package ops

import (
	"context"
	"fmt"

	"go_agent/internal/agent/agentteams"
	"go_agent/internal/agent/execution"
	"go_agent/internal/agent/rca"
	"go_agent/internal/agent/strategy"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
)

const incidentDefaultMaxExecutionLoops = agentteams.DefaultLoopMaxIterations

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

	team, maxLoops, err := newIncidentWorkflowTeam(cfg.MaxExecutionLoops, incidentWorkflowMembers{
		observation: observerAgent,
		rca:         rcaAgent,
		ops:         opsAgent,
		execution:   executionAgent,
		gate:        gate,
		strategy:    strategyAgent,
		reporter:    reporter,
	})
	if err != nil {
		return nil, fmt.Errorf("create incident workflow team failed: %w", err)
	}

	workflow, err := team.Build(ctx)
	if err != nil {
		return nil, fmt.Errorf("create incident workflow failed: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("incident workflow agent team initialized",
			zap.Int("max_execution_loops", maxLoops),
			zap.Int("team_members", len(team.Members)),
			zap.Int("team_stages", len(team.Stages)))
	}

	withState := adk.AgentWithOptions(ctx, workflow, adk.WithHistoryRewriter(incidentHistoryRewriter))
	resumable, ok := withState.(adk.ResumableAgent)
	if !ok {
		return nil, fmt.Errorf("incident workflow agent is not resumable after state binding")
	}
	return resumable, nil
}

type incidentWorkflowMembers struct {
	observation adk.Agent
	rca         adk.Agent
	ops         adk.Agent
	execution   adk.Agent
	gate        adk.Agent
	strategy    adk.Agent
	reporter    adk.Agent
}

func newIncidentWorkflowTeam(maxLoops int, members incidentWorkflowMembers) (*agentteams.Team, int, error) {
	if maxLoops <= 0 {
		maxLoops = incidentDefaultMaxExecutionLoops
	}

	team := agentteams.NewTeam(
		"incident_workflow_agent",
		"Unified incident response team: observation, RCA, ops planning, execution, strategy review, and final report",
	)

	registrations := []struct {
		name        string
		description string
		agent       adk.Agent
	}{
		{name: "observation", description: "Collect K8s, metrics, and log observations", agent: members.observation},
		{name: "rca", description: "Analyze root cause from observations and evidence", agent: members.rca},
		{name: "ops", description: "Plan remediation actions from RCA output", agent: members.ops},
		{name: "execution", description: "Validate and execute approved remediation steps", agent: members.execution},
		{name: "gate", description: "Decide whether execution should stop, replan, or request approval", agent: members.gate},
		{name: "strategy", description: "Evaluate outcome and update strategy knowledge", agent: members.strategy},
		{name: "final_report", description: "Generate final technical incident report", agent: members.reporter},
	}
	for _, registration := range registrations {
		if err := team.AddMember(registration.name, registration.description, registration.agent); err != nil {
			return nil, 0, err
		}
	}

	if err := team.AddSequentialStage("incident_observation_stage", "Collect the initial incident observation snapshot", "observation"); err != nil {
		return nil, 0, err
	}
	if err := team.AddSequentialStage("incident_rca_stage", "Analyze root cause and persist the RCA state", "rca"); err != nil {
		return nil, 0, err
	}
	if err := team.AddLoopStage(
		"incident_execute_loop",
		"Ops remediation proposal -> Execution command planning and execution -> Gate decision loop",
		maxLoops,
		"ops",
		"execution",
		"gate",
	); err != nil {
		return nil, 0, err
	}
	if err := team.AddSequentialStage("incident_strategy_stage", "Evaluate the run and update strategy state", "strategy"); err != nil {
		return nil, 0, err
	}
	if err := team.AddSequentialStage("incident_final_report_stage", "Generate the final incident report", "final_report"); err != nil {
		return nil, 0, err
	}

	return team, maxLoops, nil
}
