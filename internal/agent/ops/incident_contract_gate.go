package ops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
)

type incidentContractGateAgent struct {
	name   string
	desc   string
	logger *zap.Logger
}

func newIncidentContractGateAgent(logger *zap.Logger) adk.Agent {
	return &incidentContractGateAgent{
		name:   "incident_contract_gate",
		desc:   "validates incident RCA and remediation proposal before execution",
		logger: logger,
	}
}

func (a *incidentContractGateAgent) Name(_ context.Context) string {
	return a.name
}

func (a *incidentContractGateAgent) Description(_ context.Context) string {
	return a.desc
}

func (a *incidentContractGateAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer generator.Close()

		var messages []adk.Message
		if input != nil {
			messages = input.Messages
		}
		state := getIncidentState(ctx)
		result := validateIncidentContract(messages, state)
		applyIncidentContractValidation(ctx, state, result)

		if result.Valid {
			if a.logger != nil {
				a.logger.Info("incident contract passed",
					zap.String("risk", result.RiskLevel),
					zap.Int("evidence_count", result.EvidenceCount))
			}
			generator.Send(assistantEvent("incident_contract_gate passed: RCA evidence and remediation proposal are safe to hand to execution_agent."))
			return
		}

		if a.logger != nil {
			a.logger.Warn("incident contract blocked execution", zap.Strings("issues", result.Issues))
		}
		generator.Send(assistantEvent(fmt.Sprintf(
			"incident_contract_gate blocked execution and returned to ops_incident_agent for replanning: %s",
			strings.Join(result.Issues, "; "),
		)))
	}()
	return iterator
}

type contractGuardedExecutionAgent struct {
	inner  adk.Agent
	logger *zap.Logger
}

func newContractGuardedExecutionAgent(inner adk.Agent, logger *zap.Logger) adk.Agent {
	return &contractGuardedExecutionAgent{inner: inner, logger: logger}
}

func (a *contractGuardedExecutionAgent) Name(ctx context.Context) string {
	if a.inner == nil {
		return "execution_agent"
	}
	return a.inner.Name(ctx)
}

func (a *contractGuardedExecutionAgent) Description(ctx context.Context) string {
	if a.inner == nil {
		return "contract guarded execution agent"
	}
	return a.inner.Description(ctx)
}

func (a *contractGuardedExecutionAgent) Run(ctx context.Context, input *adk.AgentInput, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	state := getIncidentState(ctx)
	if allowed, reason := incidentContractAllowsExecution(state); !allowed {
		iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
		go func() {
			defer generator.Close()
			if a.logger != nil {
				a.logger.Warn("skip execution because incident contract is invalid", zap.String("reason", reason))
			}
			generator.Send(assistantEvent("execution_agent skipped: incident contract is invalid; ops_incident_agent must replan before execution."))
		}()
		return iterator
	}
	if a.inner == nil {
		iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
		go func() {
			defer generator.Close()
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("execution agent is required")})
		}()
		return iterator
	}
	return a.inner.Run(ctx, input, opts...)
}

func (a *contractGuardedExecutionAgent) Resume(ctx context.Context, info *adk.ResumeInfo, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	resumable, ok := a.inner.(adk.ResumableAgent)
	if !ok {
		iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
		go func() {
			defer generator.Close()
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("execution agent is not resumable")})
		}()
		return iterator
	}
	return resumable.Resume(ctx, info, opts...)
}

func incidentContractAllowsExecution(state *IncidentState) (bool, string) {
	if state == nil || !state.IncidentContractValid {
		if state != nil {
			if reason := strings.Join(state.IncidentContractIssues, "; "); strings.TrimSpace(reason) != "" {
				return false, reason
			}
		}
		return false, "incident contract gate has not passed"
	}
	return true, ""
}

type incidentContractValidation struct {
	Valid         bool
	Issues        []string
	RiskLevel     string
	EvidenceCount int
	Confidence    float64
}

