package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go_agent/internal/agent/ops/tools"

	einotool "github.com/cloudwego/eino/components/tool"
	"go.uber.org/zap"
)

// IntegratedOpsExecutor 在 Agent 层封装多工具查询执行（顺序调用 + 超时控制）。
type IntegratedOpsExecutor struct {
	timeout time.Duration

	k8sTool        einotool.InvokableTool
	prometheusTool einotool.InvokableTool
	esLogTool      einotool.InvokableTool
}

// IntegratedOpsConfig 集成执行器配置。
type IntegratedOpsConfig struct {
	KubeConfig    string
	PrometheusURL string
	Logger        *zap.Logger
	Timeout       time.Duration
}

// QueryAllSourcesInput 多数据源查询参数。
type QueryAllSourcesInput struct {
	SessionID string
	Namespace string

	PromQuery string
	TimeRange string

	ESIndex string
	ESQuery string
	ESLevel string
	ESSize  int
}

// QueryAllSourcesOutput 聚合输出。
type QueryAllSourcesOutput struct {
	Data   map[string]string
	Errors map[string]string
}

// NewIntegratedOpsExecutor 创建集成执行器。
func NewIntegratedOpsExecutor(ctx context.Context, cfg *IntegratedOpsConfig) (*IntegratedOpsExecutor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	k8sBase, err := tools.NewK8sMonitorTool(cfg.KubeConfig, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("create k8s tool failed: %w", err)
	}
	k8sTool, ok := k8sBase.(einotool.InvokableTool)
	if !ok {
		return nil, fmt.Errorf("k8s tool does not implement invokable interface")
	}

	promBase, err := tools.NewMetricsCollectorTool(cfg.PrometheusURL, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("create prometheus tool failed: %w", err)
	}
	promTool, ok := promBase.(einotool.InvokableTool)
	if !ok {
		return nil, fmt.Errorf("prometheus tool does not implement invokable interface")
	}

	esBase, err := tools.NewESLogQueryTool(cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("create es tool failed: %w", err)
	}
	esTool, ok := esBase.(einotool.InvokableTool)
	if !ok {
		return nil, fmt.Errorf("es tool does not implement invokable interface")
	}

	_ = ctx // 预留上下文扩展

	return &IntegratedOpsExecutor{
		timeout:        timeout,
		k8sTool:        k8sTool,
		prometheusTool: promTool,
		esLogTool:      esTool,
	}, nil
}

// QueryAllSources 顺序查询 K8s、Prometheus、Elasticsearch。
func (e *IntegratedOpsExecutor) QueryAllSources(ctx context.Context, in QueryAllSourcesInput) (*QueryAllSourcesOutput, error) {
	if e.timeout > 0 {
		timeoutCtx, cancel := context.WithTimeout(ctx, e.timeout)
		defer cancel()
		ctx = timeoutCtx
	}

	if in.SessionID == "" {
		in.SessionID = "default-session"
	}
	if in.Namespace == "" {
		in.Namespace = "infra"
	}
	if in.TimeRange == "" {
		in.TimeRange = "5m"
	}
	if in.PromQuery == "" {
		in.PromQuery = `up`
	}
	if in.ESIndex == "" {
		in.ESIndex = "logs-*"
	}
	if in.ESQuery == "" {
		in.ESQuery = "error"
	}
	if in.ESSize <= 0 {
		in.ESSize = 100
	}

	out := &QueryAllSourcesOutput{
		Data:   map[string]string{},
		Errors: map[string]string{},
	}

	k8sArgs, _ := json.Marshal(map[string]interface{}{
		"namespace":     in.Namespace,
		"resource_type": "pod",
	})
	k8sPayload, err := e.k8sTool.InvokableRun(ctx, string(k8sArgs))
	if err != nil {
		out.Errors["k8s"] = err.Error()
	} else {
		out.Data["k8s"] = k8sPayload
	}

	promArgs, _ := json.Marshal(map[string]interface{}{
		"query":      in.PromQuery,
		"time_range": in.TimeRange,
	})
	promPayload, err := e.prometheusTool.InvokableRun(ctx, string(promArgs))
	if err != nil {
		out.Errors["prometheus"] = err.Error()
	} else {
		out.Data["prometheus"] = promPayload
	}

	esArgs, _ := json.Marshal(map[string]interface{}{
		"index":      in.ESIndex,
		"query":      in.ESQuery,
		"time_range": in.TimeRange,
		"level":      in.ESLevel,
		"size":       in.ESSize,
	})
	esPayload, err := e.esLogTool.InvokableRun(ctx, string(esArgs))
	if err != nil {
		out.Errors["elasticsearch"] = err.Error()
	} else {
		out.Data["elasticsearch"] = esPayload
	}

	return out, nil
}
