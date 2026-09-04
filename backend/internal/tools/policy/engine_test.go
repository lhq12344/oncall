package policy

import (
	"context"
	"testing"
)

func TestPolicyRequiresMatchingApprovalForMutation(t *testing.T) {
	engine := NewEngine("")
	req := Request{ToolID: "execute_step", ToolVersion: "v1", Capability: "execution.mutation", Risk: RiskHigh, Args: map[string]any{"command": "kubectl rollout restart deploy/api"}}
	decision := engine.Decide(context.Background(), req)
	if decision.Effect != Ask || decision.ReasonCode != "mutation_requires_matching_approval" {
		t.Fatalf("expected approval ask, got %+v", decision)
	}
	snapshot, err := BindApproval(req)
	if err != nil {
		t.Fatalf("BindApproval: %v", err)
	}
	req.Approved = &snapshot
	decision = engine.Decide(context.Background(), req)
	if decision.Effect != Allow {
		t.Fatalf("expected allow with matching approval, got %+v", decision)
	}
	req.Args = map[string]any{"command": "kubectl delete pod api"}
	decision = engine.Decide(context.Background(), req)
	if decision.Effect != Ask {
		t.Fatalf("changed args should invalidate approval, got %+v", decision)
	}
}

func TestApprovalSnapshotBindsPlanTargetAndArguments(t *testing.T) {
	engine := NewEngine("")
	baseArgs := map[string]any{
		"plan_id":       "plan-123",
		"revision":      7,
		"snapshot_hash": "hash-a",
		"cluster":       "prod",
		"namespace":     "payments",
		"resource":      "deployment/api",
		"command":       "kubectl rollout restart deploy/api",
	}
	req := Request{ToolID: "execute_step", ToolVersion: "v1", Capability: "execution.mutation", Risk: RiskHigh, Args: baseArgs}
	snapshot, err := BindApproval(req)
	if err != nil {
		t.Fatalf("BindApproval: %v", err)
	}
	if snapshot.PlanID != "plan-123" || snapshot.Revision != 7 || snapshot.SnapshotHash != "hash-a" || snapshot.Cluster != "prod" || snapshot.Namespace != "payments" || snapshot.Resource != "deployment/api" {
		t.Fatalf("snapshot did not bind audit fields: %+v", snapshot)
	}
	req.Approved = &snapshot
	if decision := engine.Decide(context.Background(), req); decision.Effect != Allow {
		t.Fatalf("matching approval should allow mutation: %+v", decision)
	}

	tests := []struct {
		name string
		key  string
		val  any
	}{
		{name: "revision", key: "revision", val: 8},
		{name: "snapshot hash", key: "snapshot_hash", val: "hash-b"},
		{name: "resource target", key: "resource", val: "deployment/worker"},
		{name: "tool args", key: "command", val: "kubectl delete pod api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changedArgs := cloneArgs(baseArgs)
			changedArgs[tt.key] = tt.val
			decision := engine.Decide(context.Background(), Request{ToolID: "execute_step", ToolVersion: "v1", Capability: "execution.mutation", Risk: RiskHigh, Args: changedArgs, Approved: &snapshot})
			if decision.Effect != Ask || decision.ReasonCode != "mutation_requires_matching_approval" {
				t.Fatalf("changed %s should invalidate approval, got %+v", tt.name, decision)
			}
		})
	}
}

func TestDestructiveAlwaysAsks(t *testing.T) {
	decision := NewEngine("").Decide(context.Background(), Request{ToolID: "bash", ToolVersion: "v1", Risk: RiskDestructive, Args: map[string]any{"command": "rm -rf /tmp/x"}})
	if decision.Effect != Ask || decision.ReasonCode != "destructive_requires_approval" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func cloneArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for key, value := range args {
		out[key] = value
	}
	return out
}
