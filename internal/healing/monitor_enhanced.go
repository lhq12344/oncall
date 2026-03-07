package healing

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// MonitorConfig 监控配置
type MonitorConfig struct {
	PrometheusURL string
	KubeConfig    string
	ESAddresses   []string
	Logger        *zap.Logger
}

// MonitorRule 监控规则
type MonitorRule struct {
	Name        string
	Type        MonitorType
	Query       string
	Threshold   float64
	Duration    time.Duration
	Severity    Severity
	Enabled     bool
}

// Monitor 监控组件（增强版）
type MonitorEnhanced struct {
	promClient *v1.API
	k8sClient  *kubernetes.Clientset
	rules      []*MonitorRule
	logger     *zap.Logger
}

// NewMonitorEnhanced 创建增强版监控组件
func NewMonitorEnhanced(config *MonitorConfig) (*MonitorEnhanced, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	monitor := &MonitorEnhanced{
		logger: config.Logger,
		rules:  getDefaultMonitorRules(),
	}

	// 初始化 Prometheus 客户端
	if config.PrometheusURL != "" {
		promConfig := api.Config{
			Address: config.PrometheusURL,
		}
		client, err := api.NewClient(promConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create prometheus client: %w", err)
		}
		promAPI := v1.NewAPI(client)
		monitor.promClient = &promAPI
		config.Logger.Info("prometheus client initialized", zap.String("url", config.PrometheusURL))
	}

	// 初始化 K8s 客户端
	if config.KubeConfig != "" {
		k8sConfig, err := clientcmd.BuildConfigFromFlags("", config.KubeConfig)
		if err != nil {
			// 尝试使用 in-cluster 配置
			k8sConfig, err = rest.InClusterConfig()
			if err != nil {
				config.Logger.Warn("failed to create k8s client", zap.Error(err))
			}
		}

		if k8sConfig != nil {
			clientset, err := kubernetes.NewForConfig(k8sConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to create k8s clientset: %w", err)
			}
			monitor.k8sClient = clientset
			config.Logger.Info("k8s client initialized")
		}
	}

	return monitor, nil
}

// getDefaultMonitorRules 获取默认监控规则
func getDefaultMonitorRules() []*MonitorRule {
	return []*MonitorRule{
		{
			Name:      "pod_crash_loop",
			Type:      MonitorTypeMetric,
			Query:     `kube_pod_container_status_restarts_total > 5`,
			Threshold: 5,
			Duration:  5 * time.Minute,
			Severity:  SeverityHigh,
			Enabled:   true,
		},
		{
			Name:      "high_cpu",
			Type:      MonitorTypeMetric,
			Query:     `sum(rate(container_cpu_usage_seconds_total{container!="",pod!=""}[5m])) by (pod) > 0.8`,
			Threshold: 0.8,
			Duration:  10 * time.Minute,
			Severity:  SeverityMedium,
			Enabled:   true,
		},
		{
			Name:      "high_memory",
			Type:      MonitorTypeMetric,
			Query:     `sum(container_memory_working_set_bytes{container!="",pod!=""}) by (pod) / sum(container_spec_memory_limit_bytes{container!="",pod!=""}) by (pod) > 0.9`,
			Threshold: 0.9,
			Duration:  10 * time.Minute,
			Severity:  SeverityMedium,
			Enabled:   true,
		},
		{
			Name:      "high_error_rate",
			Type:      MonitorTypeMetric,
			Query:     `sum(rate(http_requests_total{status=~"5.."}[5m])) by (service) / sum(rate(http_requests_total[5m])) by (service) > 0.05`,
			Threshold: 0.05,
			Duration:  5 * time.Minute,
			Severity:  SeverityHigh,
			Enabled:   true,
		},
		{
			Name:      "pod_not_ready",
			Type:      MonitorTypeMetric,
			Query:     `kube_pod_status_ready{condition="false"} == 1`,
			Threshold: 1,
			Duration:  5 * time.Minute,
			Severity:  SeverityHigh,
			Enabled:   true,
		},
		{
			Name:      "disk_full",
			Type:      MonitorTypeMetric,
			Query:     `(node_filesystem_avail_bytes / node_filesystem_size_bytes) < 0.1`,
			Threshold: 0.1,
			Duration:  5 * time.Minute,
			Severity:  SeverityCritical,
			Enabled:   true,
		},
	}
}

