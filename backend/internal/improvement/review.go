package improvement

import "fmt"

func NewKnowledgeCandidate(item Case, typ KnowledgeType, content string) (KnowledgeCandidate, error) {
	if !IsKnowledgeCategory(item.FailureCategory) {
		return KnowledgeCandidate{}, fmt.Errorf("case failure category %s cannot become knowledge", item.FailureCategory)
	}
	return KnowledgeCandidate{ID: "kc-" + item.ID, CaseID: item.ID, RunID: item.RunID, TraceID: item.TraceID, RetrievalSnapshotID: item.RetrievalSnapshotID, Type: typ, Status: KDraft, Version: "v1", RollbackVersion: "v0", Content: Redact(content)}, nil
}
