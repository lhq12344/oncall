package toolregistry

import "time"

type ToolID string
type ToolSource string
type Capability string
type RiskLevel string
type ResourceScope string
type OutputPolicy string

const (
	ToolSourceLocal ToolSource = "local"
	ToolSourceMCP   ToolSource = "mcp"
	ToolSourceEino  ToolSource = "eino"

	RiskLow         RiskLevel = "low"
	RiskMedium      RiskLevel = "medium"
	RiskHigh        RiskLevel = "high"
	RiskDestructive RiskLevel = "destructive"

	OutputInlineRedacted OutputPolicy = "inline_redacted"
	OutputArtifactSpill  OutputPolicy = "artifact_spill"
)

type ToolDescriptor struct {
	ID          ToolID
	Version     string
	Source      ToolSource
	Capability  Capability
	Exposure    ToolExposure
	Risk        RiskLevel
	Scope       ResourceScope
	Timeout     time.Duration
	Concurrency int
	Idempotent  bool
	Output      OutputPolicy
}

func DefaultDescriptor(id ToolID) ToolDescriptor {
	return ToolDescriptor{ID: id, Version: "v1", Source: ToolSourceLocal, Exposure: ToolExposureDeferredGateway, Risk: RiskMedium, Timeout: 30 * time.Second, Concurrency: 1, Output: OutputInlineRedacted}
}
