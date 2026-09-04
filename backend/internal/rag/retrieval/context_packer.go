package retrieval

import (
	"strings"

	"go_agent/internal/rag"
)

type EvidenceBundle struct {
	SnapshotID string
	Evidence   []rag.Evidence
}

func Pack(snapshot rag.RetrievalSnapshot, maxItems int) EvidenceBundle {
	if maxItems <= 0 || maxItems > len(snapshot.FinalEvidence) {
		maxItems = len(snapshot.FinalEvidence)
	}
	evidence := append([]rag.Evidence(nil), snapshot.FinalEvidence[:maxItems]...)
	for i := range evidence {
		if evidence[i].Citation == "" {
			evidence[i].Citation = strings.TrimSpace(evidence[i].Source + "#" + evidence[i].ID)
		}
	}
	return EvidenceBundle{SnapshotID: snapshot.SnapshotID, Evidence: evidence}
}
