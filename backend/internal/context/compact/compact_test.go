package compact

import (
	"strings"
	"testing"
)

func TestDefaultPolicyPreservesToolPairsAndSingleRetry(t *testing.T) {
	policy := DefaultPolicy(900)
	if !policy.PreserveToolPairs || policy.MaxAutoRetries != 1 || policy.TailTokens != 300 {
		t.Fatalf("unexpected policy: %+v", policy)
	}
}

func TestRecoveryNoticeCarriesWorkflowApprovalSkillAndArtifact(t *testing.T) {
	n := RecoveryNotice(RecoveryState{WorkflowState: "plan_approval", Approval: "pending", ActiveSkills: []string{"ops"}, RecentArtifacts: []string{"a1"}, RecentTools: []string{"k8s_monitor"}})
	for _, want := range []string{"plan_approval", "pending", "skills=ops", "artifacts=a1", "tools=k8s_monitor"} {
		if !strings.Contains(n.Content, want) {
			t.Fatalf("notice missing %q: %+v", want, n)
		}
	}
}
