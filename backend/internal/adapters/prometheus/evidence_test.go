package prometheus

import (
	"context"
	"testing"

	"go_agent/internal/evidence"
)

func TestEvidenceSourceReturnsDegradedWhenUnavailable(t *testing.T) {
	item := (EvidenceSource{}).Collect(context.Background(), evidence.Query{})
	if !item.Degraded || item.Source != "prometheus" {
		t.Fatalf("expected degraded prometheus evidence: %+v", item)
	}
}
