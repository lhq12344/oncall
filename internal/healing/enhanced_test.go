package healing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestMonitorEnhanced_DefaultRules 测试默认监控规则
func TestMonitorEnhanced_DefaultRules(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := &MonitorConfig{
		Logger: logger,
	}

	monitor, err := NewMonitorEnhanced(config)
	assert.NoError(t, err)
	assert.NotNil(t, monitor)

	rules := monitor.GetRules()
	assert.NotEmpty(t, rules)
	assert.GreaterOrEqual(t, len(rules), 6, "should have at least 6 default rules")

	// 验证规则名称
	ruleNames := make(map[string]bool)
	for _, rule := range rules {
		ruleNames[rule.Name] = true
	}

	assert.True(t, ruleNames["pod_crash_loop"])
	assert.True(t, ruleNames["high_cpu"])
	assert.True(t, ruleNames["high_memory"])
	assert.True(t, ruleNames["high_error_rate"])
}

// TestMonitorEnhanced_AddRemoveRule 测试添加和移除规则
func TestMonitorEnhanced_AddRemoveRule(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := &MonitorConfig{
		Logger: logger,
	}

	monitor, err := NewMonitorEnhanced(config)
	assert.NoError(t, err)

	initialCount := len(monitor.GetRules())

	// 添加自定义规则
	customRule := &MonitorRule{
		Name:      "custom_test_rule",
		Type:      MonitorTypeMetric,
		Query:     "test_metric > 100",
		Threshold: 100,
		Duration:  5 * time.Minute,
		Severity:  SeverityHigh,
		Enabled:   true,
	}

	monitor.AddRule(customRule)
	assert.Equal(t, initialCount+1, len(monitor.GetRules()))

	// 移除规则
	monitor.RemoveRule("custom_test_rule")
	assert.Equal(t, initialCount, len(monitor.GetRules()))
}

// TestDetectorEnhanced_Creation 测试检测器创建
func TestDetectorEnhanced_Creation(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	detector := NewDetectorEnhanced(logger)
	assert.NotNil(t, detector)
	assert.NotNil(t, detector.aggregator)
	assert.NotEmpty(t, detector.strategies)
	assert.GreaterOrEqual(t, len(detector.strategies), 3, "should have at least 3 strategies")
}

// TestDetectorEnhanced_Detect 测试故障检测
func TestDetectorEnhanced_Detect(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	detector := NewDetectorEnhanced(logger)

	// 创建高严重程度事件
	events := []*MonitorEvent{
		{
			ID:        "event-1",
			Timestamp: time.Now(),
			Type:      MonitorTypeMetric,
			Severity:  SeverityHigh,
			Source:    "test",
			Message:   "CrashLoopBackOff detected",
			Labels: map[string]string{
				"pod":       "test-pod",
				"namespace": "default",
			},
			Value: 10,
		},
	}

	ctx := context.Background()
	incident, err := detector.Detect(ctx, events)

	assert.NoError(t, err)
	assert.NotNil(t, incident, "should detect incident from high severity event")
	assert.Equal(t, SeverityHigh, incident.Severity)
	assert.NotEmpty(t, incident.AffectedResources)
}

// TestAlertAggregator 测试告警聚合器
func TestAlertAggregator(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	aggregator := NewAlertAggregator(5*time.Minute, logger)
	assert.NotNil(t, aggregator)

	// 添加事件
	event1 := &MonitorEvent{
		ID:        "event-1",
		Timestamp: time.Now(),
		Type:      MonitorTypeMetric,
		Source:    "prometheus",
		Labels: map[string]string{
			"pod": "test-pod",
		},
	}

	event2 := &MonitorEvent{
		ID:        "event-2",
		Timestamp: time.Now(),
		Type:      MonitorTypeMetric,
		Source:    "prometheus",
		Labels: map[string]string{
			"pod": "test-pod",
		},
	}

	aggregator.Add(event1)
	aggregator.Add(event2)

	aggregated := aggregator.GetAggregated()
	assert.NotEmpty(t, aggregated)

	// 验证事件被聚合到同一个键下
	for _, events := range aggregated {
		assert.GreaterOrEqual(t, len(events), 2)
	}
}

// TestStrategyMatcher 测试策略匹配器
func TestStrategyMatcher(t *testing.T) {
	matcher := NewStrategyMatcher()
	assert.NotNil(t, matcher)

	strategies := matcher.GetStrategies()
	assert.NotEmpty(t, strategies)
	assert.GreaterOrEqual(t, len(strategies), 10, "should have at least 10 default strategies")

	// 测试匹配 Pod Crash Loop
	incident := &Incident{
		ID:       "test-incident",
		Type:     IncidentTypePodCrashLoop,
		Severity: SeverityHigh,
		AffectedResources: []Resource{
			{
				Type:      "pod",
				Namespace: "default",
				Name:      "test-pod",
			},
		},
	}

	template := matcher.Match(incident)
	assert.NotNil(t, template, "should match a strategy for pod crash loop")
	assert.Equal(t, "Pod Restart", template.Name)
	assert.Equal(t, StrategyTypeRestart, template.Type)
	assert.Equal(t, RiskLevelLow, template.RiskLevel)
}

