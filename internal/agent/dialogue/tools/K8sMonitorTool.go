package tools

import (
	opstools "go_agent/internal/agent/ops/tools"

	"github.com/cloudwego/eino/components/tool"
	"go.uber.org/zap"
)

// NewDialogueK8sMonitorTool 为 Dialogue Agent 提供 K8s 状态检查工具。
func NewDialogueK8sMonitorTool(kubeconfig string, logger *zap.Logger) (tool.BaseTool, error) {
	return opstools.NewK8sMonitorTool(kubeconfig, logger)
}
