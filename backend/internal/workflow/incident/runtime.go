package incident

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"go_agent/internal/events"
	"go_agent/internal/evidence"
	"go_agent/internal/telemetry"
	"go_agent/internal/tools/policy"
	"go_agent/internal/workflow/catalog"
)

type Fixture struct {
	ID                  string   `json:"id"`
	Version             string   `json:"version"`
	ExpectedTerminal    Terminal `json:"expected_terminal"`
	Evidence            []string `json:"evidence"`
	DiagnosisConfidence float64  `json:"diagnosis_confidence"`
	PlanRisk            string   `json:"plan_risk"`
	Approval            string   `json:"approval"`
	Verify              string   `json:"verify"`
}

type Runtime struct {
	Catalog    *catalog.Catalog
	Policy     *policy.Engine
	Events     *events.Emitter
	Telemetry  *telemetry.Recorder
	MaxReplans int
}

func (r Runtime) Run(ctx context.Context, f Fixture) (State, error) {
	def, err := r.catalog().Resolve(catalog.IncidentWorkflow, catalog.CurrentIncidentVersion)
	if err != nil {
		return State{}, err
	}
	state := NewState(def.Version)
	state.IncidentID = f.ID
	if r.MaxReplans <= 0 {
		r.MaxReplans = 1
	}
	for _, node := range []string{"normalize", "collect_evidence_parallel", "diagnose_agent", "diagnosis_gate", "plan_agent", "validate_plan", "approval", "execute", "verify", "final_report"} {
		if err := r.runNode(ctx, node, &state, f); err != nil {
			return state, err
		}
		if state.Terminal == TerminalFailed || state.Terminal == TerminalWaiting || state.Terminal == TerminalComplete {
			break
		}
	}
	if state.Terminal == "" {
		state.Terminal = TerminalFailed
	}
	return state, nil
}

func (r Runtime) runNode(ctx context.Context, node string, state *State, f Fixture) error {
	finish := func(error) {}
	if r.Telemetry != nil {
		info := telemetry.ContextFrom(ctx)
		if info.TraceID == "" {
			info.TraceID = "trace:" + f.ID
		}
		if info.RunID == "" {
			info.RunID = f.ID
		}
		info.Recorder = r.Telemetry
		ctx = telemetry.WithContext(ctx, info)
		finish = r.Telemetry.StartContext(ctx, "workflow.node", map[string]string{"node": node})
	}
	_ = emitEvent(r.Events, ctx, events.EventPhaseStarted, map[string]any{"node": node})
	var err error
	switch node {
	case "normalize":
		state.Metadata = map[string]string{"fixture": f.ID}
	case "collect_evidence_parallel":
		for _, ev := range f.Evidence {
			state.Evidence = append(state.Evidence, evidenceItem(ev))
		}
	case "diagnose_agent":
		state.Diagnosis = Diagnosis{RootCause: "fixture root cause", Confidence: f.DiagnosisConfidence}
	case "diagnosis_gate":
		state.Diagnosis.GatePassed = len(state.Evidence) > 0 && state.Diagnosis.Confidence >= 0.7
		if !state.Diagnosis.GatePassed {
			state.Terminal = TerminalFailed
			state.FinalReport = "terminal_failure: insufficient diagnosis evidence"
		}
	case "plan_agent":
		state.Plan = Plan{ID: "plan-" + f.ID, Revision: 1, Risk: firstNonEmpty(f.PlanRisk, "low"), Steps: []Step{{ID: "step-1", Description: "verify status", Command: "kubectl", Args: []string{"get", "pods"}, Mutation: false, IdempotencyKey: "read-" + f.ID}}}
		if state.Plan.Risk == "write" || state.Plan.Risk == "high" {
			state.Plan.RequiresApproval = true
			state.Plan.Steps = append(state.Plan.Steps, Step{ID: "step-2", Description: "safe mutation", Command: "kubectl", Args: []string{"rollout", "restart"}, Mutation: true, IdempotencyKey: "mutate-" + f.ID})
		}
		state.Plan.SnapshotHash = planHash(state.Plan)
	case "validate_plan":
		state.Plan.Validated = true
	case "approval":
		if state.Plan.RequiresApproval && f.Approval != "approved" {
			state.Terminal = TerminalWaiting
			state.FinalReport = "waiting_for_approval"
			break
		}
		if state.Plan.RequiresApproval {
			snap, snapErr := policy.BindApproval(policy.Request{ToolID: "execute_step", ToolVersion: "v1", Args: map[string]any{"plan": state.Plan.ID, "revision": state.Plan.Revision, "hash": state.Plan.SnapshotHash}})
			if snapErr != nil {
				err = snapErr
				break
			}
			state.Approvals = append(state.Approvals, snap)
		}
	case "execute":
		for _, step := range state.Plan.Steps {
			if existing := state.ExecutionReceipts[step.IdempotencyKey]; existing.Status == "success" {
				continue
			}
			state.ExecutionReceipts[step.IdempotencyKey] = Receipt{Key: step.IdempotencyKey, Status: "success", CreatedAt: time.Now().UTC()}
		}
	case "verify":
		state.Verification = Verification{Success: f.Verify != "fail", Status: firstNonEmpty(f.Verify, "success")}
		if !state.Verification.Success {
			if state.ReplanCount >= r.MaxReplans {
				state.Terminal = TerminalFailed
				state.FinalReport = "terminal_failure: replan limit reached"
			} else {
				state.ReplanCount++
				state.Verification.Success = true
				state.Verification.Status = "success_after_replan"
			}
		}
	case "final_report":
		if state.Terminal == "" {
			state.Terminal = TerminalComplete
			state.FinalReport = "complete"
		}
	}
	_ = emitEvent(r.Events, ctx, events.EventPhaseCompleted, map[string]any{"node": node, "terminal": state.Terminal})
	finish(err)
	return err
}

func (r Runtime) catalog() *catalog.Catalog {
	if r.Catalog == nil {
		return catalog.Default()
	}
	return r.Catalog
}

func evidenceItem(summary string) evidence.Evidence {
	return evidence.Evidence{Source: "fixture", Timestamp: time.Now().UTC(), Scope: evidence.Scope{Namespace: "infra"}, Freshness: "current", Summary: summary, ArtifactRef: evidence.ArtifactRef{ID: "fixture:inline", Kind: "fixture"}}
}

func planHash(plan Plan) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s:%d", plan.ID, plan.Revision, plan.Risk, len(plan.Steps))))
	return hex.EncodeToString(h[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func emitEvent(emitter *events.Emitter, ctx context.Context, typ events.EventType, payload map[string]any) error {
	if emitter == nil {
		return nil
	}
	_, err := emitter.Emit(ctx, typ, payload)
	return err
}
