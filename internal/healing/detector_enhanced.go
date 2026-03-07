package healing

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// DetectionStrategy 检测策略接口
type DetectionStrategy interface {
	Detect(events []*MonitorEvent) (*Incident, error)
	Name() string
}

// DetectorEnhanced 增强版检测器
type DetectorEnhanced struct {
	strategies []DetectionStrategy
	aggregator *AlertAggregator
	logger     *zap.Logger
}

// AlertAggregator 告警聚合器
type AlertAggregator struct {
	window     time.Duration
	events     map[string][]*MonitorEvent
	mutex      sync.RWMutex
	logger     *zap.Logger
}

// NewAlertAggregator 创建告警聚合器
func NewAlertAggregator(window time.Duration, logger *zap.Logger) *AlertAggregator {
	return &AlertAggregator{
		window: window,
		events: make(map[string][]*MonitorEvent),
		logger: logger,
	}
}

// Add 添加事件
func (a *AlertAggregator) Add(event *MonitorEvent) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	key := a.getKey(event)
	a.events[key] = append(a.events[key], event)

	// 清理过期事件
	a.cleanup()
}

// GetAggregated 获取聚合后的事件
func (a *AlertAggregator) GetAggregated() map[string][]*MonitorEvent {
	a.mutex.RLock()
	defer a.mutex.RUnlock()

	result := make(map[string][]*MonitorEvent)
	for k, v := range a.events {
		result[k] = v
	}

	return result
}

// getKey 生成聚合键
func (a *AlertAggregator) getKey(event *MonitorEvent) string {
	// 根据事件类型和标签生成键
	key := fmt.Sprintf("%s:%s", event.Type, event.Source)
	if pod, ok := event.Labels["pod"]; ok {
		key += ":" + pod
	}
	if namespace, ok := event.Labels["namespace"]; ok {
		key += ":" + namespace
	}
	return key
}

// cleanup 清理过期事件
func (a *AlertAggregator) cleanup() {
	now := time.Now()
	for key, events := range a.events {
		filtered := make([]*MonitorEvent, 0)
		for _, event := range events {
			if now.Sub(event.Timestamp) <= a.window {
				filtered = append(filtered, event)
			}
		}

		if len(filtered) == 0 {
			delete(a.events, key)
		} else {
			a.events[key] = filtered
		}
	}
}

// NewDetectorEnhanced 创建增强版检测器
func NewDetectorEnhanced(logger *zap.Logger) *DetectorEnhanced {
	detector := &DetectorEnhanced{
		strategies: make([]DetectionStrategy, 0),
		aggregator: NewAlertAggregator(5*time.Minute, logger),
		logger:     logger,
	}

	// 添加默认策略
	detector.AddStrategy(&ThresholdStrategy{logger: logger})
	detector.AddStrategy(&FrequencyStrategy{logger: logger})
	detector.AddStrategy(&PatternStrategy{logger: logger})

	return detector
}

// AddStrategy 添加检测策略
func (d *DetectorEnhanced) AddStrategy(strategy DetectionStrategy) {
	d.strategies = append(d.strategies, strategy)
	d.logger.Info("detection strategy added", zap.String("name", strategy.Name()))
}

// Detect 检测故障（增强版）
func (d *DetectorEnhanced) Detect(ctx context.Context, events []*MonitorEvent) (*Incident, error) {
	if len(events) == 0 {
		return nil, nil
	}

	// 将事件添加到聚合器
	for _, event := range events {
		d.aggregator.Add(event)
	}

	// 获取聚合后的事件
	aggregated := d.aggregator.GetAggregated()

	// 使用各个策略进行检测
	for _, strategy := range d.strategies {
		for _, eventGroup := range aggregated {
			incident, err := strategy.Detect(eventGroup)
			if err != nil {
				d.logger.Warn("detection strategy failed",
					zap.String("strategy", strategy.Name()),
					zap.Error(err))
				continue
			}

			if incident != nil {
				d.logger.Info("incident detected",
					zap.String("strategy", strategy.Name()),
					zap.String("incident_id", incident.ID),
					zap.String("type", string(incident.Type)))
				return incident, nil
			}
		}
	}

	return nil, nil
}

// ThresholdStrategy 阈值检测策略
type ThresholdStrategy struct {
	logger *zap.Logger
}

func (s *ThresholdStrategy) Name() string {
	return "threshold"
}

