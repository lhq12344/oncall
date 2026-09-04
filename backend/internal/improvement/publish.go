package improvement

import "fmt"

var nextKnowledgeStatus = map[KnowledgeStatus]KnowledgeStatus{
	KDraft:          KReviewed,
	KReviewed:       KValidated,
	KValidated:      KIndexedStaging,
	KIndexedStaging: KEvaluated,
	KEvaluated:      KCanary,
	KCanary:         KPublished,
}

func Advance(candidate KnowledgeCandidate, target KnowledgeStatus) (KnowledgeCandidate, error) {
	if target != nextKnowledgeStatus[candidate.Status] {
		return candidate, fmt.Errorf("invalid knowledge status transition %s -> %s", candidate.Status, target)
	}
	if target == KPublished && (candidate.Owner == "" || candidate.Source == "" || candidate.Scope == "" || candidate.Version == "" || candidate.Validity == "" || candidate.RollbackVersion == "") {
		return candidate, fmt.Errorf("published knowledge requires owner, source, scope, version, validity, and rollback version")
	}
	candidate.Status = target
	return candidate, nil
}

func GateMetricRegression(deltaPct float64) error {
	if deltaPct < -2.0 {
		return fmt.Errorf("rag metric regression %.2f exceeds 2 percentage points", deltaPct)
	}
	return nil
}
