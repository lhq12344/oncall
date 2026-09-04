package prometheus

import (
	"context"
	"fmt"
	"time"

	"go_agent/internal/evidence"
)

type EvidenceSource struct {
	Collector *Collector
	Query     string
	Now       func() time.Time
}

func (s EvidenceSource) Collect(ctx context.Context, query evidence.Query) evidence.Evidence {
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now()
	}
	if s.Collector == nil || !s.Collector.Available() {
		return evidence.PermissionEvidence("prometheus", query.Scope, "prometheus client unavailable", now)
	}
	promQL := s.Query
	if promQL == "" {
		promQL = "up"
	}
	out, err := s.Collector.Query(ctx, promQL, now, now.Add(-query.Since), now, query.Since > 0)
	if err != nil {
		return evidence.Evidence{Source: "prometheus", Timestamp: now, Scope: query.Scope, Freshness: "current", Summary: "metrics query degraded", ArtifactRef: evidence.ArtifactRef{ID: "prometheus:error", Kind: "error"}, Degraded: true, Reason: err.Error()}
	}
	return evidence.Evidence{Source: "prometheus", Timestamp: now, Scope: query.Scope, Freshness: "current", Summary: fmt.Sprintf("metrics evidence collected: %T", out), ArtifactRef: evidence.ArtifactRef{ID: "prometheus:inline", Kind: "metrics"}}
}
