package ops

import (
	"container/ring"
	"context"
	"math"
	"sync"
)

// AnomalyDetector 异常检测器
type AnomalyDetector struct {
	detectors map[string]*DynamicThresholdDetector
	mu        sync.RWMutex
}

// NewAnomalyDetector 创建异常检测器
func NewAnomalyDetector() *AnomalyDetector {
	return &AnomalyDetector{
		detectors: make(map[string]*DynamicThresholdDetector),
	}
}

// Detect 检测异常
func (a *AnomalyDetector) Detect(ctx context.Context, data []MetricPoint) []AnomalyPoint {
	anomalies := make([]AnomalyPoint, 0)

	if len(data) == 0 {
		return anomalies
	}

	// 为每个指标创建检测器
	for _, point := range data {
		// 使用标签作为检测器 key
		key := "default"
		if instance, ok := point.Labels["instance"]; ok {
			key = instance
		}

		a.mu.Lock()
		detector, ok := a.detectors[key]
		if !ok {
			detector = NewDynamicThresholdDetector(100, 2.5)
			a.detectors[key] = detector
		}
		a.mu.Unlock()

		// 检测异常
		isAnomaly, score := detector.Detect(point.Value)
		if isAnomaly {
			anomalies = append(anomalies, AnomalyPoint{
				Timestamp: point.Timestamp,
				Value:     point.Value,
				Score:     score,
				Reason:    "Value deviates from baseline",
			})
		}
	}

	return anomalies
}

// DynamicThresholdDetector 动态阈值检测器
type DynamicThresholdDetector struct {
	windowSize int         // 滑动窗口大小
	k          float64     // 敏感度系数（通常取 2-3）
	buffer     *ring.Ring  // 环形缓冲区
	mu         sync.RWMutex
}

// NewDynamicThresholdDetector 创建动态阈值检测器
func NewDynamicThresholdDetector(windowSize int, k float64) *DynamicThresholdDetector {
	return &DynamicThresholdDetector{
		windowSize: windowSize,
		k:          k,
		buffer:     ring.New(windowSize),
	}
}

// Detect 检测异常
func (d *DynamicThresholdDetector) Detect(value float64) (bool, float64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// 添加新数据点
	d.buffer.Value = value
	d.buffer = d.buffer.Next()

	// 计算均值和标准差
	mean, stddev := d.calculateStats()

	// 判断是否异常
	deviation := math.Abs(value - mean)
	threshold := d.k * stddev

	isAnomaly := deviation > threshold
	score := 0.0
	if threshold > 0 {
		score = deviation / threshold // 异常评分
	}

	return isAnomaly, score
}

// calculateStats 计算统计量
func (d *DynamicThresholdDetector) calculateStats() (mean, stddev float64) {
	var sum, sumSq float64
	count := 0

	d.buffer.Do(func(v interface{}) {
		if v != nil {
			val := v.(float64)
			sum += val
			sumSq += val * val
			count++
		}
	})

	if count == 0 {
		return 0, 0
	}

	mean = sum / float64(count)
	variance := (sumSq / float64(count)) - (mean * mean)
	if variance < 0 {
		variance = 0
	}
	stddev = math.Sqrt(variance)

	return mean, stddev
}

// GetStats 获取统计信息
func (d *DynamicThresholdDetector) GetStats() (mean, stddev float64, count int) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	mean, stddev = d.calculateStats()

	d.buffer.Do(func(v interface{}) {
		if v != nil {
			count++
		}
	})

	return mean, stddev, count
}

// Reset 重置检测器
func (d *DynamicThresholdDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.buffer = ring.New(d.windowSize)
}

// SignalAggregator 信号聚合器
type SignalAggregator struct {
	detectors map[string]*DynamicThresholdDetector
	mu        sync.RWMutex
}

// NewSignalAggregator 创建信号聚合器
func NewSignalAggregator() *SignalAggregator {
	return &SignalAggregator{
		detectors: map[string]*DynamicThresholdDetector{
			"cpu":        NewDynamicThresholdDetector(100, 2.5),
			"memory":     NewDynamicThresholdDetector(100, 2.5),
			"latency":    NewDynamicThresholdDetector(100, 3.0),
			"error_rate": NewDynamicThresholdDetector(100, 3.0),
			"qps":        NewDynamicThresholdDetector(100, 2.0),
		},
	}
}

// AggregateDetect 聚合检测
func (s *SignalAggregator) AggregateDetect(signals map[string]float64) *AnomalyReport {
	s.mu.Lock()
	defer s.mu.Unlock()

	report := &AnomalyReport{
		Anomalies: make(map[string]float64),
	}

	totalScore := 0.0
	anomalyCount := 0

	for metric, value := range signals {
		detector, ok := s.detectors[metric]
		if !ok {
			// 为未知指标创建检测器
			detector = NewDynamicThresholdDetector(100, 2.5)
			s.detectors[metric] = detector
		}

		isAnomaly, score := detector.Detect(value)
		if isAnomaly {
			report.Anomalies[metric] = score
			totalScore += score
			anomalyCount++
		}
	}

	// 计算综合异常评分
	if anomalyCount > 0 {
		report.OverallScore = totalScore / float64(anomalyCount)
		report.Severity = s.calculateSeverity(report.OverallScore, anomalyCount)
	} else {
		report.OverallScore = 0
		report.Severity = "normal"
	}

	return report
}

// calculateSeverity 计算严重程度
func (s *SignalAggregator) calculateSeverity(score float64, count int) string {
	// 综合考虑异常评分和异常指标数量
	if score > 2.0 && count >= 3 {
		return "critical"
	} else if score > 1.5 || count >= 2 {
		return "warning"
	} else if score > 1.0 || count >= 1 {
		return "info"
	}
	return "normal"
}

// GetDetectorStats 获取检测器统计信息
func (s *SignalAggregator) GetDetectorStats(metric string) (mean, stddev float64, count int, exists bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	detector, ok := s.detectors[metric]
	if !ok {
		return 0, 0, 0, false
	}

	mean, stddev, count = detector.GetStats()
	return mean, stddev, count, true
}

// ResetDetector 重置检测器
func (s *SignalAggregator) ResetDetector(metric string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if detector, ok := s.detectors[metric]; ok {
		detector.Reset()
	}
}

// AddDetector 添加检测器
func (s *SignalAggregator) AddDetector(metric string, windowSize int, k float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.detectors[metric] = NewDynamicThresholdDetector(windowSize, k)
}

// RemoveDetector 移除检测器
func (s *SignalAggregator) RemoveDetector(metric string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.detectors, metric)
}

// ListDetectors 列出所有检测器
func (s *SignalAggregator) ListDetectors() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	metrics := make([]string, 0, len(s.detectors))
	for metric := range s.detectors {
		metrics = append(metrics, metric)
	}

	return metrics
}
