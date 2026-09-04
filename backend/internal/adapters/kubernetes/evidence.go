package kubernetes

import (
	"context"
	"fmt"
	"time"

	"go_agent/internal/evidence"
)

type EvidenceSource struct {
	Monitor *Monitor
	Now     func() time.Time
}

func (s EvidenceSource) Collect(ctx context.Context, query evidence.Query) evidence.Evidence {
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now()
	}
	if s.Monitor == nil || !s.Monitor.Available() {
		return evidence.PermissionEvidence("kubernetes", query.Scope, "k8s client unavailable or permission denied", now)
	}
	resource := query.Scope.Resource
	if resource == "" {
		resource = query.Scope.Service
	}
	out, err := s.Monitor.MonitorResource(ctx, query.Scope.Namespace, "pods", resource)
	if err != nil {
		return evidence.Evidence{Source: "kubernetes", Timestamp: now, Scope: query.Scope, Freshness: "current", Summary: "kubernetes query degraded", ArtifactRef: evidence.ArtifactRef{ID: "kubernetes:error", Kind: "error"}, Degraded: true, Reason: err.Error()}
	}
	return evidence.Evidence{Source: "kubernetes", Timestamp: now, Scope: query.Scope, Freshness: "current", Summary: fmt.Sprintf("kubernetes evidence collected: %T", out), ArtifactRef: evidence.ArtifactRef{ID: "kubernetes:inline", Kind: "kubernetes_status"}}
}
