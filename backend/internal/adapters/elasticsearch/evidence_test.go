package elasticsearch

import (
	"context"
	"testing"

	"go_agent/internal/evidence"
)

func TestEvidenceSourceDegradesWithoutClient(t *testing.T) {
	item := (EvidenceSource{}).Collect(context.Background(), evidence.Query{})
	if !item.Degraded || item.Source != "elasticsearch" || item.Reason == "" {
		t.Fatalf("expected degraded log evidence: %+v", item)
	}
}
