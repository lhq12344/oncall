package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

type IncidentSignal struct {
	Source      string            `json:"source"`
	Severity    string            `json:"severity"`
	Cluster     string            `json:"cluster"`
	Namespace   string            `json:"namespace"`
	Service     string            `json:"service"`
	Resource    string            `json:"resource"`
	EventTime   time.Time         `json:"event_time"`
	Labels      map[string]string `json:"labels,omitempty"`
	Fingerprint string            `json:"fingerprint"`
	RawArtifact ArtifactRef       `json:"raw_artifact_ref"`
}

func (s IncidentSignal) Normalized() IncidentSignal {
	s.Source = strings.TrimSpace(s.Source)
	s.Severity = strings.ToLower(strings.TrimSpace(s.Severity))
	s.Cluster = strings.TrimSpace(s.Cluster)
	s.Namespace = strings.TrimSpace(s.Namespace)
	s.Service = strings.TrimSpace(s.Service)
	s.Resource = strings.TrimSpace(s.Resource)
	if s.Fingerprint == "" {
		s.Fingerprint = Fingerprint(s)
	}
	return s
}

func Fingerprint(s IncidentSignal) string {
	keys := make([]string, 0, len(s.Labels))
	for key := range s.Labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(s.Source + "\n" + s.Cluster + "\n" + s.Namespace + "\n" + s.Service + "\n" + s.Resource + "\n")
	for _, key := range keys {
		b.WriteString(key + "=" + s.Labels[key] + "\n")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