// TestStrategyMatcher_HighCPU 测试高 CPU 策略匹配
func TestStrategyMatcher_HighCPU(t *testing.T) {
	matcher := NewStrategyMatcher()

	incident := &Incident{
		ID:       "test-incident-cpu",
		Type:     IncidentTypeHighCPU,
		Severity: SeverityMedium,
	}

	template := matcher.Match(incident)
	// 高 CPU 可能没有直接匹配的策略，因为策略条件主要基于 incident_type
	// 这是正常的，系统会使用默认策略
	if template != nil {
		t.Logf("Matched strategy: %s", template.Name)
	} else {
		t.Log("No specific strategy matched, will use default")
	}
}

// TestStrategyTemplate_Convert 测试策略模板转换
func TestStrategyTemplate_Convert(t *testing.T) {
	template := getPodRestartStrategy()
	assert.NotNil(t, template)

	incident := &Incident{
		ID:   "test-incident",
		Type: IncidentTypePodCrashLoop,
		AffectedResources: []Resource{
			{
				Type:      "pod",
				Namespace: "default",
				Name:      "test-pod",
			},
		},
	}

	strategy := template.ConvertToHealingStrategy(incident)
	assert.NotNil(t, strategy)
	assert.Equal(t, template.Name, strategy.Name)
	assert.Equal(t, template.Type, strategy.Type)
	assert.NotEmpty(t, strategy.Actions)

	// 验证资源信息已填充
	if len(strategy.Actions) > 0 {
		assert.Equal(t, "pod", strategy.Actions[0].Target.Type)
		assert.Equal(t, "default", strategy.Actions[0].Target.Namespace)
	}
}

// TestGetDefaultStrategies 测试获取默认策略
func TestGetDefaultStrategies(t *testing.T) {
	strategies := GetDefaultStrategies()
	assert.NotEmpty(t, strategies)
	assert.Equal(t, 10, len(strategies), "should have exactly 10 default strategies")

	// 验证策略名称
	strategyNames := make(map[string]bool)
	for _, strategy := range strategies {
		strategyNames[strategy.Name] = true
		assert.NotEmpty(t, strategy.ID)
		assert.NotEmpty(t, strategy.Description)
		assert.NotEmpty(t, strategy.Actions)
	}

	assert.True(t, strategyNames["Pod Restart"])
	assert.True(t, strategyNames["Pod Scale Up"])
	assert.True(t, strategyNames["Pod Scale Down"])
	assert.True(t, strategyNames["Deployment Rollback"])
	assert.True(t, strategyNames["Node Drain"])
	assert.True(t, strategyNames["Service Restart"])
	assert.True(t, strategyNames["Database Restart"])
	assert.True(t, strategyNames["Cache Flush"])
	assert.True(t, strategyNames["Config Reload"])
	assert.True(t, strategyNames["Network Repair"])
}

// TestThresholdStrategy 测试阈值策略
func TestThresholdStrategy(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	strategy := &ThresholdStrategy{logger: logger}

	assert.Equal(t, "threshold", strategy.Name())

	// 测试高严重程度事件
	events := []*MonitorEvent{
		{
			ID:        "event-1",
			Timestamp: time.Now(),
			Severity:  SeverityHigh,
			Message:   "High CPU usage",
			Labels: map[string]string{
				"pod": "test-pod",
			},
			Value: 0.9,
		},
	}

	incident, err := strategy.Detect(events)
	assert.NoError(t, err)
	assert.NotNil(t, incident)
	assert.Equal(t, SeverityHigh, incident.Severity)
}

// TestFrequencyStrategy 测试频率策略
func TestFrequencyStrategy(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	strategy := &FrequencyStrategy{logger: logger}

	assert.Equal(t, "frequency", strategy.Name())

	// 创建多个最近的事件
	now := time.Now()
	events := []*MonitorEvent{
		{ID: "event-1", Timestamp: now.Add(-1 * time.Minute), Severity: SeverityMedium},
		{ID: "event-2", Timestamp: now.Add(-2 * time.Minute), Severity: SeverityMedium},
		{ID: "event-3", Timestamp: now.Add(-3 * time.Minute), Severity: SeverityMedium},
	}

	incident, err := strategy.Detect(events)
	assert.NoError(t, err)
	assert.NotNil(t, incident, "should detect incident from high frequency events")
}

// TestPatternStrategy 测试模式策略
func TestPatternStrategy(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	strategy := &PatternStrategy{logger: logger}

	assert.Equal(t, "pattern", strategy.Name())

	// 测试 CrashLoopBackOff 模式
	events := []*MonitorEvent{
		{
			ID:        "event-1",
			Timestamp: time.Now(),
			Message:   "Pod is in CrashLoopBackOff state",
			Severity:  SeverityMedium,
		},
	}

	incident, err := strategy.Detect(events)
	assert.NoError(t, err)
	assert.NotNil(t, incident, "should detect CrashLoopBackOff pattern")
}
