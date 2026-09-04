package policy

import "strings"

type ToolRisk string

const (
	RiskLow         ToolRisk = "low"
	RiskMedium      ToolRisk = "medium"
	RiskHigh        ToolRisk = "high"
	RiskDestructive ToolRisk = "destructive"
)

type Request struct {
	ToolID           string
	ToolVersion      string
	Capability       string
	Risk             ToolRisk
	Args             map[string]any
	NormalizedTarget string
	Approved         *ApprovalSnapshot
}

func normalizeTarget(args map[string]any) string {
	for _, key := range []string{"target", "resource", "file_path", "namespace", "command", "tool_name"} {
		if value, ok := args[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
