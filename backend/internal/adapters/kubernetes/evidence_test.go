package kubernetes

import (
	"context"
	"testing"

	"go_agent/internal/evidence"
)

func TestEvidenceSourceReturnsPermissionEvidenceWhenUnavailable(t *testing.T) {
	item := (EvidenceSource{}).Collect(context.Background(), evidence.Query{Scope: evidence.Scope{Namespace: "infra"}})
	if !item.Degraded || item.Source != "kubernetes" || item.ArtifactRef.Kind != "permission" {
		t.Fatalf("expected degraded permission evidence: %+v", item)
	}
}
