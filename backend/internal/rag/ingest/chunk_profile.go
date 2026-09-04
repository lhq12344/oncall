package ingest

type ChunkProfile string

const (
	ProfileRunbook  ChunkProfile = "runbook"
	ProfileIncident ChunkProfile = "incident_report"
	ProfileK8s      ChunkProfile = "k8s_manifest"
	ProfileLog      ChunkProfile = "log_summary"
)

type ProfileConfig struct {
	Profile      ChunkProfile
	ParentTokens int
	ChildTokens  int
	OverlapRatio float64
	Strategy     string
}

func ConfigFor(profile ChunkProfile) ProfileConfig {
	switch profile {
	case ProfileIncident:
		return ProfileConfig{Profile: profile, ParentTokens: 2000, ChildTokens: 300, OverlapRatio: 0, Strategy: "proposition_by_symptom_evidence_root_cause_action_outcome"}
	case ProfileK8s:
		return ProfileConfig{Profile: profile, ParentTokens: 1500, ChildTokens: 350, OverlapRatio: 0.1, Strategy: "object_metadata_spec_container_policy"}
	case ProfileLog:
		return ProfileConfig{Profile: profile, ParentTokens: 1000, ChildTokens: 250, OverlapRatio: 0, Strategy: "aggregate_by_trace_pod_container_window_signature"}
	default:
		return ProfileConfig{Profile: ProfileRunbook, ParentTokens: 1500, ChildTokens: 350, OverlapRatio: 0.12, Strategy: "semantic_boundary_parent_child"}
	}
}
