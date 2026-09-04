package elasticsearch

import (
	"context"
	"time"

	"go_agent/internal/evidence"
)

type EvidenceSource struct {
	Client *Client
	Now    func() time.Time
}

func (s EvidenceSource) Collect(ctx context.Context, query evidence.Query) evidence.Evidence {
	_ = ctx
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now()
	}
	if s.Client == nil || !s.Client.Available() {
		return evidence.Evidence{Source: "elasticsearch", Timestamp: now, Scope: query.Scope, Freshness: "current", Summary: "log search degraded", ArtifactRef: evidence.ArtifactRef{ID: "elasticsearch:degraded", Kind: "logs"}, Degraded: true, Reason: "elasticsearch unavailable"}
	}
	return evidence.Evidence{Source: "elasticsearch", Timestamp: now, Scope: query.Scope, Freshness: "current", Summary: "log search available", ArtifactRef: evidence.ArtifactRef{ID: "elasticsearch:inline", Kind: "logs"}}
}
