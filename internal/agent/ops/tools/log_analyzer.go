package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// LogAnalyzerTool 日志分析工具
type LogAnalyzerTool struct {
	logger *zap.Logger
}

// NewLogAnalyzerTool 创建日志分析工具
func NewLogAnalyzerTool(logger *zap.Logger) tool.BaseTool {
	return &LogAnalyzerTool{
		logger: logger,
	}
}

func (t *LogAnalyzerTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "log_analyzer",
		Desc: "分析日志中的异常模式。识别错误、警告、异常堆栈等信息，并提供统计分析。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"source": {
				Type:     schema.String,
				Desc:     "日志来源（pod名称、服务名称等）",
				Required: true,
			},
			"time_range": {
				Type:     schema.String,
				Desc:     "时间范围（如 5m, 1h, 24h），默认 1h",
				Required: false,
			},
			"level": {
				Type:     schema.String,
				Desc:     "日志级别过滤（error/warn/info），默认 error",
				Required: false,
			},
		}),
	}, nil
}

func (t *LogAnalyzerTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Source    string `json:"source"`
		TimeRange string `json:"time_range"`
		Level     string `json:"level"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	in.Source = strings.TrimSpace(in.Source)
	if in.Source == "" {
		return "", fmt.Errorf("source is required")
	}

	// 默认值
	if in.TimeRange == "" {
		in.TimeRange = "1h"
	}
	if in.Level == "" {
		in.Level = "error"
	}

	// 解析时间范围
	duration, err := time.ParseDuration(in.TimeRange)
	if err != nil {
		return "", fmt.Errorf("invalid time_range format: %w", err)
	}

	// 执行日志分析
	result := t.analyzeLogs(in.Source, duration, in.Level)

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("log analysis completed",
			zap.String("source", in.Source),
			zap.String("level", in.Level))
	}

	return string(output), nil
}

// analyzeLogs 分析日志（当前为模拟实现，实际应集成日志系统）
func (t *LogAnalyzerTool) analyzeLogs(source string, duration time.Duration, level string) map[string]interface{} {
	// TODO: 集成实际的日志系统（Loki/ElasticSearch/CLS）
	// 当前返回模拟数据和分析框架

	// 模拟日志条目
	sampleLogs := []string{
		"2024-03-06 10:15:23 ERROR Failed to connect to database: connection timeout",
		"2024-03-06 10:16:45 ERROR NullPointerException at com.example.Service.process(Service.java:123)",
		"2024-03-06 10:18:12 WARN High memory usage detected: 85%",
		"2024-03-06 10:20:33 ERROR API request failed: 500 Internal Server Error",
		"2024-03-06 10:22:15 ERROR Failed to connect to database: connection timeout",
	}

	// 分析日志模式
	patterns := t.detectPatterns(sampleLogs)
	anomalies := t.detectAnomalies(sampleLogs)
	topErrors := t.extractTopErrors(sampleLogs)

	return map[string]interface{}{
		"source":     source,
		"time_range": duration.String(),
		"level":      level,
		"summary": map[string]interface{}{
			"total_logs":   len(sampleLogs),
			"error_count":  4,
			"warn_count":   1,
			"unique_errors": len(topErrors),
		},
		"patterns":   patterns,
		"anomalies":  anomalies,
		"top_errors": topErrors,
		"note":       "This is a placeholder implementation. Integrate with actual log system (Loki/ElasticSearch/CLS) for production use.",
	}
}

// detectPatterns 检测日志模式
func (t *LogAnalyzerTool) detectPatterns(logs []string) []map[string]interface{} {
	patterns := []map[string]interface{}{}

	// 检测常见错误模式
	errorPatterns := map[string]*regexp.Regexp{
		"connection_timeout": regexp.MustCompile(`connection timeout`),
		"null_pointer":       regexp.MustCompile(`NullPointerException`),
		"api_error":          regexp.MustCompile(`API request failed`),
		"memory_issue":       regexp.MustCompile(`memory usage`),
	}

	patternCounts := make(map[string]int)
	for _, log := range logs {
		for name, pattern := range errorPatterns {
			if pattern.MatchString(log) {
				patternCounts[name]++
			}
		}
	}

	for name, count := range patternCounts {
		if count > 0 {
			patterns = append(patterns, map[string]interface{}{
				"pattern": name,
				"count":   count,
				"severity": t.assessSeverity(name, count),
			})
		}
	}

	return patterns
}

// detectAnomalies 检测异常
func (t *LogAnalyzerTool) detectAnomalies(logs []string) []map[string]interface{} {
	anomalies := []map[string]interface{}{}

	// 检测重复错误（可能表示系统性问题）
	errorCounts := make(map[string]int)
	for _, log := range logs {
		if strings.Contains(log, "ERROR") {
			// 提取错误消息
			parts := strings.SplitN(log, "ERROR", 2)
			if len(parts) == 2 {
				errorMsg := strings.TrimSpace(parts[1])
				errorCounts[errorMsg]++
			}
		}
	}

	for msg, count := range errorCounts {
		if count >= 2 {
			anomalies = append(anomalies, map[string]interface{}{
				"type":        "repeated_error",
				"message":     msg,
				"occurrences": count,
				"severity":    "high",
				"suggestion":  "This error is repeating, indicating a systemic issue that needs investigation",
			})
		}
	}

	return anomalies
}

// extractTopErrors 提取 Top 错误
func (t *LogAnalyzerTool) extractTopErrors(logs []string) []map[string]interface{} {
	errors := []map[string]interface{}{}

	for _, log := range logs {
		if strings.Contains(log, "ERROR") {
			parts := strings.SplitN(log, "ERROR", 2)
			if len(parts) == 2 {
				timestamp := strings.TrimSpace(parts[0])
				message := strings.TrimSpace(parts[1])

				errors = append(errors, map[string]interface{}{
					"timestamp": timestamp,
					"message":   message,
					"category":  t.categorizeError(message),
				})
			}
		}
	}

	// 限制返回数量
	if len(errors) > 10 {
		errors = errors[:10]
	}

	return errors
}

// assessSeverity 评估严重程度
func (t *LogAnalyzerTool) assessSeverity(pattern string, count int) string {
	// 基于模式和频率评估严重程度
	if count >= 5 {
		return "critical"
	}
	if count >= 3 {
		return "high"
	}
	if pattern == "connection_timeout" || pattern == "null_pointer" {
		return "high"
	}
	return "medium"
}

// categorizeError 错误分类
func (t *LogAnalyzerTool) categorizeError(message string) string {
	lower := strings.ToLower(message)

	if strings.Contains(lower, "database") || strings.Contains(lower, "connection") {
		return "database"
	}
	if strings.Contains(lower, "api") || strings.Contains(lower, "http") {
		return "api"
	}
	if strings.Contains(lower, "memory") || strings.Contains(lower, "oom") {
		return "resource"
	}
	if strings.Contains(lower, "exception") || strings.Contains(lower, "error") {
		return "application"
	}

	return "unknown"
}
