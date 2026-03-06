package supervisor

import (
	"fmt"
	"strings"
)

// AgentResult Agent 执行结果
type AgentResult struct {
	Type  string      // Agent 类型
	Data  interface{} // 结果数据
	Error error       // 错误信息
}

// ResultAggregator 结果聚合器
type ResultAggregator struct{}

// NewResultAggregator 创建结果聚合器
func NewResultAggregator() *ResultAggregator {
	return &ResultAggregator{}
}

// Aggregate 聚合多个 Agent 的结果
func (a *ResultAggregator) Aggregate(results []AgentResult) (string, error) {
	var builder strings.Builder
	hasError := false

	for _, result := range results {
		if result.Error != nil {
			hasError = true
			builder.WriteString(fmt.Sprintf("[%s] 错误: %v\n", result.Type, result.Error))
			continue
		}

		if result.Data != nil {
			builder.WriteString(fmt.Sprintf("[%s] %v\n", result.Type, result.Data))
		}
	}

	if hasError {
		return builder.String(), fmt.Errorf("部分 Agent 执行失败")
	}

	return builder.String(), nil
}

// AggregateWithPriority 按优先级聚合结果
func (a *ResultAggregator) AggregateWithPriority(results []AgentResult, priority []string) (string, error) {
	// 按优先级排序结果
	orderedResults := make([]AgentResult, 0)

	for _, p := range priority {
		for _, result := range results {
			if result.Type == p {
				orderedResults = append(orderedResults, result)
				break
			}
		}
	}

	// 添加未在优先级列表中的结果
	for _, result := range results {
		found := false
		for _, p := range priority {
			if result.Type == p {
				found = true
				break
			}
		}
		if !found {
			orderedResults = append(orderedResults, result)
		}
	}

	return a.Aggregate(orderedResults)
}