func (s *ThresholdStrategy) Detect(events []*MonitorEvent) (*Incident, error) {
	if len(events) == 0 {
		return nil, nil
	}

	// 检查是否有高严重程度的事件
	for _, event := range events {
		if event.Severity == SeverityCritical || event.Severity == SeverityHigh {
			return s.createIncident(events), nil
		}
	}

	return nil, nil
}

func (s *ThresholdStrategy) createIncident(events []*MonitorEvent) *Incident {
	firstEvent := events[0]

	incident := &Incident{
		ID:          uuid.New().String(),
		Timestamp:   time.Now(),
		Severity:    firstEvent.Severity,
		Type:        s.inferIncidentType(firstEvent),
		Title:       s.generateTitle(firstEvent),
		Description: s.generateDescription(events),
		Events:      events,
		AffectedResources: s.extractResources(events),
	}

	return incident
}

func (s *ThresholdStrategy) inferIncidentType(event *MonitorEvent) IncidentType {
	// 根据事件标签和消息推断故障类型
	if pod, ok := event.Labels["pod"]; ok && pod != "" {
		if event.Message != "" && contains(event.Message, "CrashLoopBackOff") {
			return IncidentTypePodCrashLoop
		}
	}

	if event.Value > 0.8 {
		if contains(event.Message, "cpu") || contains(event.Message, "CPU") {
			return IncidentTypeHighCPU
		}
		if contains(event.Message, "memory") || contains(event.Message, "Memory") {
			return IncidentTypeHighMemory
		}
	}

	if contains(event.Message, "error") || contains(event.Message, "Error") {
		return IncidentTypeHighErrorRate
	}

	return IncidentTypeServiceDown
}

func (s *ThresholdStrategy) generateTitle(event *MonitorEvent) string {
	if pod, ok := event.Labels["pod"]; ok {
		return fmt.Sprintf("Issue detected in pod %s", pod)
	}
	return "System issue detected"
}

func (s *ThresholdStrategy) generateDescription(events []*MonitorEvent) string {
	if len(events) == 0 {
		return "No details available"
	}

	firstEvent := events[0]
	desc := fmt.Sprintf("Detected %d events. ", len(events))
	desc += fmt.Sprintf("First event: %s (severity: %s, value: %.2f)",
		firstEvent.Message, firstEvent.Severity, firstEvent.Value)

	return desc
}

func (s *ThresholdStrategy) extractResources(events []*MonitorEvent) []Resource {
	resources := make([]Resource, 0)
	seen := make(map[string]bool)

	for _, event := range events {
		if pod, ok := event.Labels["pod"]; ok {
			namespace := event.Labels["namespace"]
			key := fmt.Sprintf("%s:%s", namespace, pod)

			if !seen[key] {
				resources = append(resources, Resource{
					Type:      "pod",
					Namespace: namespace,
					Name:      pod,
					Labels:    event.Labels,
				})
				seen[key] = true
			}
		}
	}

	return resources
}

// FrequencyStrategy 频率检测策略
type FrequencyStrategy struct {
	threshold int
	logger    *zap.Logger
}

func (s *FrequencyStrategy) Name() string {
	return "frequency"
}

func (s *FrequencyStrategy) Detect(events []*MonitorEvent) (*Incident, error) {
	if len(events) < 3 {
		return nil, nil
	}

	// 检查事件频率
	now := time.Now()
	recentCount := 0
	for _, event := range events {
		if now.Sub(event.Timestamp) <= 5*time.Minute {
			recentCount++
		}
	}

	if recentCount >= 3 {
		s.logger.Info("high frequency events detected", zap.Int("count", recentCount))
		return (&ThresholdStrategy{logger: s.logger}).createIncident(events), nil
	}

	return nil, nil
}

// PatternStrategy 模式匹配策略
type PatternStrategy struct {
	logger *zap.Logger
}

func (s *PatternStrategy) Name() string {
	return "pattern"
}

func (s *PatternStrategy) Detect(events []*MonitorEvent) (*Incident, error) {
	if len(events) == 0 {
		return nil, nil
	}

	// 检查是否有特定模式
	patterns := []string{
		"CrashLoopBackOff",
		"OOMKilled",
		"ImagePullBackOff",
		"FailedScheduling",
	}

	for _, event := range events {
		for _, pattern := range patterns {
			if contains(event.Message, pattern) {
				s.logger.Info("pattern matched", zap.String("pattern", pattern))
				return (&ThresholdStrategy{logger: s.logger}).createIncident(events), nil
			}
		}
	}

	return nil, nil
}

// contains 检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