// Collect 收集监控事件（增强版）
func (m *MonitorEnhanced) Collect(ctx context.Context) ([]*MonitorEvent, error) {
	events := make([]*MonitorEvent, 0)

	// 1. 从 Prometheus 收集指标
	if m.promClient != nil {
		promEvents, err := m.collectPrometheusMetrics(ctx)
		if err != nil {
			m.logger.Warn("failed to collect prometheus metrics", zap.Error(err))
		} else {
			events = append(events, promEvents...)
		}
	}

	// 2. 从 K8s 收集事件
	if m.k8sClient != nil {
		k8sEvents, err := m.collectK8sEvents(ctx)
		if err != nil {
			m.logger.Warn("failed to collect k8s events", zap.Error(err))
		} else {
			events = append(events, k8sEvents...)
		}
	}

	m.logger.Debug("collected monitor events", zap.Int("count", len(events)))

	return events, nil
}

// collectPrometheusMetrics 从 Prometheus 收集指标
func (m *MonitorEnhanced) collectPrometheusMetrics(ctx context.Context) ([]*MonitorEvent, error) {
	events := make([]*MonitorEvent, 0)

	for _, rule := range m.rules {
		if !rule.Enabled || rule.Type != MonitorTypeMetric {
			continue
		}

		result, warnings, err := (*m.promClient).Query(ctx, rule.Query, time.Now())
		if err != nil {
			m.logger.Warn("prometheus query failed",
				zap.String("rule", rule.Name),
				zap.Error(err))
			continue
		}

		if len(warnings) > 0 {
			m.logger.Warn("prometheus query warnings",
				zap.String("rule", rule.Name),
				zap.Strings("warnings", warnings))
		}

		// 解析结果
		if result.Type() == model.ValVector {
			vector := result.(model.Vector)
			for _, sample := range vector {
				value := float64(sample.Value)
				if value >= rule.Threshold {
					event := &MonitorEvent{
						ID:        fmt.Sprintf("%s-%d", rule.Name, time.Now().Unix()),
						Timestamp: time.Now(),
						Type:      MonitorTypeMetric,
						Severity:  rule.Severity,
						Source:    "prometheus",
						Message:   fmt.Sprintf("Rule '%s' triggered: value=%.2f, threshold=%.2f", rule.Name, value, rule.Threshold),
						Labels:    make(map[string]string),
						Value:     value,
					}

					// 提取标签
					for k, v := range sample.Metric {
						event.Labels[string(k)] = string(v)
					}

					events = append(events, event)

					m.logger.Info("prometheus alert triggered",
						zap.String("rule", rule.Name),
						zap.Float64("value", value),
						zap.Float64("threshold", rule.Threshold))
				}
			}
		}
	}

	return events, nil
}

// collectK8sEvents 从 K8s 收集事件
func (m *MonitorEnhanced) collectK8sEvents(ctx context.Context) ([]*MonitorEvent, error) {
	events := make([]*MonitorEvent, 0)

	// 获取最近 5 分钟的 K8s 事件
	eventList, err := m.k8sClient.CoreV1().Events("").List(ctx, metav1.ListOptions{
		FieldSelector: "type=Warning",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list k8s events: %w", err)
	}

	now := time.Now()
	for _, k8sEvent := range eventList.Items {
		// 只处理最近 5 分钟的事件
		if now.Sub(k8sEvent.LastTimestamp.Time) > 5*time.Minute {
			continue
		}

		event := &MonitorEvent{
			ID:        string(k8sEvent.UID),
			Timestamp: k8sEvent.LastTimestamp.Time,
			Type:      MonitorTypeEvent,
			Severity:  SeverityMedium,
			Source:    "kubernetes",
			Message:   k8sEvent.Message,
			Labels: map[string]string{
				"namespace": k8sEvent.Namespace,
				"kind":      k8sEvent.InvolvedObject.Kind,
				"name":      k8sEvent.InvolvedObject.Name,
				"reason":    k8sEvent.Reason,
			},
		}

		// 根据事件类型调整严重程度
		switch k8sEvent.Reason {
		case "BackOff", "CrashLoopBackOff":
			event.Severity = SeverityHigh
		case "Failed", "FailedScheduling":
			event.Severity = SeverityHigh
		case "Unhealthy":
			event.Severity = SeverityMedium
		}

		events = append(events, event)
	}

	return events, nil
}

// AddRule 添加监控规则
func (m *MonitorEnhanced) AddRule(rule *MonitorRule) {
	m.rules = append(m.rules, rule)
	m.logger.Info("monitor rule added", zap.String("name", rule.Name))
}

// RemoveRule 移除监控规则
func (m *MonitorEnhanced) RemoveRule(name string) {
	for i, rule := range m.rules {
		if rule.Name == name {
			m.rules = append(m.rules[:i], m.rules[i+1:]...)
			m.logger.Info("monitor rule removed", zap.String("name", name))
			return
		}
	}
}

// GetRules 获取所有规则
func (m *MonitorEnhanced) GetRules() []*MonitorRule {
	return m.rules
}
