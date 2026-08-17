package ops

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestValidateIncidentContractAcceptsValidProposal(t *testing.T) {
	t.Parallel()

	result := validateIncidentContract([]adk.Message{incidentContractMessage(validIncidentContractJSON())}, nil)

	if !result.Valid {
		t.Fatalf("expected valid contract, got issues: %v", result.Issues)
	}
	if result.RiskLevel != "medium" || result.EvidenceCount != 2 {
		t.Fatalf("unexpected validation metadata: %+v", result)
	}
}

func TestValidateIncidentContractRejectsEmptyActions(t *testing.T) {
	t.Parallel()

	json := `{
		"root_cause": "pod_restart",
		"target_node": "infra/api-0",
		"impact": "api unavailable",
		"confidence": 0.76,
		"evidence": ["restart count increased", "error logs matched"],
		"proposal_id": "proposal_1",
		"summary": "inspect pod state",
		"risk_level": "low",
		"actions": [],
		"fallback_plan": "manual pod event inspection"
	}`
	result := validateIncidentContract([]adk.Message{incidentContractMessage(json)}, nil)

	assertIncidentContractIssue(t, result, "empty_actions")
}

func TestValidateIncidentContractRejectsLowEvidenceAndHighRiskWithoutFallback(t *testing.T) {
	t.Parallel()

	json := `{
		"root_cause": "deployment_bad_config",
		"confidence": 0.4,
		"evidence": [],
		"summary": "roll back deployment",
		"risk_level": "high",
		"actions": [{
			"step": 1,
			"goal": "roll back deployment",
			"command_hint": "kubectl rollout undo deploy/api -n infra",
			"success_criteria": "deployment rollout completes"
		}]
	}`
	result := validateIncidentContract([]adk.Message{incidentContractMessage(json)}, nil)

	assertIncidentContractIssue(t, result, "missing_evidence")
	assertIncidentContractIssue(t, result, "high_risk_missing_fallback")
	assertIncidentContractIssue(t, result, "low_confidence_missing_fallback")
}

func TestValidateIncidentContractRejectsClaimedExecution(t *testing.T) {
	t.Parallel()

	json := strings.Replace(validIncidentContractJSON(), "restart unhealthy pod after confirming current state", "already executed rollout restart", 1)
	json = strings.Replace(json, "validate current state before remediation", "already completed the restart", 1)
	result := validateIncidentContract([]adk.Message{incidentContractMessage(json)}, nil)

	assertIncidentContractIssue(t, result, "proposal_claims_execution")
	assertIncidentContractIssue(t, result, "action_1_claims_execution")
}

func TestValidateIncidentDiagnosisRejectsSingleEvidence(t *testing.T) {
	t.Parallel()

	json := `{
		"root_cause": "pod_restart",
		"target_node": "infra/api-0",
		"impact": "api unavailable",
		"confidence": 0.76,
		"evidence": ["restart count increased"]
	}`
	result := validateIncidentDiagnosis([]adk.Message{incidentContractMessage(json)}, nil)

	assertIncidentContractIssue(t, result, "insufficient_evidence")
}

func TestInferFinalStatusResolvedWithoutStrategyStage(t *testing.T) {
	t.Parallel()

	state := &IncidentState{
		RootCause:                  "pod_restart",
		ExecutionSuccess:           true,
		Evidence:                   []string{"restart count increased"},
		Confidence:                 0.8,
		RemediationProposalActions: []string{"confirm pod health"},
	}
	state.FinalStatus = inferFinalStatus(state)
	state.FinalReport = buildFinalOpsSummary(state)

	if state.FinalStatus != "resolved" {
		t.Fatalf("FinalStatus = %q, want resolved", state.FinalStatus)
	}
	if eligible, reasons := finalOpsArchiveEligibility(state, state.FinalReport); !eligible {
		t.Fatalf("expected archive eligible final report, reasons: %v", reasons)
	}
}

func TestInferFinalStatusPlanVerificationFailureIsUnresolved(t *testing.T) {
	t.Parallel()

	state := &IncidentState{
		ExecutionSuccess: true,
		PlanVerification: &PlanVerificationState{
			Status:       "failed",
			Success:      false,
			FailedStepID: 7,
			Reason:       "success criteria mismatch",
		},
	}
	if got := inferFinalStatus(state); got != "unresolved" {
		t.Fatalf("FinalStatus = %q, want unresolved when verify_plan fails", got)
	}
}

