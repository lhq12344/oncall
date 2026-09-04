package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	executiontools "go_agent/internal/tools/execution"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
)

type diagnosisGateAgent struct {
	name   string
	desc   string
	logger *zap.Logger
}

func newDiagnosisGateAgent(logger *zap.Logger) adk.Agent {
	return &diagnosisGateAgent{
		name:   "diagnosis_gate",
		desc:   "validates incident diagnosis evidence before planning",
		logger: logger,
	}
}

func (a *diagnosisGateAgent) Name(_ context.Context) string {
	return a.name
}

func (a *diagnosisGateAgent) Description(_ context.Context) string {
	return a.desc
}

func (a *diagnosisGateAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer generator.Close()

		var messages []adk.Message
		if input != nil {
			messages = input.Messages
		}
		state := getIncidentState(ctx)
		result := validateIncidentDiagnosis(messages, state)
		applyIncidentContractValidationForGate(ctx, state, result, a.name)

		if result.Valid {
			if a.logger != nil {
				a.logger.Info("incident diagnosis contract passed",
					zap.String("gate", a.name),
					zap.String("risk", result.RiskLevel),
					zap.Int("evidence_count", result.EvidenceCount))
			}
			generator.Send(assistantEvent(a.name + " passed: incident evidence is sufficient for the next workflow stage."))
			return
		}

		if a.logger != nil {
			a.logger.Warn("incident diagnosis contract blocked workflow", zap.String("gate", a.name), zap.Strings("issues", result.Issues))
		}
		generator.Send(assistantEvent(fmt.Sprintf(
			"%s blocked workflow and returned to ops_incident_agent for replanning: %s",
			a.name,
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
	if allowed, reason := executionGuardAllowsExecution(state); !allowed {
		iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
		go func() {
			defer generator.Close()
			if a.logger != nil {
				a.logger.Warn("skip execution because incident contract is invalid", zap.String("reason", reason))
			}
			generator.Send(assistantEvent("execution_agent skipped: " + reason))
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
	if err := prepareExecutionToolStateFromApprovedPlan(ctx, state); err != nil {
		iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
		go func() {
			defer generator.Close()
			if a.logger != nil {
				a.logger.Warn("skip execution because approved plan could not seed execution tool state", zap.Error(err))
			}
			generator.Send(assistantEvent("execution_agent skipped: " + err.Error()))
		}()
		return iterator
	}
	return a.inner.Run(ctx, input, opts...)
}

func (a *contractGuardedExecutionAgent) Resume(ctx context.Context, info *adk.ResumeInfo, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	state := getIncidentState(ctx)
	if allowed, reason := executionGuardAllowsExecution(state); !allowed {
		iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
		go func() {
			defer generator.Close()
			if a.logger != nil {
				a.logger.Warn("skip execution resume because incident contract or plan gate is invalid", zap.String("reason", reason))
			}
			generator.Send(assistantEvent("execution_agent resume skipped: " + reason))
		}()
		return iterator
	}
	if err := prepareExecutionToolStateFromApprovedPlan(ctx, state); err != nil {
		iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
		go func() {
			defer generator.Close()
			if a.logger != nil {
				a.logger.Warn("skip execution resume because approved plan could not seed execution tool state", zap.Error(err))
			}
			generator.Send(assistantEvent("execution_agent resume skipped: " + err.Error()))
		}()
		return iterator
	}
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

func executionGuardAllowsExecution(state *IncidentState) (bool, string) {
	if allowed, reason := incidentContractAllowsExecution(state); !allowed {
		return false, reason
	}
	if state == nil || state.PlanState == nil || strings.TrimSpace(state.PlanState.PlanID) == "" {
		return false, "canonical plan is missing; run plan_agent before execution_agent"
	}
	if state.PlanGateState == nil {
		return false, "plan_gate has not validated the canonical plan"
	}
	if !planGateMatchesCurrentPlan(state) {
		return false, "plan_gate result is stale for the current plan snapshot"
	}
	if state.PlanGateState.Blocked || !state.PlanGateState.Valid {
		return false, "plan_gate blocked the canonical plan"
	}
	if !currentPlanApproved(state) {
		return false, "plan_approval has not approved the current full plan snapshot"
	}
	return true, ""
}

func prepareExecutionToolStateFromApprovedPlan(ctx context.Context, state *IncidentState) error {
	if allowed, reason := executionGuardAllowsExecution(state); !allowed {
		return errors.New(reason)
	}
	plan := planStateToExecutionToolPlan(state.PlanState)
	if plan == nil || strings.TrimSpace(plan.PlanID) == "" || len(plan.Steps) == 0 {
		return fmt.Errorf("approved canonical plan is empty or invalid")
	}
	return executiontools.PrepareApprovedExecutionPlanFromGraphState(ctx, plan)
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

func validateIncidentDiagnosis(messages []adk.Message, state *IncidentState) incidentContractValidation {
	report, _ := parseRCAReport(messages)
	if report == nil {
		report = rcaReportFromState(state)
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
		return result
	}
	result.Confidence = report.Confidence
	result.EvidenceCount = len(nonEmptyStrings(report.Evidence))
	if strings.TrimSpace(report.RootCause) == "" {
		addIssue("missing_root_cause")
	}
	if result.EvidenceCount < 2 {
		addIssue("insufficient_evidence")
	}
	if report.Confidence <= 0 || report.Confidence > 1 {
		addIssue("invalid_confidence")
	} else if report.Confidence < 0.35 {
		addIssue("confidence_too_low")
	}
	return result
}

func applyIncidentContractValidation(ctx context.Context, state *IncidentState, result incidentContractValidation) {
	applyIncidentContractValidationForGate(ctx, state, result, "diagnosis_gate")
}

func applyIncidentContractValidationForGate(ctx context.Context, state *IncidentState, result incidentContractValidation, gateName string) {
	if state == nil {
		state = &IncidentState{}
	}
	gateName = strings.TrimSpace(firstNonEmptyText(gateName, "diagnosis_gate"))
	state.IncidentContractValid = result.Valid
	state.IncidentContractIssues = append([]string(nil), result.Issues...)
	state.UpdatedAt = time.Now().Format(time.RFC3339)
	if result.Valid {
		state.ValidationBlocked = false
		if strings.EqualFold(strings.TrimSpace(state.ValidationRisk), "contract_invalid") {
			state.ValidationRisk = ""
		}
		appendIncidentExecutionLog(state, "["+gateName+"] contract passed")
		setIncidentState(ctx, state)
		return
	}
	state.ValidationBlocked = true
	state.ValidationRisk = "contract_invalid"
	applyReplanDecisionState(state, "refresh_observation", gateName+" invalid: "+strings.Join(result.Issues, "; "), gateName, "")
	appendIncidentExecutionLog(state, "["+gateName+"] blocked workflow: "+strings.Join(result.Issues, "; "))
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
			SuccessCriteria: "state-captured action should be executable by execute_plan",
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
