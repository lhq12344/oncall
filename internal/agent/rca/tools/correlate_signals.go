package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// CorrelateSignalsTool 信号关联工具
type CorrelateSignalsTool struct {
	logger *zap.Logger
}

// Signal 信号
type Signal struct {
	Source    string    `json:"source"`     // 来源：alert/log/metric
	Service   string    `json:"service"`    // 服务名称
	Type      string    `json:"type"`       // 类型：cpu_high/error_rate/latency
	Timestamp time.Time `json:"timestamp"`  // 时间戳
	Value     float64   `json:"value"`      // 数值
	Message   string    `json:"message"`    // 消息
	Severity  string    `json:"severity"`   // 严重程度：critical/warning/info
}

// CorrelationResult 关联结果
type CorrelationResult struct {
	Signal1      Signal  `json:"signal1"`
	Signal2      Signal  `json:"signal2"`
	Correlation  float64 `json:"correlation"`  // 相关系数 [-1, 1]
	TimeDiff     int64   `json:"time_diff"`    // 时间差（毫秒）
	CausalType   string  `json:"causal_type"`  // 因果类型：cause/effect/concurrent
	Confidence   float64 `json:"confidence"`   // 置信度 [0, 1]
}

// SignalCorrelation 信号关联分析结果
type SignalCorrelation struct {
	Signals       []Signal            `json:"signals"`
	Correlations  []CorrelationResult `json:"correlations"`
	Timeline      []TimelineEvent     `json:"timeline"`
	TotalSignals  int                 `json:"total_signals"`
}

// TimelineEvent 时间线事件
type TimelineEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Service   string    `json:"service"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	Severity  string    `json:"severity"`
}

func NewCorrelateSignalsTool(logger *zap.Logger) tool.BaseTool {
	return &CorrelateSignalsTool{
		logger: logger,
	}
}

func (t *CorrelateSignalsTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "correlate_signals",
		Desc: "关联不同来源的信号（告警、日志、指标）。分析信号之间的时间关系和因果关系。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"signals": {
				Type:     schema.Array,
				Desc:     "信号列表（JSON 数组）",
				Required: true,
			},
			"time_window": {
				Type:     schema.Integer,
				Desc:     "时间窗口（秒），默认 300",
				Required: false,
			},
		}),
	}, nil
}

func (t *CorrelateSignalsTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Signals    []Signal `json:"signals"`
		TimeWindow int      `json:"time_window"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if len(in.Signals) == 0 {
		return "", fmt.Errorf("signals is required")
	}

	// 默认时间窗口 5 分钟
	if in.TimeWindow <= 0 {
		in.TimeWindow = 300
	}

	// 1. 时间窗口对齐
	alignedSignals := t.alignTimeWindow(in.Signals, in.TimeWindow)

	// 2. 计算信号之间的相关性
	correlations := t.calculateCorrelations(alignedSignals, in.TimeWindow)

	// 3. 生成时间线
	timeline := t.generateTimeline(alignedSignals)

	result := &SignalCorrelation{
		Signals:      alignedSignals,
		Correlations: correlations,
		Timeline:     timeline,
		TotalSignals: len(alignedSignals),
	}

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("signal correlation completed",
			zap.Int("signals", len(alignedSignals)),
			zap.Int("correlations", len(correlations)))
	}

	return string(output), nil
}

// alignTimeWindow 时间窗口对齐
func (t *CorrelateSignalsTool) alignTimeWindow(signals []Signal, windowSec int) []Signal {
	if len(signals) == 0 {
		return signals
	}

	// 按时间排序
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].Timestamp.Before(signals[j].Timestamp)
	})

	// 找到最早的时间
	startTime := signals[0].Timestamp

	// 过滤时间窗口外的信号
	aligned := []Signal{}
	for _, sig := range signals {
		if sig.Timestamp.Sub(startTime).Seconds() <= float64(windowSec) {
			aligned = append(aligned, sig)
		}
	}

	return aligned
}

