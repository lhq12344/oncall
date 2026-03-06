package ops

import (
	"context"
	"testing"
	"time"

	appcontext "go_agent/internal/context"
)

func TestDynamicThresholdDetector(t *testing.T) {
	detector := NewDynamicThresholdDetector(10, 2.5)

	// 添加正常数据
	normalValues := []float64{50, 52, 48, 51, 49, 50, 51, 49, 50, 52}
	for _, val := range normalValues {
		isAnomaly, score := detector.Detect(val)
		if isAnomaly {
			t.Logf("Value %.2f detected as anomaly (score: %.2f)", val, score)
		}
	}

	// 添加异常数据
	anomalyValue := 100.0
	isAnomaly, score := detector.Detect(anomalyValue)
	if !isAnomaly {
		t.Errorf("expected anomaly for value %.2f", anomalyValue)
	}

	t.Logf("Anomaly detected: value=%.2f, score=%.2f", anomalyValue, score)

	// 获取统计信息
	mean, stddev, count := detector.GetStats()
	t.Logf("Stats: mean=%.2f, stddev=%.2f, count=%d", mean, stddev, count)

	t.Log("✓ DynamicThresholdDetector works correctly")
}

func TestSignalAggregator(t *testing.T) {
	aggregator := NewSignalAggregator()

	// 测试正常信号
	normalSignals := map[string]float64{
		"cpu":        50.0,
		"memory":     60.0,
		"latency":    100.0,
		"error_rate": 0.01,
	}

	report := aggregator.AggregateDetect(normalSignals)
	t.Logf("Normal signals: overall_score=%.2f, severity=%s, anomalies=%d",
		report.OverallScore, report.Severity, len(report.Anomalies))

	// 添加更多正常数据以建立基线
	for i := 0; i < 20; i++ {
		signals := map[string]float64{
			"cpu":        50.0 + float64(i%5),
			"memory":     60.0 + float64(i%3),
			"latency":    100.0 + float64(i%10),
			"error_rate": 0.01,
		}
		aggregator.AggregateDetect(signals)
	}

	// 测试异常信号
	anomalySignals := map[string]float64{
		"cpu":        95.0,  // 异常高
		"memory":     90.0,  // 异常高
		"latency":    500.0, // 异常高
		"error_rate": 0.5,   // 异常高
	}

	report = aggregator.AggregateDetect(anomalySignals)
	t.Logf("Anomaly signals: overall_score=%.2f, severity=%s, anomalies=%d",
		report.OverallScore, report.Severity, len(report.Anomalies))

	if len(report.Anomalies) == 0 {
		t.Error("expected anomalies to be detected")
	}

	for metric, score := range report.Anomalies {
		t.Logf("  Anomaly: %s (score: %.2f)", metric, score)
	}

	t.Log("✓ SignalAggregator works correctly")
}

func TestAnomalyDetector(t *testing.T) {
	detector := NewAnomalyDetector()
	ctx := context.Background()

	// 创建测试数据
	now := time.Now()
	data := []MetricPoint{
		{Timestamp: now.Add(-4 * time.Minute), Value: 50.0, Labels: map[string]string{"instance": "pod-1"}},
		{Timestamp: now.Add(-3 * time.Minute), Value: 52.0, Labels: map[string]string{"instance": "pod-1"}},
		{Timestamp: now.Add(-2 * time.Minute), Value: 48.0, Labels: map[string]string{"instance": "pod-1"}},
		{Timestamp: now.Add(-1 * time.Minute), Value: 51.0, Labels: map[string]string{"instance": "pod-1"}},
		{Timestamp: now, Value: 100.0, Labels: map[string]string{"instance": "pod-1"}}, // 异常值
	}

	anomalies := detector.Detect(ctx, data)

	t.Logf("Detected %d anomalies", len(anomalies))
	for _, anomaly := range anomalies {
		t.Logf("  Anomaly: time=%s, value=%.2f, score=%.2f",
			anomaly.Timestamp.Format("15:04:05"), anomaly.Value, anomaly.Score)
	}

	t.Log("✓ AnomalyDetector works correctly")
}

