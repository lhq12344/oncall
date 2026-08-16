package ops

import (
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

const incidentStateSessionKey = "incident_graph_state"

const (
	maxIncidentUserInputRunes = 1600
	maxIncidentStateRunes     = 2400
	maxIncidentExecutionLogs  = 200
)

func init() {
	gob.Register(&IncidentState{})
	gob.Register(&PlanState{})
	gob.Register(&PlanGateState{})
	gob.Register(&PlanVerificationState{})
	gob.Register(&ReplanState{})
	gob.Register(&PlanApprovalState{})
}

type PlanState struct {
	PlanID        string                   `json:"plan_id,omitempty"`
	Revision      int                      `json:"revision,omitempty"`
	Description   string                   `json:"description,omitempty"`
	RiskLevel     string                   `json:"risk_level,omitempty"`
	Steps         []GeneratedExecutionStep `json:"steps,omitempty"`
	StepSummaries []string                 `json:"step_summaries,omitempty"`
	TotalSteps    int                      `json:"total_steps,omitempty"`
	EstimatedTime int                      `json:"estimated_time,omitempty"`
	SnapshotHash  string                   `json:"snapshot_hash,omitempty"`
	GeneratedAt   string                   `json:"generated_at,omitempty"`
}

type PlanGateState struct {
	PlanID           string   `json:"plan_id,omitempty"`
	Revision         int      `json:"revision,omitempty"`
	SnapshotHash     string   `json:"snapshot_hash,omitempty"`
	Valid            bool     `json:"valid"`
	Blocked          bool     `json:"blocked,omitempty"`
	RequiresApproval bool     `json:"requires_approval,omitempty"`
	RiskLevel        string   `json:"risk_level,omitempty"`
	Reasons          []string `json:"reasons,omitempty"`
	UnsafeCommands   []string `json:"unsafe_commands,omitempty"`
	ReviewCommands   []string `json:"review_commands,omitempty"`
	ValidatedAt      string   `json:"validated_at,omitempty"`
}

type PlanVerificationState struct {
	PlanID       string `json:"plan_id,omitempty"`
	Revision     int    `json:"revision,omitempty"`
	Status       string `json:"status,omitempty"`
	Success      bool   `json:"success"`
	FailedStepID int    `json:"failed_step_id,omitempty"`
	Reason       string `json:"reason,omitempty"`
	VerifiedAt   string `json:"verified_at,omitempty"`
}

type ReplanState struct {
	Decision                  string `json:"decision,omitempty"`
	Reason                    string `json:"reason,omitempty"`
	Source                    string `json:"source,omitempty"`
	PlanID                    string `json:"plan_id,omitempty"`
	PlanRevision              int    `json:"plan_revision,omitempty"`
	ObservationRefreshNeeded  bool   `json:"observation_refresh_needed,omitempty"`
	RuntimeObservationSummary string `json:"runtime_observation_summary,omitempty"`
	UpdatedAt                 string `json:"updated_at,omitempty"`
}

type PlanApprovalState struct {
	PlanID         string `json:"plan_id,omitempty"`
	Revision       int    `json:"revision,omitempty"`
	SnapshotHash   string `json:"snapshot_hash,omitempty"`
	ApprovalStatus string `json:"approval_status,omitempty"`
	Approved       bool   `json:"approved,omitempty"`
	ApprovedBy     string `json:"approved_by,omitempty"`
	ApprovedAt     string `json:"approved_at,omitempty"`
	RejectedReason string `json:"rejected_reason,omitempty"`
	ApprovalScope  string `json:"approval_scope,omitempty"`
}

type IncidentState struct {
	ObservationCollected      bool     `json:"observation_collected,omitempty"`
	ObservationNamespace      string   `json:"observation_namespace,omitempty"`
	ObservationCollectedAt    string   `json:"observation_collected_at,omitempty"`
	ObservationTimeRange      string   `json:"observation_time_range,omitempty"`
	ObservationSummary        string   `json:"observation_summary,omitempty"`
	ObservationErrors         []string `json:"observation_errors,omitempty"`
	ObservationRefreshNeeded  bool     `json:"observation_refresh_needed,omitempty"`
	ObservationRefreshReason  string   `json:"observation_refresh_reason,omitempty"`
	RuntimeObservationSummary string   `json:"runtime_observation_summary,omitempty"`

	RootCause  string   `json:"root_cause,omitempty"`
	TargetNode string   `json:"target_node,omitempty"`
	Path       string   `json:"path,omitempty"`
	Impact     string   `json:"impact,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	Evidence   []string `json:"evidence,omitempty"`

	NextVerification    []string `json:"next_verification,omitempty"`
	MissingData         []string `json:"missing_data,omitempty"`
	RemediationIntent   string   `json:"remediation_intent,omitempty"`
	PlanningConstraints []string `json:"planning_constraints,omitempty"`
	FallbackGuidance    string   `json:"fallback_guidance,omitempty"`

	PlanID       string `json:"plan_id,omitempty"`      // 兼容旧字段：映射 remediation proposal id
	PlanSummary  string `json:"plan_summary,omitempty"` // 兼容旧字段：映射 remediation proposal summary
	PlanRisk     string `json:"plan_risk,omitempty"`    // 兼容旧字段：映射 remediation proposal risk
	FallbackPlan string `json:"fallback_plan,omitempty"`

	RemediationProposalID       string   `json:"remediation_proposal_id,omitempty"`
	RemediationProposalSummary  string   `json:"remediation_proposal_summary,omitempty"`
	RemediationProposalRisk     string   `json:"remediation_proposal_risk,omitempty"`
	RemediationProposalFallback string   `json:"remediation_proposal_fallback,omitempty"`
	RemediationProposalActions  []string `json:"remediation_proposal_actions,omitempty"`

	PlanState         *PlanState             `json:"plan_state,omitempty"`
	PlanGateState     *PlanGateState         `json:"plan_gate_state,omitempty"`
	PlanVerification  *PlanVerificationState `json:"plan_verification,omitempty"`
	ReplanState       *ReplanState           `json:"replan_state,omitempty"`
	PlanApprovalState *PlanApprovalState     `json:"plan_approval_state,omitempty"`

	IncidentContractValid  bool     `json:"incident_contract_valid,omitempty"`
	IncidentContractIssues []string `json:"incident_contract_issues,omitempty"`

	ValidationBlocked bool   `json:"validation_blocked,omitempty"`
	ValidationRisk    string `json:"validation_risk,omitempty"`

	ExecutionStatus          string   `json:"execution_status,omitempty"`
	ExecutionSuccess         bool     `json:"execution_success,omitempty"`
	ExecutionStepCount       int      `json:"execution_step_count,omitempty"`
	ExecutionReason          string   `json:"execution_reason,omitempty"`
	ExecutionFallback        string   `json:"execution_fallback,omitempty"`
	ExecutionOverallHealth   string   `json:"execution_overall_health,omitempty"`
	ExecutionFindings        []string `json:"execution_findings,omitempty"`
	ExecutionIssues          []string `json:"execution_issues,omitempty"`
	ExecutionRecommendations []string `json:"execution_recommendations,omitempty"`
	ExecutionPlanID          string   `json:"execution_plan_id,omitempty"`
	ExecutionPlanDesc        string   `json:"execution_plan_desc,omitempty"`
	ExecutionPlanRisk        string   `json:"execution_plan_risk,omitempty"`
	ExecutionPlanSteps       []string `json:"execution_plan_steps,omitempty"`
	ExecutionLogs            []string `json:"execution_logs,omitempty"`
	RepeatedIssueKey         string   `json:"repeated_issue_key,omitempty"`
	RepeatedIssueReason      string   `json:"repeated_issue_reason,omitempty"`
	RepeatedIssueRetryCount  int      `json:"repeated_issue_retry_count,omitempty"`
	RepeatedIssueRetryLimit  int      `json:"repeated_issue_retry_limit,omitempty"`
	RepeatedIssueEscalated   bool     `json:"repeated_issue_escalated,omitempty"`

	FinalStatus string `json:"final_status,omitempty"`
	FinalReport string `json:"final_report,omitempty"`

	UpdatedAt string `json:"updated_at,omitempty"`
}

type stateBridgeAgent struct {
	name   string
	desc   string
	stage  string
	inner  adk.Agent
	logger *zap.Logger
}

func wrapWithIncidentState(stage string, inner adk.Agent, logger *zap.Logger) adk.Agent {
	if inner == nil {
		return nil
	}
	return &stateBridgeAgent{
		name:   inner.Name(context.Background()),
		desc:   inner.Description(context.Background()),
		stage:  stage,
		inner:  inner,
		logger: logger,
	}
}

func (a *stateBridgeAgent) Name(_ context.Context) string {
	return a.name
}

func (a *stateBridgeAgent) Description(_ context.Context) string {
	return a.desc
}

func (a *stateBridgeAgent) Run(ctx context.Context, input *adk.AgentInput, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	agentName := strings.TrimSpace(a.name)
	if agentName != "" {
		adk.AddSessionValue(ctx, "current_agent", agentName)
	}
	iter := a.inner.Run(ctx, input, opts...)
	return a.track(ctx, iter)
}

func (a *stateBridgeAgent) Resume(ctx context.Context, info *adk.ResumeInfo, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	agentName := strings.TrimSpace(a.name)
	if agentName != "" {
		adk.AddSessionValue(ctx, "current_agent", agentName)
	}
	ra, ok := a.inner.(adk.ResumableAgent)
	if !ok {
		iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
		go func() {
			defer generator.Close()
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("agent %s is not resumable", a.name)})
		}()
		return iterator
	}
	return a.track(ctx, ra.Resume(ctx, info, opts...))
}

func (a *stateBridgeAgent) track(ctx context.Context, iter *adk.AsyncIterator[*adk.AgentEvent]) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer generator.Close()
		for {
			event, ok := iter.Next()
			if !ok {
				break
			}
			if event == nil {
				continue
			}
			a.captureState(ctx, event)
			generator.Send(event)
		}
	}()
	return iterator
}

func (a *stateBridgeAgent) captureState(ctx context.Context, event *adk.AgentEvent) {
	if event == nil {
		return
	}
	state := getIncidentState(ctx)
	agentName := strings.TrimSpace(event.AgentName)
	if agentName == "" {
		agentName = strings.TrimSpace(a.name)
	}

	if event.Err != nil {
		appendIncidentExecutionLog(state, fmt.Sprintf("[%s] 事件错误：%s", agentName, clipText(event.Err.Error(), 600)))
	}

	if event.Output != nil && event.Output.MessageOutput != nil && !event.Output.MessageOutput.IsStreaming && event.Output.MessageOutput.Message != nil {
		msg := event.Output.MessageOutput.Message
		if msg != nil {
			for _, call := range msg.ToolCalls {
				toolName := strings.TrimSpace(call.Function.Name)
				if toolName == "" {
					toolName = "(unknown)"
				}
				args := clipText(strings.TrimSpace(call.Function.Arguments), 280)
				if args == "" {
					args = "{}"
				}
				appendIncidentExecutionLog(state, fmt.Sprintf("[%s] 调用工具：%s args=%s", agentName, toolName, args))
			}
			if content := strings.TrimSpace(msg.Content); content != "" {
				appendIncidentExecutionLog(state, fmt.Sprintf("[%s] 输出：%s", agentName, clipText(content, 600)))
			}
			a.updateByStage(state, msg)
		}
	}

	if event.Action != nil && event.Action.Interrupted != nil {
		if info, ok := event.Action.Interrupted.Data.(*IncidentInterruptInfo); ok && info != nil {
			if strings.EqualFold(strings.TrimSpace(info.Type), "manual_required") {
				state.ExecutionStatus = "manual_required"
			}
			state.ExecutionReason = clipText(info.Reason, 600)
			if strings.TrimSpace(info.FallbackPlan) != "" {
				state.ExecutionFallback = clipText(info.FallbackPlan, 800)
			}
			appendIncidentExecutionLog(state, fmt.Sprintf("[%s] 中断：type=%s reason=%s", agentName, strings.TrimSpace(info.Type), clipText(info.Reason, 300)))
		} else if detail := strings.TrimSpace(fmt.Sprintf("%v", event.Action.Interrupted.Data)); detail != "" {
			appendIncidentExecutionLog(state, fmt.Sprintf("[%s] 中断：%s", agentName, clipText(detail, 300)))
		}
	}

	state.UpdatedAt = time.Now().Format(time.RFC3339)
	setIncidentState(ctx, state)
}

func (a *stateBridgeAgent) updateByStage(state *IncidentState, msg *schema.Message) {
	if state == nil || msg == nil {
		return
	}
	messages := []adk.Message{msg}
	switch a.stage {
	case "incident", "incident_analysis":
		if report, ok := parseRCAReport(messages); ok && report != nil {
			applyIncidentRCAReport(state, report)
		}
		if proposal, ok := parseRemediationProposal(messages); ok && proposal != nil {
			applyIncidentRemediationProposal(state, proposal)
		}
	case "rca":
		report, ok := parseRCAReport(messages)
		if !ok || report == nil {
			return
		}
		applyIncidentRCAReport(state, report)
	case "ops":
		proposal, ok := parseRemediationProposal(messages)
		if ok && proposal != nil {
			applyIncidentRemediationProposal(state, proposal)
		}
	case "plan":
		plan, ok := parseGeneratedExecutionPlan(messages)
		if ok && plan != nil {
			applyExecutionPlanState(state, plan)
		}
	case "plan_gate":
		validation, ok := parseValidationResult(messages)
		if ok && validation != nil {
			applyPlanGateValidationState(state, validation)
		}
	case "execution", "execute_plan", "verify_plan":
		if a.stage == "verify_plan" {
			if result, ok := parseExecutionResult(messages); ok && result != nil {
				applyPlanVerificationState(state, result)
			}
		}
		validation, ok := parseValidationResult(messages)
		if ok && validation != nil {
			state.ValidationBlocked = validation.Blocked
			state.ValidationRisk = strings.TrimSpace(validation.RiskLevel)
		}
		plan, ok := parseGeneratedExecutionPlan(messages)
		if ok && plan != nil {
			reason := "execution stage attempted to emit a new execution plan after plan approval"
			state.ExecutionStatus = "manual_required"
			state.ExecutionSuccess = false
			state.ExecutionReason = clipText(reason, 600)
			appendIncidentExecutionLog(state, "[execution] boundary violation: "+reason)
		}
		stepValidation, ok := parseStepValidationResult(messages)
		if ok && stepValidation != nil && stepValidation.ShouldStop {
			if strings.EqualFold(strings.TrimSpace(stepValidation.StopAction), "replan") {
				state.ExecutionStatus = "replan_required"
				state.ExecutionSuccess = false
				state.ObservationRefreshNeeded = true
				if reason := firstNonEmptyText(stepValidation.StopReason, stepValidation.MismatchReason, stepValidation.Message); reason != "" {
					state.ObservationRefreshReason = clipText(reason, 600)
				}
				if runtimeSummary := firstNonEmptyText(stepValidation.RuntimeSummary, stepValidation.MismatchReason, stepValidation.Actual); runtimeSummary != "" {
					state.RuntimeObservationSummary = clipText(runtimeSummary, 800)
				}
			} else {
				state.ExecutionStatus = "manual_required"
				state.ExecutionSuccess = false
			}
			state.ExecutionReason = clipText(firstNonEmptyText(stepValidation.StopReason, stepValidation.Message), 600)
		}
		status := detectExecutionStatus(messages)
		if status.Found {
			if status.Success {
				state.ExecutionStatus = "success"
			} else {
				state.ExecutionStatus = "failed"
			}
			state.ExecutionSuccess = status.Success
			state.ExecutionReason = clipText(status.RawMessageHint, 600)
		}
		result, ok := parseExecutionResult(messages)
		if ok && result != nil {
			if value, exists := result["execution_status"]; exists {
				if statusText, ok := value.(string); ok && strings.TrimSpace(statusText) != "" {
					nextStatus := strings.TrimSpace(statusText)
					if !(strings.EqualFold(state.ExecutionStatus, "replan_required") && strings.EqualFold(nextStatus, "manual_required")) {
						state.ExecutionStatus = nextStatus
					}
				}
			}
			state.ExecutionStepCount = parseExecutedStepCount(result)
			if value, exists := result["failed_reason"]; exists {
				if reason, ok := value.(string); ok && strings.TrimSpace(reason) != "" {
					state.ExecutionReason = clipText(reason, 600)
				}
			}
			if value, exists := result["manual_plan"]; exists {
				if manualPlan, ok := value.(string); ok && strings.TrimSpace(manualPlan) != "" {
					state.ExecutionFallback = clipText(manualPlan, 800)
				}
			}
			diagnostic := parseExecutionDiagnosticInsight(result)
			if health := strings.TrimSpace(diagnostic.OverallHealth); health != "" {
				state.ExecutionOverallHealth = clipText(health, 120)
			}
			if len(diagnostic.Findings) > 0 {
				state.ExecutionFindings = latestIncidentLogs(diagnostic.Findings, 5)
			}
			if len(diagnostic.Issues) > 0 {
				state.ExecutionIssues = latestIncidentLogs(diagnostic.Issues, 5)
			}
			if len(diagnostic.Recommendations) > 0 {
				state.ExecutionRecommendations = latestIncidentLogs(diagnostic.Recommendations, 5)
			}
			if diagnostic.ActionableIssueCount > 0 && strings.EqualFold(strings.TrimSpace(state.ExecutionStatus), "success") {
				reason := clipText(firstNonEmptyText(
					joinExecutionIssueSummaries(diagnostic.Issues, 2),
					diagnostic.Summary,
					state.ExecutionReason,
				), 600)
				state.ExecutionStatus = "replan_required"
				state.ExecutionSuccess = false
				state.ObservationRefreshNeeded = true
				state.ObservationRefreshReason = reason
				if diagnostic.Summary != "" {
					state.RuntimeObservationSummary = clipText(diagnostic.Summary, 800)
				}
				state.ExecutionReason = reason
			}
		}
	case "strategy":
		report, ok := parseStrategyReport(messages)
		if !ok || report == nil {
			return
		}
		if value, exists := report["final_status"]; exists {
			if finalStatus, ok := value.(string); ok && strings.TrimSpace(finalStatus) != "" {
				state.FinalStatus = strings.TrimSpace(finalStatus)
			}
		}
		if value, exists := report["summary"]; exists {
			if summary, ok := value.(string); ok && strings.TrimSpace(summary) != "" {
				state.FinalReport = clipText(summary, 800)
			}
		}
	}
}

func applyIncidentRCAReport(state *IncidentState, report *RCAReport) {
	if state == nil || report == nil {
		return
	}
	state.RootCause = strings.TrimSpace(report.RootCause)
	state.TargetNode = strings.TrimSpace(report.TargetNode)
	state.Path = strings.TrimSpace(report.Path)
	state.Impact = strings.TrimSpace(report.Impact)
	state.Confidence = report.Confidence
	state.Evidence = report.Evidence
	state.NextVerification = nonEmptyStrings(report.NextVerification)
	state.MissingData = nonEmptyStrings(report.MissingData)
	state.RemediationIntent = clipText(report.RemediationIntent, 600)
	state.PlanningConstraints = nonEmptyStrings(report.PlanningConstraints)
	state.FallbackGuidance = clipText(report.FallbackGuidance, 800)
}

func applyIncidentRemediationProposal(state *IncidentState, proposal *RemediationProposal) {
	if state == nil || proposal == nil {
		return
	}
	state.RemediationProposalID = strings.TrimSpace(proposal.ProposalID)
	state.RemediationProposalSummary = clipText(strings.TrimSpace(proposal.Summary), 600)
	state.RemediationProposalRisk = strings.TrimSpace(proposal.RiskLevel)
	state.RemediationProposalFallback = clipText(strings.TrimSpace(proposal.FallbackPlan), 800)
	state.RemediationProposalActions = summarizeRemediationActions(proposal)

	state.PlanID = state.RemediationProposalID
	state.PlanSummary = state.RemediationProposalSummary
	state.PlanRisk = state.RemediationProposalRisk
	state.FallbackPlan = state.RemediationProposalFallback
}

func applyExecutionPlanState(state *IncidentState, plan *GeneratedExecutionPlan) {
	if state == nil || plan == nil {
		return
	}

	snapshotHash := computeExecutionPlanSnapshotHash(plan)
	revision := 1
	planChanged := true
	if state.PlanState != nil {
		previousRevision := state.PlanState.Revision
		if previousRevision <= 0 {
			previousRevision = 1
		}
		if snapshotHash != "" && snapshotHash == strings.TrimSpace(state.PlanState.SnapshotHash) {
			revision = previousRevision
			planChanged = false
		} else {
			revision = previousRevision + 1
		}
	}

	stepSummaries := summarizeGeneratedExecutionPlan(plan)
	totalSteps := plan.TotalSteps
	if totalSteps <= 0 {
		totalSteps = len(plan.Steps)
	}
	state.PlanState = &PlanState{
		PlanID:        strings.TrimSpace(plan.PlanID),
		Revision:      revision,
		Description:   clipText(strings.TrimSpace(plan.Description), 600),
		RiskLevel:     strings.TrimSpace(plan.RiskLevel),
		Steps:         cloneGeneratedExecutionSteps(plan.Steps),
		StepSummaries: stepSummaries,
		TotalSteps:    totalSteps,
		EstimatedTime: plan.EstimatedTime,
		SnapshotHash:  snapshotHash,
		GeneratedAt:   time.Now().Format(time.RFC3339),
	}

	state.ExecutionPlanID = state.PlanState.PlanID
	state.ExecutionPlanDesc = state.PlanState.Description
	state.ExecutionPlanRisk = state.PlanState.RiskLevel
	state.ExecutionPlanSteps = append([]string(nil), stepSummaries...)

	if planChanged && state.PlanApprovalState != nil && !strings.EqualFold(strings.TrimSpace(state.PlanApprovalState.ApprovalStatus), "pending") {
		state.PlanApprovalState = &PlanApprovalState{
			PlanID:         state.PlanState.PlanID,
			Revision:       state.PlanState.Revision,
			SnapshotHash:   state.PlanState.SnapshotHash,
			ApprovalStatus: "pending",
			ApprovalScope:  "full_plan",
		}
	}
}

func applyPlanGateValidationState(state *IncidentState, validation *PlanValidationResult) {
	if state == nil || validation == nil {
		return
	}
	planID := strings.TrimSpace(validation.PlanID)
	revision := 0
	snapshotHash := ""
	if state.PlanState != nil {
		planID = strings.TrimSpace(firstNonEmptyText(planID, state.PlanState.PlanID))
		revision = state.PlanState.Revision
		snapshotHash = strings.TrimSpace(state.PlanState.SnapshotHash)
	}
	state.ValidationBlocked = validation.Blocked
	state.ValidationRisk = strings.TrimSpace(validation.RiskLevel)
	state.PlanGateState = &PlanGateState{
		PlanID:           planID,
		Revision:         revision,
		SnapshotHash:     snapshotHash,
		Valid:            validation.Valid,
		Blocked:          validation.Blocked,
		RequiresApproval: validation.RequiresConfirmation || planValidationRequiresApproval(validation),
		RiskLevel:        strings.TrimSpace(validation.RiskLevel),
		Reasons:          latestIncidentLogs(validation.Reasons, 6),
		UnsafeCommands:   latestIncidentLogs(validation.UnsafeCommands, 6),
		ReviewCommands:   latestIncidentLogs(validation.ReviewCommands, 6),
		ValidatedAt:      time.Now().Format(time.RFC3339),
	}
	if validation.Blocked || !validation.Valid {
		reason := "plan gate rejected canonical execution plan"
		if details := formatReasons(validation.Reasons); strings.TrimSpace(details) != "" {
			reason += ": " + details
		}
		applyReplanDecisionState(state, "refresh_observation", reason, "plan_gate", "")
	}
}

func applyPlanVerificationState(state *IncidentState, result map[string]any) {
	if state == nil || result == nil {
		return
	}
	status := strings.TrimSpace(stringFromMap(result, "verification_status"))
	if status == "" {
		status = strings.TrimSpace(stringFromMap(result, "execution_status"))
	}
	success, _ := boolFromAny(result["success"])
	reason := strings.TrimSpace(firstNonEmptyText(
		stringFromMap(result, "failed_reason"),
		stringFromMap(result, "reason"),
		state.ExecutionReason,
	))
	planID := canonicalPlanID(state)
	revision := statePlanRevision(state)
	if rawPlanID := strings.TrimSpace(stringFromMap(result, "plan_id")); rawPlanID != "" {
		planID = rawPlanID
	}
	if rawRevision := intFromAny(result["plan_revision"]); rawRevision > 0 {
		revision = rawRevision
	}
	state.PlanVerification = &PlanVerificationState{
		PlanID:       planID,
		Revision:     revision,
		Status:       status,
		Success:      success,
		FailedStepID: intFromAny(result["failed_step_id"]),
		Reason:       clipText(reason, 600),
		VerifiedAt:   time.Now().Format(time.RFC3339),
	}
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed)
		}
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &parsed); err == nil {
			return parsed
		}
	}
	return 0
}

func planValidationRequiresApproval(validation *PlanValidationResult) bool {
	if validation == nil {
		return false
	}
	if validation.RequiresConfirmation || len(validation.ReviewCommands) > 0 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(validation.RiskLevel)) {
	case "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func computeExecutionPlanSnapshotHash(plan *GeneratedExecutionPlan) string {
	if plan == nil {
		return ""
	}
	payload, err := json.Marshal(struct {
		PlanID        string
		Description   string
		Steps         []GeneratedExecutionStep
		TotalSteps    int
		EstimatedTime int
		RiskLevel     string
	}{
		PlanID:        strings.TrimSpace(plan.PlanID),
		Description:   strings.TrimSpace(plan.Description),
		Steps:         cloneGeneratedExecutionSteps(plan.Steps),
		TotalSteps:    plan.TotalSteps,
		EstimatedTime: plan.EstimatedTime,
		RiskLevel:     strings.TrimSpace(plan.RiskLevel),
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%x", sum)
}

func cloneGeneratedExecutionSteps(steps []GeneratedExecutionStep) []GeneratedExecutionStep {
	if len(steps) == 0 {
		return nil
	}
	out := make([]GeneratedExecutionStep, 0, len(steps))
	for _, step := range steps {
		copied := step
		copied.Args = append([]string(nil), step.Args...)
		copied.RollbackArgs = append([]string(nil), step.RollbackArgs...)
		out = append(out, copied)
	}
	return out
}

func applyReplanDecisionState(state *IncidentState, decision, reason, source, runtimeSummary string) {
	if state == nil {
		return
	}
	decision = normalizeReplanDecision(decision)
	if decision == "" {
		return
	}
	reason = clipText(reason, 600)
	runtimeSummary = clipText(runtimeSummary, 800)

	planID := strings.TrimSpace(state.ExecutionPlanID)
	planRevision := 0
	if state.PlanState != nil {
		planID = strings.TrimSpace(firstNonEmptyText(state.PlanState.PlanID, planID))
		planRevision = state.PlanState.Revision
	}

	switch decision {
	case "refresh_observation":
		state.ExecutionStatus = "replan_required"
		state.ExecutionSuccess = false
		state.ObservationRefreshNeeded = true
		if reason != "" {
			state.ObservationRefreshReason = reason
		}
		if runtimeSummary != "" {
			state.RuntimeObservationSummary = runtimeSummary
		}
	case "manual_required", "abort":
		state.ExecutionStatus = "manual_required"
		state.ExecutionSuccess = false
	case "complete":
		state.ExecutionStatus = "success"
		state.ExecutionSuccess = true
	}
	if reason != "" {
		state.ExecutionReason = reason
	}

	state.ReplanState = &ReplanState{
		Decision:                  decision,
		Reason:                    reason,
		Source:                    strings.TrimSpace(source),
		PlanID:                    planID,
		PlanRevision:              planRevision,
		ObservationRefreshNeeded:  decision == "refresh_observation",
		RuntimeObservationSummary: runtimeSummary,
		UpdatedAt:                 time.Now().Format(time.RFC3339),
	}
}

func normalizeReplanDecision(decision string) string {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "complete", "success", "succeeded", "resolved", "done":
		return "complete"
	case "refresh_observation", "replan_required", "replan", "revise_plan", "refresh", "retry":
		return "refresh_observation"
	case "manual_required", "approval_required", "validator_blocked", "blocked":
		return "manual_required"
	case "abort", "aborted", "cancelled", "canceled":
		return "abort"
	default:
		return ""
	}
}

func compactPlanStateForRender(plan *PlanState) map[string]any {
	if plan == nil {
		return nil
	}
	return map[string]any{
		"plan_id":        plan.PlanID,
		"revision":       plan.Revision,
		"description":    plan.Description,
		"risk_level":     plan.RiskLevel,
		"total_steps":    plan.TotalSteps,
		"estimated_time": plan.EstimatedTime,
		"snapshot_hash":  plan.SnapshotHash,
		"step_summaries": latestIncidentLogs(plan.StepSummaries, 4),
		"generated_at":   plan.GeneratedAt,
	}
}

func compactPlanGateStateForRender(gate *PlanGateState) map[string]any {
	if gate == nil {
		return nil
	}
	return map[string]any{
		"plan_id":           gate.PlanID,
		"revision":          gate.Revision,
		"snapshot_hash":     gate.SnapshotHash,
		"valid":             gate.Valid,
		"blocked":           gate.Blocked,
		"requires_approval": gate.RequiresApproval,
		"risk_level":        gate.RiskLevel,
		"reasons":           latestIncidentLogs(gate.Reasons, 4),
		"unsafe_commands":   latestIncidentLogs(gate.UnsafeCommands, 4),
		"review_commands":   latestIncidentLogs(gate.ReviewCommands, 4),
		"validated_at":      gate.ValidatedAt,
	}
}

func compactPlanVerificationForRender(verification *PlanVerificationState) map[string]any {
	if verification == nil {
		return nil
	}
	return map[string]any{
		"plan_id":        verification.PlanID,
		"revision":       verification.Revision,
		"status":         verification.Status,
		"success":        verification.Success,
		"failed_step_id": verification.FailedStepID,
		"reason":         verification.Reason,
		"verified_at":    verification.VerifiedAt,
	}
}

func incidentHistoryRewriter(ctx context.Context, entries []*adk.HistoryEntry) ([]adk.Message, error) {
	out := make([]adk.Message, 0, 3)
	lastUser := findLastUserInput(entries)
	if lastUser != nil {
		latestQuestion := clipText(lastUser.Content, maxIncidentUserInputRunes)
		if strings.TrimSpace(latestQuestion) != "" {
			out = append(out, schema.UserMessage(latestQuestion))
		}
	}

	state := getIncidentState(ctx)
	if state == nil {
		return out, nil
	}
	stateText := renderIncidentState(state)
	if strings.TrimSpace(stateText) != "" {
		out = append(out, schema.UserMessage(stateText))
	}
	return out, nil
}

func findLastUserInput(entries []*adk.HistoryEntry) adk.Message {
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry == nil || !entry.IsUserInput || entry.Message == nil {
			continue
		}
		if entry.Message.Role != schema.User {
			continue
		}
		return entry.Message
	}
	return nil
}

func renderIncidentState(state *IncidentState) string {
	if state == nil {
		return ""
	}
	payload := map[string]any{
		"observation_collected":         state.ObservationCollected,
		"observation_namespace":         state.ObservationNamespace,
		"observation_collected_at":      state.ObservationCollectedAt,
		"observation_time_range":        state.ObservationTimeRange,
		"observation_summary":           state.ObservationSummary,
		"observation_errors":            state.ObservationErrors,
		"observation_refresh_needed":    state.ObservationRefreshNeeded,
		"observation_refresh_reason":    state.ObservationRefreshReason,
		"runtime_observation_summary":   state.RuntimeObservationSummary,
		"root_cause":                    state.RootCause,
		"target_node":                   state.TargetNode,
		"path":                          state.Path,
		"impact":                        state.Impact,
		"confidence":                    state.Confidence,
		"next_verification":             latestIncidentLogs(state.NextVerification, 4),
		"missing_data":                  latestIncidentLogs(state.MissingData, 4),
		"remediation_intent":            state.RemediationIntent,
		"planning_constraints":          latestIncidentLogs(state.PlanningConstraints, 4),
		"fallback_guidance":             state.FallbackGuidance,
		"plan_id":                       state.PlanID,
		"plan_summary":                  state.PlanSummary,
		"plan_risk":                     state.PlanRisk,
		"fallback_plan":                 state.FallbackPlan,
		"remediation_proposal_id":       state.RemediationProposalID,
		"remediation_proposal_summary":  state.RemediationProposalSummary,
		"remediation_proposal_risk":     state.RemediationProposalRisk,
		"remediation_proposal_fallback": state.RemediationProposalFallback,
		"remediation_proposal_actions":  latestIncidentLogs(state.RemediationProposalActions, 4),
		"plan_state":                    compactPlanStateForRender(state.PlanState),
		"plan_gate_state":               compactPlanGateStateForRender(state.PlanGateState),
		"plan_verification":             compactPlanVerificationForRender(state.PlanVerification),
		"replan_state":                  state.ReplanState,
		"plan_approval_state":           state.PlanApprovalState,
		"incident_contract_valid":       state.IncidentContractValid,
		"incident_contract_issues":      latestIncidentLogs(state.IncidentContractIssues, 4),
		"validation_blocked":            state.ValidationBlocked,
		"validation_risk":               state.ValidationRisk,
		"execution_status":              state.ExecutionStatus,
		"execution_success":             state.ExecutionSuccess,
		"execution_step_count":          state.ExecutionStepCount,
		"execution_reason":              state.ExecutionReason,
		"execution_fallback":            state.ExecutionFallback,
		"execution_overall_health":      state.ExecutionOverallHealth,
		"execution_findings":            latestIncidentLogs(state.ExecutionFindings, 4),
		"execution_issues":              latestIncidentLogs(state.ExecutionIssues, 4),
		"execution_recommendations":     latestIncidentLogs(state.ExecutionRecommendations, 4),
		"execution_plan_id":             state.ExecutionPlanID,
		"execution_plan_desc":           state.ExecutionPlanDesc,
		"execution_plan_risk":           state.ExecutionPlanRisk,
		"execution_plan_steps":          latestIncidentLogs(state.ExecutionPlanSteps, 4),
		"execution_log_count":           len(state.ExecutionLogs),
		"latest_execution_logs":         latestIncidentLogs(state.ExecutionLogs, 5),
		"repeated_issue_reason":         state.RepeatedIssueReason,
		"repeated_issue_retry_count":    state.RepeatedIssueRetryCount,
		"repeated_issue_retry_limit":    state.RepeatedIssueRetryLimit,
		"repeated_issue_escalated":      state.RepeatedIssueEscalated,
		"final_status":                  state.FinalStatus,
		"final_report":                  state.FinalReport,
		"updated_at":                    state.UpdatedAt,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return clipText("当前工作流结构化状态（Graph State）：\n"+string(body), maxIncidentStateRunes)
}

func clipText(text string, maxRunes int) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if maxRunes <= 0 {
		maxRunes = 512
	}
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	return string(runes[:maxRunes]) + "..."
}

func getIncidentState(ctx context.Context) *IncidentState {
	value, ok := adk.GetSessionValue(ctx, incidentStateSessionKey)
	if !ok || value == nil {
		return &IncidentState{}
	}
	switch typed := value.(type) {
	case *IncidentState:
		copyState := *typed
		return &copyState
	case IncidentState:
		copyState := typed
		return &copyState
	default:
		return &IncidentState{}
	}
}

func setIncidentState(ctx context.Context, state *IncidentState) {
	if state == nil {
		return
	}
	adk.AddSessionValue(ctx, incidentStateSessionKey, state)
}

// appendIncidentExecutionLog 追加执行日志并控制总量。
// 输入：state（流程状态）、entry（日志文本）。
// 输出：无。
func appendIncidentExecutionLog(state *IncidentState, entry string) {
	if state == nil {
		return
	}
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return
	}
	state.ExecutionLogs = append(state.ExecutionLogs, entry)
	if len(state.ExecutionLogs) > maxIncidentExecutionLogs {
		state.ExecutionLogs = append([]string(nil), state.ExecutionLogs[len(state.ExecutionLogs)-maxIncidentExecutionLogs:]...)
	}
}

// latestIncidentLogs 返回日志尾部 N 条。
// 输入：logs（日志列表）、limit（条数上限）。
// 输出：尾部日志切片。
func latestIncidentLogs(logs []string, limit int) []string {
	if len(logs) == 0 {
		return nil
	}
	if limit <= 0 || len(logs) <= limit {
		return append([]string(nil), logs...)
	}
	return append([]string(nil), logs[len(logs)-limit:]...)
}

// summarizeRemediationActions 将修复提案动作转换为简明文本。
// 输入：修复提案。
// 输出：动作摘要列表。
func summarizeRemediationActions(proposal *RemediationProposal) []string {
	if proposal == nil || len(proposal.Actions) == 0 {
		return nil
	}
	out := make([]string, 0, len(proposal.Actions))
	for _, action := range proposal.Actions {
		line := fmt.Sprintf("步骤 %d：%s", action.Step, firstNonEmptyText(action.Goal, "未命名动作"))
		if rationale := strings.TrimSpace(action.Rationale); rationale != "" {
			line += fmt.Sprintf("；理由=%s", rationale)
		}
		if hint := strings.TrimSpace(action.CommandHint); hint != "" {
			line += fmt.Sprintf("；命令提示=%s", hint)
		}
		if success := strings.TrimSpace(action.SuccessCriteria); success != "" {
			line += fmt.Sprintf("；成功判据=%s", success)
		}
		if rollback := strings.TrimSpace(action.RollbackHint); rollback != "" {
			line += fmt.Sprintf("；回退=%s", rollback)
		}
		out = append(out, clipText(line, 320))
	}
	return out
}

// summarizeGeneratedExecutionPlan 将结构化计划转换为可展示的步骤摘要。
// 输入：execution_agent 生成的计划。
// 输出：步骤摘要列表。
func summarizeGeneratedExecutionPlan(plan *GeneratedExecutionPlan) []string {
	if plan == nil || len(plan.Steps) == 0 {
		return nil
	}
	out := make([]string, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		command := strings.TrimSpace(step.Command)
		if len(step.Args) > 0 {
			command = strings.TrimSpace(command + " " + strings.Join(step.Args, " "))
		}
		line := fmt.Sprintf("步骤 %d：%s", step.StepID, firstNonEmptyText(step.Description, "执行命令"))
		if command != "" {
			line += fmt.Sprintf("；命令=%s", command)
		}
		if expected := strings.TrimSpace(step.ExpectedResult); expected != "" {
			line += fmt.Sprintf("；预期=%s", expected)
		}
		if rollback := strings.TrimSpace(step.RollbackCommand); rollback != "" {
			if len(step.RollbackArgs) > 0 {
				rollback = strings.TrimSpace(rollback + " " + strings.Join(step.RollbackArgs, " "))
			}
			line += fmt.Sprintf("；回滚=%s", rollback)
		}
		out = append(out, clipText(line, 320))
	}
	return out
}

// firstNonEmptyText 返回第一个非空文本。
// 输入：候选文本列表。
// 输出：第一个去空白后非空的文本；若都为空返回空字符串。
func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}
