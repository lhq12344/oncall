package evidence

import "time"

type ArtifactRef struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	URI  string `json:"uri,omitempty"`
}

type Evidence struct {
	Source      string      `json:"source"`
	Timestamp   time.Time   `json:"timestamp"`
	Scope       Scope       `json:"scope"`
	Freshness   string      `json:"freshness"`
	Summary     string      `json:"summary"`
	ArtifactRef ArtifactRef `json:"artifact_ref"`
	Degraded    bool        `json:"degraded,omitempty"`
	Reason      string      `json:"reason,omitempty"`
}

type Scope struct {
	Cluster   string `json:"cluster,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Service   string `json:"service,omitempty"`
	Resource  string `json:"resource,omitempty"`
}

func PermissionEvidence(source string, scope Scope, reason string, now time.Time) Evidence {
	return Evidence{Source: source, Timestamp: now, Scope: scope, Freshness: "current", Summary: "permission unavailable", ArtifactRef: ArtifactRef{ID: "permission:" + source, Kind: "permission"}, Degraded: true, Reason: reason}
}
