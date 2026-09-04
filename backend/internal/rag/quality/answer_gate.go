package quality

import (
	"context"
	"strings"

	"go_agent/internal/rag/retrieval"
)

type AnswerCandidate struct {
	Content   string
	Citations []string
}

type AnswerGate struct{}

func (AnswerGate) Evaluate(_ context.Context, answer AnswerCandidate, evidence retrieval.EvidenceBundle) EvaluationResult {
	text := strings.ToLower(answer.Content)
	if strings.Contains(text, "password=") || strings.Contains(text, "secret=") || strings.Contains(text, "token=") {
		return EvaluationResult{Status: Fail, Reasons: []string{"secret_leakage"}}
	}
	if strings.TrimSpace(answer.Content) != "" && len(answer.Citations) == 0 && len(evidence.Evidence) > 0 {
		return EvaluationResult{Status: Fail, Reasons: []string{"unsupported_claim"}}
	}
	return ok()
}
