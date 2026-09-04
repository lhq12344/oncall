package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	k8sadapter "go_agent/internal/adapters/kubernetes"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type clusterMonitor interface {
	Available() bool
	MonitorResource(context.Context, string, string, string) (interface{}, error)
}

type K8sMonitorTool struct {
	monitor clusterMonitor
	logger  *zap.Logger
}

func NewK8sMonitorTool(kubeconfig string, logger *zap.Logger) (tool.BaseTool, error) {
	return NewK8sMonitorToolWithMonitor(k8sadapter.NewMonitor(kubeconfig, logger), logger), nil
}

func NewK8sMonitorToolWithMonitor(monitor clusterMonitor, logger *zap.Logger) tool.BaseTool {
	return &K8sMonitorTool{monitor: monitor, logger: logger}
}

func (t *K8sMonitorTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "k8s_monitor",
		Desc: "Monitor Kubernetes resource state for pods, nodes, deployments, statefulsets, and services.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"namespace": {
				Type:     schema.String,
				Desc:     "Namespace. Defaults to infra.",
				Required: false,
			},
			"resource_type": {
				Type:     schema.String,
				Desc:     "Resource type: pod/node/deployment/statefulset/service.",
				Required: true,
			},
			"resource_name": {
				Type:     schema.String,
				Desc:     "Optional resource name. Empty means list all resources of that type.",
				Required: false,
			},
		}),
	}, nil
}

func (t *K8sMonitorTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Namespace    string `json:"namespace"`
		ResourceType string `json:"resource_type"`
		ResourceName string `json:"resource_name"`
	}

	var in args
	if err := unmarshalOpsArgsLenient(argumentsInJSON, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if in.Namespace == "" {
		in.Namespace = "infra"
	}
	in.Namespace = strings.TrimSpace(in.Namespace)
	in.ResourceType = strings.ToLower(strings.TrimSpace(in.ResourceType))
	if in.ResourceType == "" {
		in.ResourceType = "pod"
	}
	in.ResourceName = strings.TrimSpace(in.ResourceName)

	callCount := increaseToolCallCount(ctx, "k8s_monitor")
	cacheKey := strings.ToLower(in.Namespace) + "|" + in.ResourceType + "|" + strings.ToLower(in.ResourceName)
	if cached, ok := getCachedToolResult(ctx, "k8s_monitor", cacheKey); ok {
		if t.logger != nil {
			t.logger.Info("k8s monitor cache hit",
				zap.String("agent", currentAgentForLog(ctx, "ops_incident_agent")),
				zap.String("resource_type", in.ResourceType),
				zap.String("namespace", in.Namespace),
				zap.String("resource_name", in.ResourceName),
				zap.Int("call_count", callCount))
		}
		return cached, nil
	}

	if t.monitor == nil || !t.monitor.Available() {
		output := t.fallbackResponse(in.ResourceType, in.Namespace, in.ResourceName)
		setCachedToolResult(ctx, "k8s_monitor", cacheKey, output)
		return output, nil
	}

	result, err := t.monitor.MonitorResource(ctx, in.Namespace, in.ResourceType, in.ResourceName)
	if err != nil {
		if t.logger != nil {
			t.logger.Error("k8s monitor failed",
				zap.String("agent", currentAgentForLog(ctx, "ops_incident_agent")),
				zap.String("resource_type", in.ResourceType),
				zap.String("namespace", in.Namespace),
				zap.Error(err))
		}
		return "", fmt.Errorf("failed to monitor %s: %w", in.ResourceType, err)
	}

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("k8s monitor completed",
			zap.String("agent", currentAgentForLog(ctx, "ops_incident_agent")),
			zap.String("resource_type", in.ResourceType),
			zap.String("namespace", in.Namespace),
			zap.Int("call_count", callCount))
	}

	setCachedToolResult(ctx, "k8s_monitor", cacheKey, string(output))
	return string(output), nil
}

func (t *K8sMonitorTool) fallbackResponse(resourceType, namespace, name string) string {
	result := map[string]interface{}{
		"error":         "k8s_client_unavailable",
		"message":       fmt.Sprintf("Kubernetes client not available. Cannot query %s in namespace: %s", resourceType, namespace),
		"suggestion":    "Please check kubeconfig or in-cluster configuration",
		"resource_type": resourceType,
		"namespace":     namespace,
		"resource_name": name,
	}
	output, _ := json.Marshal(result)
	return string(output)
}
