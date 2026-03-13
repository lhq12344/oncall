package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// BuildDependencyGraphTool 依赖图构建工具
type BuildDependencyGraphTool struct {
	logger *zap.Logger
}

// ServiceNode 服务节点
type ServiceNode struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`         // service/database/cache/mq
	Dependencies []string `json:"dependencies"` // 依赖的服务
	Dependents   []string `json:"dependents"`   // 依赖此服务的服务
	Critical     bool     `json:"critical"`     // 是否关键服务
}

// DependencyGraph 依赖图
type DependencyGraph struct {
	Nodes      map[string]*ServiceNode `json:"nodes"`
	Edges      []Edge                  `json:"edges"`
	Layers     [][]string              `json:"layers"` // 分层结构
	TotalNodes int                     `json:"total_nodes"`
}

// Edge 依赖边
type Edge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Type   string `json:"type"`   // sync/async/data
	Weight int    `json:"weight"` // 调用频率权重
}

func NewBuildDependencyGraphTool(logger *zap.Logger) tool.BaseTool {
	return &BuildDependencyGraphTool{
		logger: logger,
	}
}

func (t *BuildDependencyGraphTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "build_dependency_graph",
		Desc: "构建服务依赖图。分析服务之间的调用关系，识别上下游依赖。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"services": {
				Type:     schema.Array,
				Desc:     "服务列表（JSON 数组），每个服务包含 name 和 dependencies",
				Required: false,
			},
			"auto_discover": {
				Type:     schema.Boolean,
				Desc:     "是否自动发现服务依赖，默认 true",
				Required: false,
			},
		}),
	}, nil
}

func (t *BuildDependencyGraphTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type serviceInput struct {
		Name         string   `json:"name"`
		Type         string   `json:"type"`
		Dependencies []string `json:"dependencies"`
		Critical     bool     `json:"critical"`
	}

	type args struct {
		Services     []serviceInput `json:"services"`
		AutoDiscover bool           `json:"auto_discover"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	// 默认自动发现
	if len(in.Services) == 0 {
		in.AutoDiscover = true
	}

	callCount := increaseRCAToolCallCount(ctx, "build_dependency_graph")
	cacheKeyRaw, _ := json.Marshal(in)
	cacheKey := string(cacheKeyRaw)
	if cached, ok := getRCACachedToolResult(ctx, "build_dependency_graph", cacheKey); ok {
		if t.logger != nil {
			t.logger.Info("dependency graph cache hit",
				zap.Int("call_count", callCount))
		}
		return cached, nil
	}

	var graph *DependencyGraph

	if in.AutoDiscover {
		// 自动发现服务依赖（模拟实现）
		graph = t.autoDiscoverDependencies()
	} else {
		// 转换类型
		convertedServices := make([]struct {
			Name         string
			Type         string
			Dependencies []string
			Critical     bool
		}, len(in.Services))

		for i, svc := range in.Services {
			convertedServices[i] = struct {
				Name         string
				Type         string
				Dependencies []string
				Critical     bool
			}{
				Name:         svc.Name,
				Type:         svc.Type,
				Dependencies: svc.Dependencies,
				Critical:     svc.Critical,
			}
		}

		// 根据输入构建依赖图
		graph = t.buildGraphFromInput(convertedServices)
	}

	// 计算分层结构
	graph.Layers = t.calculateLayers(graph)

	output, err := json.Marshal(graph)
	if err != nil {
		return "", fmt.Errorf("failed to marshal graph: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("dependency graph built",
			zap.Int("nodes", graph.TotalNodes),
			zap.Int("edges", len(graph.Edges)),
			zap.Int("layers", len(graph.Layers)),
			zap.Int("call_count", callCount))
	}

	setRCACachedToolResult(ctx, "build_dependency_graph", cacheKey, string(output))
	return string(output), nil
}

