package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// AnalyzeImpactTool 影响分析工具
type AnalyzeImpactTool struct {
	logger *zap.Logger
}

// ImpactNode 影响节点
type ImpactNode struct {
	Service      string  `json:"service"`
	ImpactLevel  string  `json:"impact_level"`  // critical/high/medium/low
	ImpactType   string  `json:"impact_type"`   // direct/indirect
	Distance     int     `json:"distance"`      // 距离根因的跳数
	AffectedUsers int    `json:"affected_users"` // 受影响用户数（估算）
	Probability  float64 `json:"probability"`   // 受影响概率
}

// ImpactAnalysis 影响分析结果
type ImpactAnalysis struct {
	RootCause      string       `json:"root_cause"`
	ImpactedNodes  []ImpactNode `json:"impacted_nodes"`
	TotalImpacted  int          `json:"total_impacted"`
	CriticalCount  int          `json:"critical_count"`
	PropagationPath []string    `json:"propagation_path"` // 传播路径
	EstimatedUsers int          `json:"estimated_users"`  // 估算受影响用户总数
}

func NewAnalyzeImpactTool(logger *zap.Logger) tool.BaseTool {
	return &AnalyzeImpactTool{
		logger: logger,
	}
}

func (t *AnalyzeImpactTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "analyze_impact",
		Desc: "分析故障的影响范围。评估受影响的下游服务、用户数量和影响程度。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"root_cause": {
				Type:     schema.String,
				Desc:     "根因服务名称",
				Required: true,
			},
			"dependency_graph": {
				Type:     schema.Object,
				Desc:     "依赖图（JSON 对象）",
				Required: true,
			},
		}),
	}, nil
}

func (t *AnalyzeImpactTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		RootCause       string                 `json:"root_cause"`
		DependencyGraph map[string]interface{} `json:"dependency_graph"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if in.RootCause == "" {
		return "", fmt.Errorf("root_cause is required")
	}

	// 1. 前向搜索受影响的服务
	impactedNodes := t.forwardSearch(in.RootCause, in.DependencyGraph)

	// 2. 计算传播路径
	propagationPath := t.calculatePropagationPath(in.RootCause, impactedNodes)

	// 3. 统计关键服务数量
	criticalCount := 0
	estimatedUsers := 0
	for _, node := range impactedNodes {
		if node.ImpactLevel == "critical" {
			criticalCount++
		}
		estimatedUsers += node.AffectedUsers
	}

	result := &ImpactAnalysis{
		RootCause:       in.RootCause,
		ImpactedNodes:   impactedNodes,
		TotalImpacted:   len(impactedNodes),
		CriticalCount:   criticalCount,
		PropagationPath: propagationPath,
		EstimatedUsers:  estimatedUsers,
	}

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("impact analysis completed",
			zap.String("root_cause", in.RootCause),
			zap.Int("impacted_nodes", len(impactedNodes)),
			zap.Int("critical_count", criticalCount))
	}

	return string(output), nil
}

// forwardSearch 前向搜索受影响的服务
func (t *AnalyzeImpactTool) forwardSearch(rootCause string, graph map[string]interface{}) []ImpactNode {
	impactedNodes := []ImpactNode{}

	// 提取 nodes
	nodesData, ok := graph["nodes"].(map[string]interface{})
	if !ok {
		return impactedNodes
	}

	// BFS 前向搜索
	visited := make(map[string]bool)
	queue := []struct {
		service  string
		distance int
	}{{rootCause, 0}}
	visited[rootCause] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// 获取当前节点
		nodeData, ok := nodesData[current.service].(map[string]interface{})
		if !ok {
			continue
		}

		// 获取依赖此服务的服务（下游）
		dependents, ok := nodeData["dependents"].([]interface{})
		if !ok {
			continue
		}

		for _, dep := range dependents {
			depName, ok := dep.(string)
			if !ok {
				continue
			}

			if !visited[depName] {
				visited[depName] = true
				distance := current.distance + 1
				queue = append(queue, struct {
					service  string
					distance int
				}{depName, distance})

				// 创建影响节点
				impactNode := t.createImpactNode(depName, distance, nodesData)
				impactedNodes = append(impactedNodes, impactNode)
			}
		}
	}

	return impactedNodes
}

// createImpactNode 创建影响节点
func (t *AnalyzeImpactTool) createImpactNode(service string, distance int, nodesData map[string]interface{}) ImpactNode {
	node := ImpactNode{
		Service:  service,
		Distance: distance,
	}

	// 获取节点信息
	nodeData, ok := nodesData[service].(map[string]interface{})
	if !ok {
		return node
	}

	// 判断是否关键服务
	critical, _ := nodeData["critical"].(bool)

	// 计算影响级别
	if distance == 1 {
		node.ImpactType = "direct"
		if critical {
			node.ImpactLevel = "critical"
			node.AffectedUsers = 10000
			node.Probability = 0.9
		} else {
			node.ImpactLevel = "high"
			node.AffectedUsers = 5000
			node.Probability = 0.8
		}
	} else if distance == 2 {
		node.ImpactType = "indirect"
		if critical {
			node.ImpactLevel = "high"
			node.AffectedUsers = 5000
			node.Probability = 0.7
		} else {
			node.ImpactLevel = "medium"
			node.AffectedUsers = 2000
			node.Probability = 0.6
		}
	} else {
		node.ImpactType = "indirect"
		node.ImpactLevel = "low"
		node.AffectedUsers = 1000
		node.Probability = 0.4
	}

	return node
}

// calculatePropagationPath 计算传播路径
func (t *AnalyzeImpactTool) calculatePropagationPath(rootCause string, impactedNodes []ImpactNode) []string {
	path := []string{rootCause}

	// 按距离排序
	for distance := 1; distance <= 3; distance++ {
		for _, node := range impactedNodes {
			if node.Distance == distance && node.ImpactLevel == "critical" {
				path = append(path, node.Service)
				break
			}
		}
	}

	return path
}