func validateIncidentContract(messages []adk.Message, state *IncidentState) incidentContractValidation {
	report, _ := parseRCAReport(messages)
	proposal, _ := parseRemediationProposal(messages)
	if report == nil {
		report = rcaReportFromState(state)
	}
	if proposal == nil {
		proposal = remediationProposalFromState(state)
	}

	result := incidentContractValidation{Valid: true}
	addIssue := func(issue string) {
		issue = strings.TrimSpace(issue)
		if issue == "" {
			return
		}
		result.Valid = false
		result.Issues = append(result.Issues, issue)
	}

	if report == nil {
		addIssue("missing_rca_report")
	} else {
		result.Confidence = report.Confidence
		result.EvidenceCount = len(nonEmptyStrings(report.Evidence))
		if strings.TrimSpace(report.RootCause) == "" {
			addIssue("missing_root_cause")
		}
		if result.EvidenceCount == 0 {
			addIssue("missing_evidence")
		}
		if report.Confidence <= 0 || report.Confidence > 1 {
			addIssue("invalid_confidence")
		} else if report.Confidence < 0.35 {
			addIssue("confidence_too_low")
		}
	}

	if proposal == nil {
		addIssue("missing_remediation_proposal")
	} else {
		result.RiskLevel = normalizeIncidentRisk(proposal.RiskLevel)
		if strings.TrimSpace(proposal.Summary) == "" {
			addIssue("missing_proposal_summary")
		}
		if result.RiskLevel == "" {
			addIssue("missing_or_invalid_risk_level")
		}
		if len(proposal.Actions) == 0 {
			addIssue("empty_actions")
		}
		for idx, action := range proposal.Actions {
			if strings.TrimSpace(action.Goal) == "" && strings.TrimSpace(action.CommandHint) == "" {
				addIssue(fmt.Sprintf("action_%d_missing_goal_or_command", idx+1))
			}
			if strings.TrimSpace(action.SuccessCriteria) == "" {
				addIssue(fmt.Sprintf("action_%d_missing_success_criteria", idx+1))
			}
			if incidentTextClaimsExecution(action.Rationale) {
				addIssue(fmt.Sprintf("action_%d_claims_execution", idx+1))
			}
		}
		if incidentTextClaimsExecution(proposal.Summary) {
			addIssue("proposal_claims_execution")
		}
		if (result.RiskLevel == "high" || result.RiskLevel == "critical") && strings.TrimSpace(proposal.FallbackPlan) == "" {
			addIssue("high_risk_missing_fallback")
		}
		if report != nil && report.Confidence < 0.5 && strings.TrimSpace(proposal.FallbackPlan) == "" {
			addIssue("low_confidence_missing_fallback")
		}
	}

	return result
}

func applyIncidentContractValidation(ctx context.Context, state *IncidentState, result incidentContractValidation) {
	if state == nil {
		state = &IncidentState{}
	}
	state.IncidentContractValid = result.Valid
	state.IncidentContractIssues = append([]string(nil), result.Issues...)
	state.UpdatedAt = time.Now().Format(time.RFC3339)
	if result.Valid {
		state.ValidationBlocked = false
		if strings.EqualFold(strings.TrimSpace(state.ValidationRisk), "contract_invalid") {
			state.ValidationRisk = ""
		}
		appendIncidentExecutionLog(state, "[incident_contract_gate] contract passed")
		setIncidentState(ctx, state)
		return
	}
	state.ValidationBlocked = true
	state.ValidationRisk = "contract_invalid"
	state.ExecutionStatus = "replan_required"
	state.ExecutionSuccess = false
	state.ExecutionReason = clipText("incident contract invalid: "+strings.Join(result.Issues, "; "), 600)
	appendIncidentExecutionLog(state, "[incident_contract_gate] blocked execution: "+strings.Join(result.Issues, "; "))
	setIncidentState(ctx, state)
}

func rcaReportFromState(state *IncidentState) *RCAReport {
	if state == nil {
		return nil
	}
	if strings.TrimSpace(state.RootCause) == "" && len(state.Evidence) == 0 && state.Confidence == 0 {
		return nil
	}
	return &RCAReport{
		RootCause:  strings.TrimSpace(state.RootCause),
		TargetNode: strings.TrimSpace(state.TargetNode),
		Path:       strings.TrimSpace(state.Path),
		Impact:     strings.TrimSpace(state.Impact),
		Confidence: state.Confidence,
		Evidence:   append([]string(nil), state.Evidence...),
	}
}

func remediationProposalFromState(state *IncidentState) *RemediationProposal {
	if state == nil {
		return nil
	}
	if strings.TrimSpace(state.RemediationProposalSummary) == "" && len(state.RemediationProposalActions) == 0 {
		return nil
	}
	proposal := &RemediationProposal{
		ProposalID:   strings.TrimSpace(firstNonEmptyText(state.RemediationProposalID, state.PlanID)),
		Summary:      strings.TrimSpace(firstNonEmptyText(state.RemediationProposalSummary, state.PlanSummary)),
		RootCause:    strings.TrimSpace(state.RootCause),
		TargetNode:   strings.TrimSpace(state.TargetNode),
		RiskLevel:    strings.TrimSpace(firstNonEmptyText(state.RemediationProposalRisk, state.PlanRisk)),
		FallbackPlan: strings.TrimSpace(firstNonEmptyText(state.RemediationProposalFallback, state.FallbackPlan)),
		Actions:      make([]RemediationAction, 0, len(state.RemediationProposalActions)),
	}
	for idx, summary := range state.RemediationProposalActions {
		summary = strings.TrimSpace(summary)
		if summary == "" {
			continue
		}
		proposal.Actions = append(proposal.Actions, RemediationAction{
			Step:            idx + 1,
			Goal:            summary,
			SuccessCriteria: "state-captured action should be executable by execution_agent",
		})
	}
	return proposal
}

func normalizeIncidentRisk(risk string) string {
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "low", "medium", "high", "critical":
		return strings.ToLower(strings.TrimSpace(risk))
	default:
		return ""
	}
}

func incidentTextClaimsExecution(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	claims := []string{
		"already executed",
		"already completed",
		"already fixed",
		"execution completed",
		"remediation completed",
		"已执行",
		"已完成",
		"已修复",
		"执行完成",
		"修复完成",
	}
	for _, claim := range claims {
		if strings.Contains(lower, strings.ToLower(claim)) {
			return true
		}
	}
	return false
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}
