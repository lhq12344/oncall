package delegation

import "time"

type Task struct {
	ID             string
	ParentRunID    string
	ParentSpanID   string
	InputSchema    string
	OutputSchema   string
	Timeout        time.Duration
	ModelProfileID string
	ToolAllowlist  []string
	TokenBudget    int
	ToolBudget     int
	CancellationID string
	ArtifactRefs   []string
}
