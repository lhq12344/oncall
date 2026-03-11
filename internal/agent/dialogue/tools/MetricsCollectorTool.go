package tools

import (
	opstools "go_agent/internal/agent/ops/tools"

	"github.com/cloudwego/eino/components/tool"
	"go.uber.org/zap"
)

// NewDialogueMetricsCollectorTool 为 Dialogue Agent 提供 Prometheus 指标采集工具。
func NewDialogueMetricsCollectorTool(prometheusURL string, logger *zap.Logger) (tool.BaseTool, error) {
	return opstools.NewMetricsCollectorTool(prometheusURL, logger)
}