// autoDiscoverDependencies 自动发现服务依赖（模拟实现）
func (t *BuildDependencyGraphTool) autoDiscoverDependencies() *DependencyGraph {
	// TODO: 实际实现应该从服务注册中心、配置文件或 APM 系统获取
	// 这里返回一个示例依赖图

	nodes := map[string]*ServiceNode{
		"api-gateway": {
			Name:         "api-gateway",
			Type:         "service",
			Dependencies: []string{"user-service", "order-service"},
			Dependents:   []string{},
			Critical:     true,
		},
		"user-service": {
			Name:         "user-service",
			Type:         "service",
			Dependencies: []string{"user-db", "redis"},
			Dependents:   []string{"api-gateway"},
			Critical:     true,
		},
		"order-service": {
			Name:         "order-service",
			Type:         "service",
			Dependencies: []string{"order-db", "payment-service", "kafka"},
			Dependents:   []string{"api-gateway"},
			Critical:     true,
		},
		"payment-service": {
			Name:         "payment-service",
			Type:         "service",
			Dependencies: []string{"payment-db"},
			Dependents:   []string{"order-service"},
			Critical:     true,
		},
		"user-db": {
			Name:         "user-db",
			Type:         "database",
			Dependencies: []string{},
			Dependents:   []string{"user-service"},
			Critical:     true,
		},
		"order-db": {
			Name:         "order-db",
			Type:         "database",
			Dependencies: []string{},
			Dependents:   []string{"order-service"},
			Critical:     true,
		},
		"payment-db": {
			Name:         "payment-db",
			Type:         "database",
			Dependencies: []string{},
			Dependents:   []string{"payment-service"},
			Critical:     true,
		},
		"redis": {
			Name:         "redis",
			Type:         "cache",
			Dependencies: []string{},
			Dependents:   []string{"user-service"},
			Critical:     false,
		},
		"kafka": {
			Name:         "kafka",
			Type:         "mq",
			Dependencies: []string{},
			Dependents:   []string{"order-service"},
			Critical:     false,
		},
	}

	edges := []Edge{
		{From: "api-gateway", To: "user-service", Type: "sync", Weight: 100},
		{From: "api-gateway", To: "order-service", Type: "sync", Weight: 80},
		{From: "user-service", To: "user-db", Type: "sync", Weight: 100},
		{From: "user-service", To: "redis", Type: "sync", Weight: 50},
		{From: "order-service", To: "order-db", Type: "sync", Weight: 100},
		{From: "order-service", To: "payment-service", Type: "sync", Weight: 60},
		{From: "order-service", To: "kafka", Type: "async", Weight: 40},
		{From: "payment-service", To: "payment-db", Type: "sync", Weight: 100},
	}

	return &DependencyGraph{
		Nodes:      nodes,
		Edges:      edges,
		TotalNodes: len(nodes),
	}
}

// buildGraphFromInput 根据输入构建依赖图
func (t *BuildDependencyGraphTool) buildGraphFromInput(services []struct {
	Name         string
	Type         string
	Dependencies []string
	Critical     bool
}) *DependencyGraph {
	nodes := make(map[string]*ServiceNode)
	edges := []Edge{}

	// 创建节点
	for _, svc := range services {
		nodes[svc.Name] = &ServiceNode{
			Name:         svc.Name,
			Type:         svc.Type,
			Dependencies: svc.Dependencies,
			Dependents:   []string{},
			Critical:     svc.Critical,
		}
	}

	// 创建边并更新 dependents
	for _, svc := range services {
		for _, dep := range svc.Dependencies {
			edges = append(edges, Edge{
				From:   svc.Name,
				To:     dep,
				Type:   "sync",
				Weight: 50,
			})

			// 更新被依赖服务的 dependents
			if depNode, ok := nodes[dep]; ok {
				depNode.Dependents = append(depNode.Dependents, svc.Name)
			}
		}
	}

	return &DependencyGraph{
		Nodes:      nodes,
		Edges:      edges,
		TotalNodes: len(nodes),
	}
}

// calculateLayers 计算分层结构（拓扑排序）
func (t *BuildDependencyGraphTool) calculateLayers(graph *DependencyGraph) [][]string {
	// 计算每个节点的入度
	inDegree := make(map[string]int)
	for name := range graph.Nodes {
		inDegree[name] = 0
	}
	for _, edge := range graph.Edges {
		inDegree[edge.To]++
	}

	layers := [][]string{}
	visited := make(map[string]bool)

	// 逐层处理
	for len(visited) < graph.TotalNodes {
		currentLayer := []string{}

		// 找到入度为 0 的节点
		for name, degree := range inDegree {
			if !visited[name] && degree == 0 {
				currentLayer = append(currentLayer, name)
			}
		}

		if len(currentLayer) == 0 {
			// 有环或孤立节点
			break
		}

		layers = append(layers, currentLayer)

		// 标记已访问并减少相关节点的入度
		for _, name := range currentLayer {
			visited[name] = true
			for _, edge := range graph.Edges {
				if edge.From == name {
					inDegree[edge.To]--
				}
			}
		}
	}

	return layers
}
