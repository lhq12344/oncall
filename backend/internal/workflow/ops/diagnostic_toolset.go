package ops

import (
	"fmt"

	rcatools "go_agent/internal/agent/rca/tools"
	opstools "go_agent/internal/workflow/ops/tools"

	"github.com/cloudwego/eino/components/tool"
)

// IncidentDiagnosticToolset is the explicit boundary for read-only incident diagnostics.
// Ops composes this toolset on demand while the RCA package owns RCA-specific tool behavior.
type IncidentDiagnosticToolset struct {
	cfg *Config
}

func NewIncidentDiagnosticToolset(cfg *Config) (*IncidentDiagnosticToolset, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}
	return &IncidentDiagnosticToolset{cfg: cfg}, nil
}

func (s *IncidentDiagnosticToolset) BuildDeferredTools() ([]tool.BaseTool, error) {
	if s == nil || s.cfg == nil {
		return nil, fmt.Errorf("diagnostic toolset config is required")
	}
	cfg := s.cfg
	k8sTool, err := opstools.NewK8sMonitorTool(cfg.KubeConfig, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create ops k8s monitor tool: %w", err)
	}
	metricsTool, err := opstools.NewMetricsCollectorTool(cfg.PrometheusURL, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create ops metrics collector tool: %w", err)
	}
	esTool, err := opstools.NewESLogQueryTool(cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create ops es log query tool: %w", err)
	}

	return []tool.BaseTool{
		k8sTool,
		metricsTool,
		esTool,
		rcatools.NewTimeQueryTool(cfg.Logger),
		rcatools.NewBuildDependencyGraphTool(cfg.KubeConfig, cfg.Logger),
		rcatools.NewCorrelateSignalsTool(cfg.Logger),
		rcatools.NewInferRootCauseTool(cfg.ChatModel, cfg.Logger),
		rcatools.NewAnalyzeImpactTool(cfg.Logger),
	}, nil
}

func buildOpsIncidentDeferredTools(cfg *Config) ([]tool.BaseTool, error) {
	toolset, err := NewIncidentDiagnosticToolset(cfg)
	if err != nil {
		return nil, err
	}
	return toolset.BuildDeferredTools()
}