func TestBuildFinalOpsSummaryIncludesCanonicalPlanAndReplan(t *testing.T) {
	t.Parallel()

	state := &IncidentState{
		RootCause:       "pod_restart",
		TargetNode:      "infra/api-0",
		Evidence:        []string{"restart count increased"},
		Confidence:      0.8,
		ExecutionStatus: "replan_required",
		PlanState: &PlanState{
			PlanID:        "plan_001",
			Revision:      2,
			Description:   "canonical pod health inspection",
			RiskLevel:     "low",
			StepSummaries: []string{"step 1 check pod status"},
			SnapshotHash:  "abcdef1234567890",
		},
		ReplanState: &ReplanState{
			Decision:     "refresh_observation",
			Reason:       "runtime state changed",
			PlanID:       "plan_001",
			PlanRevision: 2,
		},
		PlanVerification: &PlanVerificationState{
			PlanID:       "plan_001",
			Revision:     2,
			Status:       "failed",
			Success:      false,
			FailedStepID: 7,
			Reason:       "runtime state changed during verification",
		},
	}

	report := buildFinalOpsSummary(state)
	if !strings.Contains(report, "plan_001") {
		t.Fatalf("final report missing canonical plan id: %s", report)
	}
	if !strings.Contains(report, "refresh_observation") {
		t.Fatalf("final report missing replan decision: %s", report)
	}
	if !strings.Contains(report, "verify_plan") || !strings.Contains(report, "failed_step_id=7") {
		t.Fatalf("final report missing plan verification result: %s", report)
	}
	if statePlanRevision(state) != 2 {
		t.Fatalf("plan revision helper = %d, want 2", statePlanRevision(state))
	}
	if stateReplanCount(state) != 1 {
		t.Fatalf("replan count helper = %d, want 1", stateReplanCount(state))
	}
	if stateTerminalDecision(state) != "refresh_observation" {
		t.Fatalf("terminal decision = %q, want refresh_observation", stateTerminalDecision(state))
	}
	if statePlanVerificationStatus(state) != "failed" || statePlanVerificationFailedStepID(state) != 7 {
		t.Fatalf("verification helpers returned unexpected values: status=%q step=%d", statePlanVerificationStatus(state), statePlanVerificationFailedStepID(state))
	}
}

func TestPersistFinalOpsReportIncludesPlanMetadata(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	}()

	state := &IncidentState{
		FinalStatus:     "unresolved",
		ExecutionStatus: "failed",
		PlanState: &PlanState{
			PlanID:   "plan_001",
			Revision: 3,
		},
		PlanVerification: &PlanVerificationState{
			Status:       "failed",
			Success:      false,
			FailedStepID: 7,
			Reason:       "verification mismatch",
		},
		ReplanState: &ReplanState{
			Decision: "manual_required",
			Reason:   "retry budget reached",
		},
	}

	path, err := persistFinalOpsReport(context.Background(), state, "## report\nbody")
	if err != nil {
		t.Fatalf("persist report: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	text := string(content)
	for _, want := range []string{
		"plan_id: plan_001",
		"plan_revision: 3",
		"verification_status: failed",
		"verification_success: false",
		"verification_failed_step_id: 7",
		"replan_count: 1",
		"terminal_decision: manual_required",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("persisted report missing %q:\n%s", want, text)
		}
	}
}

func TestFinalOpsArchiveEligibilityRejectsUnknownStatus(t *testing.T) {
	t.Parallel()

	state := &IncidentState{
		FinalStatus: "unknown",
		Evidence:    []string{"some evidence"},
		Confidence:  0.7,
	}
	eligible, reasons := finalOpsArchiveEligibility(state, strings.Repeat("report ", 20))
	if eligible {
		t.Fatal("expected unknown status to fail archive eligibility")
	}
	if !containsString(reasons, "missing_final_status") {
		t.Fatalf("reasons = %v, want missing_final_status", reasons)
	}
}

func TestContractGuardedExecutionSkipsInvalidContract(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	innerRan := false
	guard := newContractGuardedExecutionAgent(incidentRunFuncAgent{
		name: "execution_agent",
		run: func(context.Context, *adk.AgentInput, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
			innerRan = true
			iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
			go func() {
				defer generator.Close()
				generator.Send(assistantEvent("inner ran"))
			}()
			return iterator
		},
	}, nil)

	events := collectIncidentContractEvents(guard.Run(ctx, &adk.AgentInput{}))
	if innerRan {
		t.Fatal("execution inner agent ran despite invalid incident contract")
	}
	if len(events) == 0 {
		t.Fatal("expected guard to emit skip event")
	}
	if got := events[0].Output.MessageOutput.Message.Content; !strings.Contains(got, "skipped") {
		t.Fatalf("unexpected guard output: %q", got)
	}
}