func TestK8sMonitor(t *testing.T) {
	monitor := NewK8sMonitor(nil)
	ctx := context.Background()

	// 测试获取 Pod 状态
	podStatus, err := monitor.GetPodStatus(ctx, "default", "test-pod")
	if err != nil {
		t.Fatalf("failed to get pod status: %v", err)
	}

	t.Logf("Pod status: name=%s, phase=%s, ready=%v",
		podStatus.Name, podStatus.Phase, podStatus.Ready)

	// 测试获取 Deployment 状态
	deployStatus, err := monitor.GetDeploymentStatus(ctx, "default", "test-deployment")
	if err != nil {
		t.Fatalf("failed to get deployment status: %v", err)
	}

	t.Logf("Deployment status: name=%s, replicas=%d, ready=%d",
		deployStatus.Name, deployStatus.Replicas, deployStatus.ReadyReplicas)

	// 测试获取资源使用情况
	usage, err := monitor.GetResourceUsage(ctx, "default", "test-pod")
	if err != nil {
		t.Fatalf("failed to get resource usage: %v", err)
	}

	t.Logf("Resource usage: cpu=%.2f, memory=%.0f MB",
		usage.CPUUsage, float64(usage.MemoryUsage)/(1024*1024))

	t.Log("✓ K8sMonitor works correctly")
}

func TestMetricsCollector(t *testing.T) {
	collector := NewMetricsCollector("http://localhost:9090")
	ctx := context.Background()

	// 测试查询指标
	query := "rate(container_cpu_usage_seconds_total[5m])"
	data, err := collector.Query(ctx, query, "5m")
	if err != nil {
		t.Fatalf("failed to query metrics: %v", err)
	}

	t.Logf("Collected %d data points", len(data))
	for i, point := range data {
		t.Logf("  Point %d: time=%s, value=%.2f",
			i+1, point.Timestamp.Format("15:04:05"), point.Value)
	}

	t.Log("✓ MetricsCollector works correctly")
}

func TestLogAnalyzer(t *testing.T) {
	analyzer := NewLogAnalyzer()
	ctx := context.Background()

	// 测试查询日志
	logs, err := analyzer.Query(ctx, "loki", `{app="test"}`, "5m")
	if err != nil {
		t.Fatalf("failed to query logs: %v", err)
	}

	t.Logf("Collected %d log entries", len(logs))

	// 测试分析日志
	analysis := analyzer.Analyze(ctx, logs)

	t.Logf("Log analysis: errors=%d, warnings=%d, patterns=%d",
		analysis.ErrorCount, analysis.WarningCount, len(analysis.Patterns))

	if len(analysis.TopErrors) > 0 {
		t.Log("Top errors:")
		for i, err := range analysis.TopErrors {
			t.Logf("  %d. %s (count: %d)", i+1, err.Message, err.Count)
		}
	}

	t.Log("✓ LogAnalyzer works correctly")
}

func TestHealthChecker(t *testing.T) {
	checker := NewHealthChecker()
	ctx := context.Background()

	// 测试 HTTP 健康检查
	target := &HealthCheckTarget{
		Type:     "http",
		Endpoint: "http://localhost:8080/health",
		Timeout:  5 * time.Second,
	}

	result, err := checker.Check(ctx, target)
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}

	t.Logf("Health check result: healthy=%v, latency=%v, message=%s",
		result.Healthy, result.Latency, result.Message)

	t.Log("✓ HealthChecker works correctly")
}

func TestOpsAgent_MonitorK8s(t *testing.T) {
	// 创建上下文管理器
	storage := &MockStorage{}
	contextManager := appcontext.NewContextManager(storage)

	// 创建 Ops Agent
	agent := NewOpsAgent(&Config{
		ContextManager: contextManager,
		PrometheusURL:  "http://localhost:9090",
		Logger:         nil,
	})

	ctx := context.Background()

	// 测试监控 Pod
	result, err := agent.MonitorK8s(ctx, "default", "pod", "test-pod")
	if err != nil {
		t.Fatalf("monitor k8s failed: %v", err)
	}

	t.Logf("K8s monitoring result: healthy=%v, status=%+v",
		result.Healthy, result.Status)

	t.Log("✓ OpsAgent.MonitorK8s works correctly")
}