// calculateCorrelations 计算信号相关性
func (t *CorrelateSignalsTool) calculateCorrelations(signals []Signal, windowSec int) []CorrelationResult {
	correlations := []CorrelationResult{}

	// 两两计算相关性
	for i := 0; i < len(signals); i++ {
		for j := i + 1; j < len(signals); j++ {
			sig1 := signals[i]
			sig2 := signals[j]

			// 计算时间差（毫秒）
			timeDiff := sig2.Timestamp.Sub(sig1.Timestamp).Milliseconds()

			// 如果时间差太大，跳过
			if math.Abs(float64(timeDiff)) > float64(windowSec*1000) {
				continue
			}

			// 计算相关系数（简化版本）
			correlation := t.calculateCorrelationCoefficient(sig1, sig2)

			// 判断因果类型
			causalType := t.determineCausalType(sig1, sig2, timeDiff)

			// 计算置信度
			confidence := t.calculateConfidence(correlation, timeDiff, sig1, sig2)

			correlations = append(correlations, CorrelationResult{
				Signal1:     sig1,
				Signal2:     sig2,
				Correlation: correlation,
				TimeDiff:    timeDiff,
				CausalType:  causalType,
				Confidence:  confidence,
			})
		}
	}

	// 按置信度排序
	sort.Slice(correlations, func(i, j int) bool {
		return correlations[i].Confidence > correlations[j].Confidence
	})

	// 只返回 Top 10
	if len(correlations) > 10 {
		correlations = correlations[:10]
	}

	return correlations
}

// calculateCorrelationCoefficient 计算相关系数（简化版本）
func (t *CorrelateSignalsTool) calculateCorrelationCoefficient(sig1, sig2 Signal) float64 {
	// 简化实现：基于服务、类型和严重程度
	score := 0.0

	// 同一服务相关性更高
	if sig1.Service == sig2.Service {
		score += 0.5
	}

	// 相同类型相关性更高
	if sig1.Type == sig2.Type {
		score += 0.3
	}

	// 严重程度相似相关性更高
	if sig1.Severity == sig2.Severity {
		score += 0.2
	}

	// 归一化到 [-1, 1]
	return score
}

// determineCausalType 判断因果类型
func (t *CorrelateSignalsTool) determineCausalType(sig1, sig2 Signal, timeDiff int64) string {
	// 时间差小于 1 秒，认为是并发
	if math.Abs(float64(timeDiff)) < 1000 {
		return "concurrent"
	}

	// sig1 在前，可能是原因
	if timeDiff > 0 {
		// 如果 sig1 是错误/告警，sig2 是后续影响
		if (sig1.Severity == "critical" || sig1.Severity == "warning") &&
			sig2.Severity == "critical" {
			return "cause"
		}
		return "effect"
	}

	// sig2 在前
	return "effect"
}

// calculateConfidence 计算置信度
func (t *CorrelateSignalsTool) calculateConfidence(correlation float64, timeDiff int64, sig1, sig2 Signal) float64 {
	confidence := correlation

	// 时间差越小，置信度越高
	timeFactor := 1.0 - math.Min(math.Abs(float64(timeDiff))/60000.0, 1.0) // 60秒内
	confidence = confidence*0.7 + timeFactor*0.3

	// 严重程度高的信号置信度更高
	if sig1.Severity == "critical" || sig2.Severity == "critical" {
		confidence *= 1.2
	}

	// 限制在 [0, 1]
	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < 0.0 {
		confidence = 0.0
	}

	return confidence
}

// generateTimeline 生成时间线
func (t *CorrelateSignalsTool) generateTimeline(signals []Signal) []TimelineEvent {
	timeline := []TimelineEvent{}

	for _, sig := range signals {
		timeline = append(timeline, TimelineEvent{
			Timestamp: sig.Timestamp,
			Service:   sig.Service,
			Type:      sig.Type,
			Message:   sig.Message,
			Severity:  sig.Severity,
		})
	}

	// 按时间排序
	sort.Slice(timeline, func(i, j int) bool {
		return timeline[i].Timestamp.Before(timeline[j].Timestamp)
	})

	return timeline
}
