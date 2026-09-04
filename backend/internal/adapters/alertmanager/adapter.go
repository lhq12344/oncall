package alertmanager

import (
	"time"

	"go_agent/internal/evidence"
)

type Alert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
}

func ToIncidentSignal(alert Alert) evidence.IncidentSignal {
	labels := alert.Labels
	return evidence.IncidentSignal{
		Source:      "alertmanager",
		Severity:    labels["severity"],
		Cluster:     labels["cluster"],
		Namespace:   labels["namespace"],
		Service:     labels["service"],
		Resource:    labels["pod"],
		EventTime:   alert.StartsAt,
		Labels:      labels,
		RawArtifact: evidence.ArtifactRef{ID: "alertmanager:inline", Kind: "alertmanager_payload"},
	}.Normalized()
}