func TestContractGuardedExecutionResumeRechecksPlanGuard(t *testing.T) {
	t.Parallel()

	resumeRan := false
	guard := newContractGuardedExecutionAgent(incidentResumableFuncAgent{
		incidentRunFuncAgent: incidentRunFuncAgent{
			name: "execution_agent",
			run: func(context.Context, *adk.AgentInput, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
				t.Fatal("run should not be called by resume")
				return nil
			},
		},
		resume: func(context.Context, *adk.ResumeInfo, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
			resumeRan = true
			iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
			go func() {
				defer generator.Close()
				generator.Send(assistantEvent("inner resumed"))
			}()
			return iterator
		},
	}, nil)

	resumable, ok := guard.(adk.ResumableAgent)
	if !ok {
		t.Fatal("expected guarded execution agent to be resumable")
	}
	events := collectIncidentContractEvents(resumable.Resume(context.Background(), &adk.ResumeInfo{WasInterrupted: true, IsResumeTarget: true}))
	if resumeRan {
		t.Fatal("execution inner agent resumed despite missing canonical plan approval")
	}
	if len(events) == 0 {
		t.Fatal("expected resume guard to emit skip event")
	}
	if got := events[0].Output.MessageOutput.Message.Content; !strings.Contains(got, "resume skipped") {
		t.Fatalf("unexpected guard output: %q", got)
	}
}

func TestIncidentContractAllowsExecutionRequiresValidGate(t *testing.T) {
	t.Parallel()

	if allowed, reason := incidentContractAllowsExecution(&IncidentState{
		IncidentContractValid:  false,
		IncidentContractIssues: []string{"missing_evidence"},
	}); allowed || reason != "missing_evidence" {
		t.Fatalf("invalid contract allowed=%v reason=%q", allowed, reason)
	}
	if allowed, reason := incidentContractAllowsExecution(&IncidentState{IncidentContractValid: true}); !allowed || reason != "" {
		t.Fatalf("valid contract allowed=%v reason=%q", allowed, reason)
	}
}

func TestApplyIncidentContractValidationWritesCanonicalReplanDecision(t *testing.T) {
	t.Parallel()

	state := &IncidentState{}
	applyIncidentContractValidation(context.Background(), state, incidentContractValidation{
		Valid:  false,
		Issues: []string{"missing_evidence"},
	})

	if state.ReplanState == nil {
		t.Fatal("expected contract gate to write ReplanState")
	}
	if state.ReplanState.Decision != "refresh_observation" {
		t.Fatalf("decision = %q, want refresh_observation", state.ReplanState.Decision)
	}
	if state.ExecutionStatus != "replan_required" || !state.ObservationRefreshNeeded {
		t.Fatalf("expected replan-required refresh state, got status=%q refresh=%v", state.ExecutionStatus, state.ObservationRefreshNeeded)
	}
	if !strings.Contains(state.ExecutionReason, "missing_evidence") {
		t.Fatalf("execution reason missing issue: %q", state.ExecutionReason)
	}
}

func validIncidentContractJSON() string {
	return `{
		"root_cause": "pod_restart",
		"target_node": "infra/api-0",
		"impact": "api unavailable",
		"confidence": 0.76,
		"evidence": ["restart count increased", "error logs matched"],
		"proposal_id": "proposal_1",
		"summary": "restart unhealthy pod after confirming current state",
		"risk_level": "medium",
		"actions": [{
			"step": 1,
			"goal": "confirm pod health",
			"rationale": "validate current state before remediation",
			"command_hint": "kubectl get pod api-0 -n infra",
			"success_criteria": "pod status is Running",
			"rollback_hint": "no rollback required for read-only check",
			"read_only": true
		}],
		"fallback_plan": "manual pod event inspection"
	}`
}

type incidentRunFuncAgent struct {
	name string
	run  func(context.Context, *adk.AgentInput, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent]
}

func (a incidentRunFuncAgent) Name(context.Context) string {
	return a.name
}

func (a incidentRunFuncAgent) Description(context.Context) string {
	return "test agent " + a.name
}

func (a incidentRunFuncAgent) Run(ctx context.Context, input *adk.AgentInput, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	return a.run(ctx, input, opts...)
}

type incidentResumableFuncAgent struct {
	incidentRunFuncAgent
	resume func(context.Context, *adk.ResumeInfo, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent]
}

func (a incidentResumableFuncAgent) Resume(ctx context.Context, info *adk.ResumeInfo, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	return a.resume(ctx, info, opts...)
}

func collectIncidentContractEvents(iter *adk.AsyncIterator[*adk.AgentEvent]) []*adk.AgentEvent {
	var events []*adk.AgentEvent
	for {
		event, ok := iter.Next()
		if !ok {
			return events
		}
		if event != nil {
			events = append(events, event)
		}
	}
}

func incidentContractMessage(content string) adk.Message {
	return &schema.Message{Content: content}
}

func assertIncidentContractIssue(t *testing.T, result incidentContractValidation, issue string) {
	t.Helper()
	if result.Valid {
		t.Fatalf("expected invalid contract containing %q, got valid", issue)
	}
	if !containsString(result.Issues, issue) {
		t.Fatalf("issues = %v, want %q", result.Issues, issue)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
