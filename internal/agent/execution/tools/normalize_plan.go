package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// NormalizePlanTool 将完整 command_hint 的修复提案规范化为 ExecutionPlan。
type NormalizePlanTool struct {
	chatModel *models.ChatModel
	logger    *zap.Logger
}

// NewNormalizePlanTool 创建提案规范化工具。
// 输入：chatModel（用于 command_hint 不完整时降级补全）、logger（日志）。
// 输出：可供 execution_agent 调用的规范化工具。
func NewNormalizePlanTool(chatModel *models.ChatModel, logger *zap.Logger) tool.BaseTool {
	return &NormalizePlanTool{
		chatModel: chatModel,
		logger:    logger,
	}
}

func (t *NormalizePlanTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "normalize_plan",
		Desc: "优先将包含完整 command_hint 的 RemediationProposal 规范化为 ExecutionPlan；若 command_hint 不完整，会自动降级补全计划，避免流程中断。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"proposal": {
				Type:     schema.Object,
				Desc:     "上游 RemediationProposal 结构化提案",
				Required: true,
			},
		}),
	}, nil
}

func (t *NormalizePlanTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Proposal map[string]any `json:"proposal"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	proposal, err := parseProposalInput(in.Proposal)
	if err != nil {
		return "", err
	}
	if proposal == nil {
		return "", fmt.Errorf("proposal is required")
	}

	var plan *ExecutionPlan
	if proposalHasCompleteCommandHints(proposal) {
		plan, err = normalizeProposalToPlan(proposal)
		if err != nil {
			return "", err
		}
	} else {
		intent, contextText := buildPlanIntentAndContext(proposal, "", "")
		generator := &GeneratePlanTool{
			chatModel: t.chatModel,
			logger:    t.logger,
		}
		plan, err = generator.generatePlanWithLLM(ctx, proposal, intent, contextText)
		if err != nil {
			if t.logger != nil {
				t.logger.Warn("normalize_plan fallback to template generation",
					zap.Error(err),
					zap.String("proposal_id", proposal.ProposalID))
			}
			plan = generator.generatePlanWithTemplate(proposal, intent, contextText)
		}
	}
	if err := validateExecutionPlanStructure(plan); err != nil {
		return "", fmt.Errorf("normalize_plan validation failed: %w", err)
	}

	plan.RiskLevel = assessExecutionPlanRisk(plan)
	markExecutionPlanPrepared(ctx, plan.PlanID)

	output, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("failed to marshal plan: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("execution plan normalized",
			zap.String("plan_id", plan.PlanID),
			zap.Int("steps", plan.TotalSteps),
			zap.String("risk_level", plan.RiskLevel),
			zap.Bool("fallback_generated", !proposalHasCompleteCommandHints(proposal)))
	}
	return string(output), nil
}
