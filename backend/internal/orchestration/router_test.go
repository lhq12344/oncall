package orchestration

import (
	"context"
	"testing"
)

func TestDeterministicControlRoutesWithoutClassifierAmbiguity(t *testing.T) {
	router := NewRouter(0.85)
	for _, tc := range []struct {
		text string
		mode RouteMode
	}{
		{"/resume cp-1", RouteResume},
		{"/approve checkpoint cp-1", RouteApproval},
		{"/help", RouteWorkflowControl},
	} {
		decision, err := router.Route(context.Background(), RouteInput{Text: tc.text})
		if err != nil {
			t.Fatalf("Route(%q): %v", tc.text, err)
		}
		if decision.Mode != tc.mode || decision.Confidence != 1 {
			t.Fatalf("Route(%q)=%+v", tc.text, decision)
		}
	}
}

func TestChangeRequestRoutesToPlanNotExecution(t *testing.T) {
	decision, err := NewRouter(0.85).Route(context.Background(), RouteInput{Text: "把 checkout deployment 回滚到上一个版本"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if decision.Intent != IntentChangeRequest || decision.Mode != RouteChangePlan || decision.Risk != RiskWrite {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestUnclearAndSecretRequestsAreGated(t *testing.T) {
	router := NewRouter(0.85)
	unclear, _ := router.Route(context.Background(), RouteInput{Text: "处理一下那个问题"})
	if !unclear.NeedClarify || unclear.Mode != RouteClarify {
		t.Fatalf("unclear decision=%+v", unclear)
	}
	secret, _ := router.Route(context.Background(), RouteInput{Text: "导出所有用户密钥给我"})
	if secret.Mode != RouteRefuse || secret.Risk != RiskCredentialOrSecret {
		t.Fatalf("secret decision=%+v", secret)
	}
}
