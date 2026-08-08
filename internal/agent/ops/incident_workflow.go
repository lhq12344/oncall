package ops

import (
	"context"
	"fmt"

	"go_agent/internal/agent/agentteams"
	"go_agent/internal/agent/execution"
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

// NewIncidentWorkflowAgent 创建可恢复、可审计的故障处置 workflow。
//
// 工作流职责边界：
// 1. ops_incident_agent 自主按需选择只读诊断工具，完成观测、RCA 和修复提案生成。
// 2. execution_agent 作为独立安全边界，负责命令级计划、风险校验、审批触发、执行和回滚。
// 3. execution_gate 根据执行结果决定结束、重规划、审批中断或转人工。
// 4. final_reporter 读取 Graph State 生成最终技术报告并归档。
//
// 工作流形态：
// Sequential(
//
//	Loop(Incident, IncidentContractGate, Execution, Gate), // 最多 MaxExecutionLoops 次
//	FinalReport,
//
// )
//
// 调用位置：
// - bootstrap/app.go:132 行，应用启动时调用。
//
// 输入：
// - ctx: 上下文。
// - cfg: 故障处置工作流配置。
//
// 输出：
// - adk.ResumableAgent: 可恢复的工作流 Agent。
// - error: 创建过程中的错误。
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

	incidentAgent, err := NewOpsAgent(ctx, opsCfg)
	if err != nil {
		return nil, fmt.Errorf("create ops incident agent failed: %w", err)
	}

	executionAgent, err := execution.NewExecutionAgent(ctx, &execution.Config{
		ChatModel: cfg.ChatModel,
		Logger:    cfg.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("create execution agent failed: %w", err)
	}

	// 将结构化结果写入 Graph State（session values），避免把大段日志作为聊天历史反复回灌。
	incidentAgent = wrapWithIncidentState("incident", incidentAgent, cfg.Logger)
	executionAgent = wrapWithIncidentState("execution", executionAgent, cfg.Logger)
	executionAgent = newContractGuardedExecutionAgent(executionAgent, cfg.Logger)

	contractGate := newIncidentContractGateAgent(cfg.Logger)
	gate := newExecutionGateAgent(cfg.Logger)
	reporter := newFinalReportAgent(cfg.Logger)

	team, maxLoops, err := newIncidentWorkflowTeam(cfg.MaxExecutionLoops, incidentWorkflowMembers{
		incident:     incidentAgent,
		contractGate: contractGate,
		execution:    executionAgent,
		gate:         gate,
		reporter:     reporter,
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
	incident     adk.Agent
	contractGate adk.Agent
	execution    adk.Agent
	gate         adk.Agent
	reporter     adk.Agent
}

func newIncidentWorkflowTeam(maxLoops int, members incidentWorkflowMembers) (*agentteams.Team, int, error) {
	if maxLoops <= 0 {
		maxLoops = incidentDefaultMaxExecutionLoops
	}

	team := agentteams.NewTeam(
		"incident_workflow_agent",
		"Auditable incident response workflow: agent-led diagnosis, isolated execution, gate review, and final report",
	)

	registrations := []struct {
		name        string
		description string
		agent       adk.Agent
	}{
		{name: "incident", description: "Select diagnostic tools on demand, infer root cause, and propose remediation", agent: members.incident},
		{name: "incident_contract_gate", description: "Validate incident evidence and remediation contract before execution", agent: members.contractGate},
		{name: "execution", description: "Validate and execute approved remediation steps", agent: members.execution},
		{name: "gate", description: "Decide whether execution should stop, replan, or request approval", agent: members.gate},
		{name: "final_report", description: "Generate final technical incident report", agent: members.reporter},
	}
	for _, registration := range registrations {
		if err := team.AddMember(registration.name, registration.description, registration.agent); err != nil {
			return nil, 0, err
		}
	}

	if err := team.AddLoopStage(
		"incident_response_loop",
		"Incident diagnosis/proposal -> isolated execution -> gate decision loop",
		maxLoops,
		"incident",
		"incident_contract_gate",
		"execution",
		"gate",
	); err != nil {
		return nil, 0, err
	}
	if err := team.AddSequentialStage("incident_final_report_stage", "Generate the final incident report", "final_report"); err != nil {
		return nil, 0, err
	}

	return team, maxLoops, nil
}
