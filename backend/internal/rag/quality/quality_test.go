package quality

import (
	"context"
	"testing"

	"go_agent/internal/rag"
	"go_agent/internal/rag/retrieval"
)

func TestEvidenceGateBlocksWeakEvidence(t *testing.T) {
	result := (EvidenceGate{MinEvidence: 1}).Evaluate(context.Background(), retrieval.EvidenceBundle{})
	if result.Status != Fail {
		t.Fatalf("expected fail, got %+v", result)
	}
	repair := (EvidenceGate{}).Evaluate(context.Background(), retrieval.EvidenceBundle{Evidence: []rag.Evidence{{ID: "e1", Source: "doc"}}})
	if repair.Status != Repairable {
		t.Fatalf("expected citation repairable, got %+v", repair)
	}
}

func TestAnswerGateBlocksUnsupportedAndSecretAnswers(t *testing.T) {
	evidence := retrieval.EvidenceBundle{Evidence: []rag.Evidence{{ID: "e1", Source: "doc", Citation: "doc#e1"}}}
	if got := (AnswerGate{}).Evaluate(context.Background(), AnswerCandidate{Content: "claim"}, evidence); got.Status != Fail {
		t.Fatalf("unsupported answer should fail: %+v", got)
	}
	if got := (AnswerGate{}).Evaluate(context.Background(), AnswerCandidate{Content: "password=abc", Citations: []string{"doc#e1"}}, evidence); got.Status != Fail {
		t.Fatalf("secret answer should fail: %+v", got)
	}
	if got := (AnswerGate{}).Evaluate(context.Background(), AnswerCandidate{Content: "claim", Citations: []string{"doc#e1"}}, evidence); got.Status != Pass {
		t.Fatalf("grounded answer should pass: %+v", got)
	}
}