func TestOpsAgent_CollectMetrics(t *testing.T) {
	storage := &MockStorage{}
	contextManager := appcontext.NewContextManager(storage)

	agent := NewOpsAgent(&Config{
		ContextManager: contextManager,
		PrometheusURL:  "http://localhost:9090",
		Logger:         nil,
	})

	ctx := context.Background()

	// 测试采集指标
	result, err := agent.CollectMetrics(ctx, "rate(cpu[5m])", "5m")
	if err != nil {
		t.Fatalf("collect metrics failed: %v", err)
	}

	t.Logf("Metrics result: data_points=%d, anomalies=%d",
		len(result.Data), len(result.Anomalies))

	t.Log("✓ OpsAgent.CollectMetrics works correctly")
}

func TestOpsAgent_DiagnoseSystem(t *testing.T) {
	storage := &MockStorage{}
	contextManager := appcontext.NewContextManager(storage)

	agent := NewOpsAgent(&Config{
		ContextManager: contextManager,
		PrometheusURL:  "http://localhost:9090",
		Logger:         nil,
	})

	ctx := context.Background()

	// 测试系统诊断
	target := &DiagnosisTarget{
		Namespace:    "default",
		Service:      "test-service",
		CheckK8s:     true,
		CheckMetrics: true,
		CheckLogs:    true,
	}

	report, err := agent.DiagnoseSystem(ctx, target)
	if err != nil {
		t.Fatalf("diagnose system failed: %v", err)
	}

	t.Logf("Diagnosis report: overall_health=%.2f, issues=%d",
		report.OverallHealth, len(report.Issues))

	if len(report.Issues) > 0 {
		t.Log("Issues found:")
		for i, issue := range report.Issues {
			t.Logf("  %d. [%s] %s: %s", i+1, issue.Severity, issue.Type, issue.Message)
		}
	}

	t.Log("✓ OpsAgent.DiagnoseSystem works correctly")
}

// MockStorage 用于测试的 mock storage
type MockStorage struct{}

func (m *MockStorage) SaveSession(ctx context.Context, sessionID string, data []byte, ttl time.Duration) error {
	return nil
}

func (m *MockStorage) LoadSession(ctx context.Context, sessionID string) (*appcontext.SessionContext, error) {
	return nil, fmt.Errorf("not found")
}

func (m *MockStorage) DeleteSession(ctx context.Context, sessionID string) error {
	return nil
}

func (m *MockStorage) SaveAgentContext(ctx context.Context, agentID string, data []byte, ttl time.Duration) error {
	return nil
}

func (m *MockStorage) LoadAgentContext(ctx context.Context, agentID string) (*appcontext.AgentContext, error) {
	return nil, fmt.Errorf("not found")
}

func (m *MockStorage) SaveExecutionContext(ctx context.Context, executionID string, data []byte, ttl time.Duration) error {
	return nil
}

func (m *MockStorage) LoadExecutionContext(ctx context.Context, executionID string) (*appcontext.ExecutionContext, error) {
	return nil, fmt.Errorf("not found")
}

func (m *MockStorage) ListSessions(ctx context.Context, pattern string) ([]string, error) {
	return []string{}, nil
}

func (m *MockStorage) DeleteExpiredSessions(ctx context.Context, before time.Time) error {
	return nil
}

func TestCalculateOverallHealth(t *testing.T) {
	storage := &MockStorage{}
	contextManager := appcontext.NewContextManager(storage)

	agent := NewOpsAgent(&Config{
		ContextManager: contextManager,
		Logger:         nil,
	})

	tests := []struct {
		issues         []*Issue
		expectedHealth float64
	}{
		{
			issues:         []*Issue{},
			expectedHealth: 1.0,
		},
		{
			issues: []*Issue{
				{Type: "k8s", Severity: "low", Message: "test"},
			},
			expectedHealth: 0.95,
		},
		{
			issues: []*Issue{
				{Type: "k8s", Severity: "critical", Message: "test"},
			},
			expectedHealth: 0.7,
		},
		{
			issues: []*Issue{
				{Type: "k8s", Severity: "high", Message: "test1"},
				{Type: "metrics", Severity: "medium", Message: "test2"},
			},
			expectedHealth: 0.7,
		},
	}

	for i, tt := range tests {
		report := &DiagnosisReport{
			Issues: tt.issues,
		}

		health := agent.calculateOverallHealth(report)

		if health != tt.expectedHealth {
			t.Errorf("test %d: expected health %.2f, got %.2f",
				i+1, tt.expectedHealth, health)
		}

		t.Logf("Test %d: issues=%d, health=%.2f", i+1, len(tt.issues), health)
	}

	t.Log("✓ calculateOverallHealth works correctly")
}
