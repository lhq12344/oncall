package ops

import (
	"context"
	"fmt"
	"sync"
	"time"

	appcontext "go_agent/internal/context"

	"go.uber.org/zap"
)

// OpsAgent 运维代理
type OpsAgent struct {
	contextManager    *appcontext.ContextManager
	k8sMonitor        *K8sMonitor           // K8s 监控器
	metricsCollector  *MetricsCollector     // 指标采集器
	logAnalyzer       *LogAnalyzer          // 日志分析器
	anomalyDetector   *AnomalyDetector      // 异常检测器
	signalAggregator  *SignalAggregator     // 信号聚合器
	healthChecker     *HealthChecker        // 健康检查器
	logger            *zap.Logger
	mu                sync.RWMutex
}

// Config Ops Agent 配置
type Config struct {
	ContextManager *appcontext.ContextManager
	K8sConfig      *K8sConfig      // K8s 配置（可选）
	PrometheusURL  string          // Prometheus URL
	Logger         *zap.Logger
}

// NewOpsAgent 创建 Ops Agent
func NewOpsAgent(cfg *Config) *OpsAgent {
	return &OpsAgent{
		contextManager:   cfg.ContextManager,
		k8sMonitor:       NewK8sMonitor(cfg.K8sConfig),
		metricsCollector: NewMetricsCollector(cfg.PrometheusURL),
		logAnalyzer:      NewLogAnalyzer(),
		anomalyDetector:  NewAnomalyDetector(),
		signalAggregator: NewSignalAggregator(),
		healthChecker:    NewHealthChecker(),
		logger:           cfg.Logger,
	}
}

// MonitorK8s 监控 K8s 资源
func (o *OpsAgent) MonitorK8s(ctx context.Context, namespace, resourceType, resourceName string) (*K8sMonitorResult, error) {
	o.logger.Info("monitoring k8s resource",
		zap.String("namespace", namespace),
		zap.String("type", resourceType),
		zap.String("name", resourceName),
	)

	result := &K8sMonitorResult{
		Namespace:    namespace,
		ResourceType: resourceType,
		ResourceName: resourceName,
		Timestamp:    time.Now(),
	}

	switch resourceType {
	case "pod":
		podStatus, err := o.k8sMonitor.GetPodStatus(ctx, namespace, resourceName)
		if err != nil {
			return nil, fmt.Errorf("failed to get pod status: %w", err)
		}
		result.Status = podStatus
		result.Healthy = podStatus.Phase == "Running"

	case "deployment":
		deployStatus, err := o.k8sMonitor.GetDeploymentStatus(ctx, namespace, resourceName)
		if err != nil {
			return nil, fmt.Errorf("failed to get deployment status: %w", err)
		}
		result.Status = deployStatus
		result.Healthy = deployStatus.ReadyReplicas == deployStatus.Replicas

	case "service":
		svcStatus, err := o.k8sMonitor.GetServiceStatus(ctx, namespace, resourceName)
		if err != nil {
			return nil, fmt.Errorf("failed to get service status: %w", err)
		}
		result.Status = svcStatus
		result.Healthy = true // 服务存在即认为健康

	default:
		return nil, fmt.Errorf("unsupported resource type: %s", resourceType)
	}

	o.logger.Info("k8s monitoring completed",
		zap.Bool("healthy", result.Healthy),
	)

	return result, nil
}

// CollectMetrics 采集指标
func (o *OpsAgent) CollectMetrics(ctx context.Context, query string, timeRange string) (*MetricsResult, error) {
	o.logger.Info("collecting metrics",
		zap.String("query", query),
		zap.String("time_range", timeRange),
	)

	// 执行 Prometheus 查询
	data, err := o.metricsCollector.Query(ctx, query, timeRange)
	if err != nil {
		return nil, fmt.Errorf("failed to query metrics: %w", err)
	}

	result := &MetricsResult{
		Query:     query,
		TimeRange: timeRange,
		Data:      data,
		Timestamp: time.Now(),
	}

	// 检测异常
	anomalies := o.anomalyDetector.Detect(ctx, data)
	result.Anomalies = anomalies

	o.logger.Info("metrics collected",
		zap.Int("data_points", len(data)),
		zap.Int("anomalies", len(anomalies)),
	)

	return result, nil
}

