package improvement

import (
	"context"
	"strings"
	"testing"
)

func TestReviewCaseStoreClusterAndPriority(t *testing.T) {
	store := NewMemoryCaseStore()
	_ = store.SaveCase(context.Background(), Case{ID: "1", NormalizedQuestion: "redis timeout", Status: ReviewNew})
	_ = store.SaveCase(context.Background(), Case{ID: "2", NormalizedQuestion: "Redis Timeout", Status: ReviewNew})
	cases, err := store.ListCases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	clusters := ClusterCases(cases)
	if len(clusters) != 1 || len(clusters[0].Cases) != 2 {
		t.Fatalf("unexpected clusters: %+v", clusters)
	}
	if Priority(PriorityInput{Severity: "p0", Frequency: 1}) <= Priority(PriorityInput{Severity: "p3", Frequency: 100}) {
		t.Fatal("rare P0 must outrank frequent ordinary FAQ")
	}
}

func TestTriageRequiresCategoryBeforePublishPipeline(t *testing.T) {
	_, err := Triage(Case{ID: "c1"}, TriageDecision{ResolutionPath: KnowledgeCandidatePath})
	if err == nil || !strings.Contains(err.Error(), "failure category") {
		t.Fatalf("expected missing category error, got %v", err)
	}
	_, err = Triage(Case{ID: "c1"}, TriageDecision{FailureCategory: ToolFailure, ResolutionPath: KnowledgeCandidatePath})
	if err == nil || !strings.Contains(err.Error(), "missing/stale knowledge") {
		t.Fatalf("expected knowledge gate error, got %v", err)
	}
	triaged, err := Triage(Case{ID: "c1"}, TriageDecision{FailureCategory: MissingKnowledge, ResolutionPath: KnowledgeCandidatePath})
	if err != nil || triaged.Status != ReviewTriaged {
		t.Fatalf("triage failed: %+v err=%v", triaged, err)
	}
}

func TestKnowledgePublishPipelineAndRegressionGate(t *testing.T) {
	candidate, err := NewKnowledgeCandidate(Case{ID: "c1", RunID: "run", TraceID: "trace", RetrievalSnapshotID: "snap", FailureCategory: MissingKnowledge}, KnowledgeRunbook, "password=abc fix")
	if err != nil {
		t.Fatalf("NewKnowledgeCandidate: %v", err)
	}
	if strings.Contains(candidate.Content, "abc") {
		t.Fatalf("candidate content not redacted: %q", candidate.Content)
	}
	for _, status := range []KnowledgeStatus{KReviewed, KValidated, KIndexedStaging, KEvaluated, KCanary} {
		candidate, err = Advance(candidate, status)
		if err != nil {
			t.Fatalf("Advance %s: %v", status, err)
		}
	}
	if _, err := Advance(candidate, KPublished); err == nil {
		t.Fatal("publish should require governance fields")
	}
	candidate.Owner = "sre"
	candidate.Source = "review"
	candidate.Scope = "project"
	candidate.Validity = "2026"
	candidate, err = Advance(candidate, KPublished)
	if err != nil || candidate.Status != KPublished {
		t.Fatalf("publish failed: %+v err=%v", candidate, err)
	}
	if err := GateMetricRegression(-2.1); err == nil {
		t.Fatal("expected metric regression gate failure")
	}
}

func TestDataFlywheelGovernanceEndToEndDrill(t *testing.T) {
	store := NewMemoryCaseStore()
	created := Case{
		ID:                  "case-redis-timeout",
		RunID:               "run-123",
		TraceID:             "trace-123",
		SessionID:           "session-123",
		NormalizedQuestion:  "redis timeout",
		RetrievalSnapshotID: "snapshot-123",
		Status:              ReviewNew,
	}
	if err := store.SaveCase(context.Background(), created); err != nil {
		t.Fatalf("SaveCase: %v", err)
	}

	cases, err := store.ListCases(context.Background())
	if err != nil {
		t.Fatalf("ListCases: %v", err)
	}
	if len(cases) != 1 || cases[0].RunID == "" || cases[0].TraceID == "" || cases[0].RetrievalSnapshotID == "" {
		t.Fatalf("case is not traceable: %+v", cases)
	}

	if _, err := NewKnowledgeCandidate(cases[0], KnowledgeRunbook, "should not publish"); err == nil {
		t.Fatal("untriaged case should not enter knowledge publish pipeline")
	}
	toolFailure, err := Triage(cases[0], TriageDecision{FailureCategory: ToolFailure, ResolutionPath: ToolDefect})
	if err != nil {
		t.Fatalf("tool failure triage: %v", err)
	}
	if _, err := NewKnowledgeCandidate(toolFailure, KnowledgeRunbook, "should not publish"); err == nil {
		t.Fatal("tool defect should not become a knowledge candidate")
	}

	triaged, err := Triage(cases[0], TriageDecision{FailureCategory: StaleKnowledge, ResolutionPath: KnowledgeCandidatePath})
	if err != nil {
		t.Fatalf("knowledge triage: %v", err)
	}
	candidate, err := NewKnowledgeCandidate(triaged, KnowledgeTroubleshooting, "token=abc rotate Redis timeout runbook")
	if err != nil {
		t.Fatalf("NewKnowledgeCandidate: %v", err)
	}
	if candidate.RunID != created.RunID || candidate.TraceID != created.TraceID || candidate.RetrievalSnapshotID != created.RetrievalSnapshotID {
		t.Fatalf("candidate lost provenance: %+v", candidate)
	}
	if strings.Contains(candidate.Content, "abc") {
		t.Fatalf("candidate leaked secret material: %q", candidate.Content)
	}

	for _, status := range []KnowledgeStatus{KReviewed, KValidated, KIndexedStaging, KEvaluated, KCanary} {
		candidate, err = Advance(candidate, status)
		if err != nil {
			t.Fatalf("Advance %s: %v", status, err)
		}
	}
	canary := CanaryScope{Tenant: "tenant-a", Project: "oncall", TrafficPercent: 5, RollbackVersion: candidate.RollbackVersion}
	if !canary.CanRollback() {
		t.Fatalf("canary must preserve rollback version: %+v", canary)
	}
	candidate.Owner = "sre"
	candidate.Source = "review-case"
	candidate.Scope = "project:oncall"
	candidate.Validity = "2026-Q4"
	candidate, err = Advance(candidate, KPublished)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if candidate.Status != KPublished || candidate.RollbackVersion == "" {
		t.Fatalf("published candidate missing rollback evidence: %+v", candidate)
	}
}
