package quality

import (
	"context"
	"strings"

	"go_agent/internal/rag/retrieval"
)

type EvidenceGate struct {
	MinEvidence int
}

func (g EvidenceGate) Evaluate(_ context.Context, bundle retrieval.EvidenceBundle) EvaluationResult {
	if g.MinEvidence <= 0 {
		g.MinEvidence = 1
	}
	if len(bundle.Evidence) < g.MinEvidence {
		return EvaluationResult{Status: Fail, Reasons: []string{"insufficient_evidence"}}
	}
	for _, item := range bundle.Evidence {
		if strings.TrimSpace(item.Citation) == "" {
			return EvaluationResult{Status: Repairable, Reasons: []string{"citation_not_ready"}}
		}
	}
	return ok()
}