// AnalyzeLogs 分析日志
func (o *OpsAgent) AnalyzeLogs(ctx context.Context, source, query string, timeRange string) (*LogAnalysisResult, error) {
	o.logger.Info("analyzing logs",
		zap.String("source", source),
		zap.String("query", query),
		zap.String("time_range", timeRange),
	)

	// 查询日志
	logs, err := o.logAnalyzer.Query(ctx, source, query, timeRange)
	if err != nil {
		return nil, fmt.Errorf("failed to query logs: %w", err)
	}

	// 分析日志
	analysis := o.logAnalyzer.Analyze(ctx, logs)

	result := &LogAnalysisResult{
		Source:       source,
		Query:        query,
		TimeRange:    timeRange,
		TotalCount:   len(logs),
		ErrorCount:   analysis.ErrorCount,
		WarningCount: analysis.WarningCount,
		Patterns:     analysis.Patterns,
		TopErrors:    analysis.TopErrors,
		Timestamp:    time.Now(),
	}

	o.logger.Info("log analysis completed",
		zap.Int("total", result.TotalCount),
		zap.Int("errors", result.ErrorCount),
	)

	return result, nil
}

// CheckHealth 健康检查
func (o *OpsAgent) CheckHealth(ctx context.Context, target *HealthCheckTarget) (*HealthCheckResult, error) {
	o.logger.Info("checking health",
		zap.String("type", target.Type),
		zap.String("endpoint", target.Endpoint),
	)

	result, err := o.healthChecker.Check(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("health check failed: %w", err)
	}

	o.logger.Info("health check completed",
		zap.Bool("healthy", result.Healthy),
		zap.Duration("latency", result.Latency),
	)

	return result, nil
}

// AggregateSignals 聚合多维信号
func (o *OpsAgent) AggregateSignals(ctx context.Context, signals map[string]float64) (*AnomalyReport, error) {
	o.logger.Info("aggregating signals",
		zap.Int("signal_count", len(signals)),
	)

	report := o.signalAggregator.AggregateDetect(signals)

	o.logger.Info("signal aggregation completed",
		zap.Float64("overall_score", report.OverallScore),
		zap.String("severity", report.Severity),
		zap.Int("anomaly_count", len(report.Anomalies)),
	)

	return report, nil
}

// DiagnoseSystem 系统诊断（综合分析）
func (o *OpsAgent) DiagnoseSystem(ctx context.Context, target *DiagnosisTarget) (*DiagnosisReport, error) {
	o.logger.Info("diagnosing system",
		zap.String("namespace", target.Namespace),
		zap.String("service", target.Service),
	)

	report := &DiagnosisReport{
		Target:    target,
		Timestamp: time.Now(),
		Issues:    make([]*Issue, 0),
	}

	// 1. 检查 K8s 资源状态
	if target.CheckK8s {
		k8sResult, err := o.MonitorK8s(ctx, target.Namespace, "deployment", target.Service)
		if err != nil {
			o.logger.Warn("k8s monitoring failed", zap.Error(err))
		} else {
			report.K8sStatus = k8sResult
			if !k8sResult.Healthy {
				report.Issues = append(report.Issues, &Issue{
					Type:     "k8s",
					Severity: "high",
					Message:  fmt.Sprintf("K8s resource unhealthy: %v", k8sResult.Status),
				})
			}
		}
	}

	// 2. 采集指标
	if target.CheckMetrics {
		// CPU 使用率
		cpuQuery := fmt.Sprintf(`rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s.*"}[5m])`, target.Namespace, target.Service)
		cpuResult, err := o.CollectMetrics(ctx, cpuQuery, "5m")
		if err != nil {
			o.logger.Warn("cpu metrics collection failed", zap.Error(err))
		} else {
			report.Metrics = append(report.Metrics, cpuResult)
			if len(cpuResult.Anomalies) > 0 {
				report.Issues = append(report.Issues, &Issue{
					Type:     "metrics",
					Severity: "medium",
					Message:  fmt.Sprintf("CPU anomalies detected: %d", len(cpuResult.Anomalies)),
				})
			}
		}

		// 内存使用率
		memQuery := fmt.Sprintf(`container_memory_usage_bytes{namespace="%s",pod=~"%s.*"}`, target.Namespace, target.Service)
		memResult, err := o.CollectMetrics(ctx, memQuery, "5m")
		if err != nil {
			o.logger.Warn("memory metrics collection failed", zap.Error(err))
		} else {
			report.Metrics = append(report.Metrics, memResult)
			if len(memResult.Anomalies) > 0 {
				report.Issues = append(report.Issues, &Issue{
					Type:     "metrics",
					Severity: "medium",
					Message:  fmt.Sprintf("Memory anomalies detected: %d", len(memResult.Anomalies)),
				})
			}
		}
	}

	// 3. 分析日志
	if target.CheckLogs {
		logQuery := fmt.Sprintf(`{namespace="%s",app="%s"}`, target.Namespace, target.Service)
		logResult, err := o.AnalyzeLogs(ctx, "loki", logQuery, "5m")
		if err != nil {
			o.logger.Warn("log analysis failed", zap.Error(err))
		} else {
			report.LogAnalysis = logResult
			if logResult.ErrorCount > 0 {
				report.Issues = append(report.Issues, &Issue{
					Type:     "logs",
					Severity: "high",
					Message:  fmt.Sprintf("Errors found in logs: %d", logResult.ErrorCount),
				})
			}
		}
	}

	// 4. 健康检查
	if target.HealthCheckURL != "" {
		healthTarget := &HealthCheckTarget{
			Type:     "http",
			Endpoint: target.HealthCheckURL,
			Timeout:  5 * time.Second,
		}
		healthResult, err := o.CheckHealth(ctx, healthTarget)
		if err != nil {
			o.logger.Warn("health check failed", zap.Error(err))
		} else {
			report.HealthCheck = healthResult
			if !healthResult.Healthy {
				report.Issues = append(report.Issues, &Issue{
					Type:     "health",
					Severity: "critical",
					Message:  fmt.Sprintf("Health check failed: %s", healthResult.Message),
				})
			}
		}
	}

	// 5. 计算整体健康度
	report.OverallHealth = o.calculateOverallHealth(report)

	o.logger.Info("system diagnosis completed",
		zap.Float64("overall_health", report.OverallHealth),
		zap.Int("issues", len(report.Issues)),
	)

	return report, nil
}

// calculateOverallHealth 计算整体健康度
func (o *OpsAgent) calculateOverallHealth(report *DiagnosisReport) float64 {
	if len(report.Issues) == 0 {
		return 1.0
	}

	// 根据问题严重程度计算健康度
	score := 1.0
	for _, issue := range report.Issues {
		switch issue.Severity {
		case "critical":
			score -= 0.3
		case "high":
			score -= 0.2
		case "medium":
			score -= 0.1
		case "low":
			score -= 0.05
		}
	}

	if score < 0 {
		score = 0
	}

	return score
}

// GetResourceUsage 获取资源使用情况
func (o *OpsAgent) GetResourceUsage(ctx context.Context, namespace, podName string) (*ResourceUsage, error) {
	o.logger.Info("getting resource usage",
		zap.String("namespace", namespace),
		zap.String("pod", podName),
	)

	usage, err := o.k8sMonitor.GetResourceUsage(ctx, namespace, podName)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource usage: %w", err)
	}

	o.logger.Info("resource usage retrieved",
		zap.Float64("cpu", usage.CPUUsage),
		zap.Float64("memory", usage.MemoryUsage),
	)

	return usage, nil
}
