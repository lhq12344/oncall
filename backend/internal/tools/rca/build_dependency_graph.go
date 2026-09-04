package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	k8sclient "go_agent/internal/adapters/kubernetes"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// BuildDependencyGraphTool 依赖图构建工具
type BuildDependencyGraphTool struct {
	client *k8sclient.Monitor
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
	Source     string                  `json:"source,omitempty"`
	Namespace  string                  `json:"namespace,omitempty"`
	Warnings   []string                `json:"warnings,omitempty"`
}

// Edge 依赖边
type Edge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Type   string `json:"type"`   // sync/async/data
	Weight int    `json:"weight"` // 调用频率权重
}

func NewBuildDependencyGraphTool(kubeconfig string, logger *zap.Logger) tool.BaseTool {
	monitor := k8sclient.NewMonitor(kubeconfig, logger)
	if !monitor.Available() && logger != nil {
		logger.Warn("failed to init rca k8s client, dependency graph will degrade to input-only mode")
	}
	return NewBuildDependencyGraphToolWithMonitor(monitor, logger)
}

func NewBuildDependencyGraphToolWithMonitor(monitor *k8sclient.Monitor, logger *zap.Logger) tool.BaseTool {
	return &BuildDependencyGraphTool{client: monitor, logger: logger}
}

func (t *BuildDependencyGraphTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "build_dependency_graph",
		Desc: "构建服务依赖图。分析服务之间的调用关系，识别上下游依赖。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"services": {
				Type:     schema.Array,
				ElemInfo: &schema.ParameterInfo{Type: schema.Object},
				Desc:     "服务列表（JSON 数组），每个服务包含 name 和 dependencies",
				Required: false,
			},
			"auto_discover": {
				Type:     schema.Boolean,
				Desc:     "是否自动发现服务依赖（优先从 K8s 真实资源发现），默认 true",
				Required: false,
			},
			"namespace": {
				Type:     schema.String,
				Desc:     "自动发现命名空间，默认 infra；支持 all/* 表示全命名空间",
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
		Namespace    string         `json:"namespace"`
	}

	var in args
	if err := unmarshalRCAArgsLenient(argumentsInJSON, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	// 默认自动发现
	if len(in.Services) == 0 {
		in.AutoDiscover = true
	}
	in.Namespace = strings.TrimSpace(in.Namespace)
	if in.Namespace == "" {
		in.Namespace = "infra"
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
		discovered, err := t.autoDiscoverDependencies(ctx, in.Namespace)
		if err != nil {
			graph = &DependencyGraph{
				Nodes:      map[string]*ServiceNode{},
				Edges:      []Edge{},
				Layers:     [][]string{},
				TotalNodes: 0,
				Source:     "auto_discover",
				Namespace:  in.Namespace,
				Warnings:   []string{err.Error()},
			}
		} else {
			graph = discovered
		}
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
		graph.Source = "input"
		graph.Namespace = in.Namespace
	}

	// 计算分层结构
	if graph != nil {
		graph.Layers = t.calculateLayers(graph)
	}

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

// autoDiscoverDependencies 自动发现服务依赖（基于 K8s 真实资源）。
// 输入：ctx、namespace（infra/all/*）。
// 输出：依赖图；当资源发现失败时返回错误。
func (t *BuildDependencyGraphTool) autoDiscoverDependencies(ctx context.Context, namespace string) (*DependencyGraph, error) {
	queryNamespace := strings.TrimSpace(namespace)
	if queryNamespace == "" {
		queryNamespace = "infra"
	}
	resources, err := t.client.DiscoverDependencyResources(ctx, queryNamespace)
	if err != nil {
		return nil, err
	}

	nodes := make(map[string]*ServiceNode)
	edges := make([]Edge, 0)
	warnings := append([]string(nil), resources.Warnings...)

	// 1) 注册 workload 节点
	workloadLabels := make(map[string]map[string]string)
	addWorkloadNode := func(namespace, name string, labels map[string]string, nodeType string, critical bool) {
		key := qualifyResourceName(namespace, name)
		nodes[key] = &ServiceNode{
			Name:         key,
			Type:         nodeType,
			Dependencies: []string{},
			Dependents:   []string{},
			Critical:     critical,
		}
		workloadLabels[key] = labels
	}

	for _, workload := range resources.Workloads {
		addWorkloadNode(
			workload.Namespace,
			workload.Name,
			workload.Labels,
			workloadNodeType(workload),
			isCriticalWorkload(workload.Name, workload.Replicas),
		)
	}

	// 2) 注册 service 节点并建立 service -> workload 依赖（selector）
	edgeSet := make(map[string]struct{})
	for _, service := range resources.Services {
		serviceKey := qualifyResourceName(service.Namespace, service.Name)
		serviceNode := &ServiceNode{
			Name:         serviceKey,
			Type:         "service",
			Dependencies: []string{},
			Dependents:   []string{},
			Critical:     isCriticalService(service.Name),
		}
		nodes[serviceKey] = serviceNode

		if len(service.Selector) == 0 {
			continue
		}

		for workloadKey, labels := range workloadLabels {
			if !matchSelector(service.Selector, labels) {
				continue
			}
			serviceNode.Dependencies = appendUniqueString(serviceNode.Dependencies, workloadKey)
			if workloadNode, ok := nodes[workloadKey]; ok {
				workloadNode.Dependents = appendUniqueString(workloadNode.Dependents, serviceKey)
			}

			edge := Edge{
				From:   serviceKey,
				To:     workloadKey,
				Type:   "sync",
				Weight: 100,
			}
			edgeKey := buildEdgeKey(edge)
			if _, exists := edgeSet[edgeKey]; exists {
				continue
			}
			edgeSet[edgeKey] = struct{}{}
			edges = append(edges, edge)
		}
	}

	graph := &DependencyGraph{
		Nodes:      nodes,
		Edges:      edges,
		TotalNodes: len(nodes),
		Source:     "k8s",
		Namespace:  namespace,
		Warnings:   warnings,
	}
	return graph, nil
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
	if graph == nil || len(graph.Nodes) == 0 {
		return [][]string{}
	}

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

// qualifyResourceName 生成带命名空间的资源名。
// 输入：namespace、name。
// 输出：namespace/name 格式资源名。
func qualifyResourceName(namespace, name string) string {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

// matchSelector 判断 labels 是否满足 selector。
// 输入：selector、labels。
// 输出：true 表示命中；false 表示不命中。
func matchSelector(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	if len(labels) == 0 {
		return false
	}
	for key, value := range selector {
		labelValue, exists := labels[key]
		if !exists || strings.TrimSpace(labelValue) != strings.TrimSpace(value) {
			return false
		}
	}
	return true
}

// appendUniqueString 去重追加字符串。
// 输入：原切片、待追加值。
// 输出：追加后的切片。
func appendUniqueString(values []string, item string) []string {
	item = strings.TrimSpace(item)
	if item == "" {
		return values
	}
	for _, value := range values {
		if value == item {
			return values
		}
	}
	return append(values, item)
}

// buildEdgeKey 构建边去重键。
// 输入：Edge。
// 输出：唯一键。
func buildEdgeKey(edge Edge) string {
	return edge.From + "|" + edge.To + "|" + edge.Type
}

// isCriticalWorkload 判断 workload 是否关键。
// 输入：name、replicas。
// 输出：是否关键。
func isCriticalWorkload(name string, replicas *int32) bool {
	if replicas != nil && *replicas <= 1 {
		return true
	}
	lowerName := strings.ToLower(strings.TrimSpace(name))
	keywords := []string{"mysql", "redis", "etcd", "milvus", "kafka", "nacos", "gateway"}
	for _, keyword := range keywords {
		if strings.Contains(lowerName, keyword) {
			return true
		}
	}
	return false
}

// isCriticalService 判断 service 是否关键。
// 输入：service 名称。
// 输出：是否关键。
func isCriticalService(name string) bool {
	lowerName := strings.ToLower(strings.TrimSpace(name))
	keywords := []string{"mysql", "redis", "etcd", "milvus", "kafka", "nacos", "gateway"}
	for _, keyword := range keywords {
		if strings.Contains(lowerName, keyword) {
			return true
		}
	}
	return false
}

// classifyStatefulType 推断 StatefulSet 节点类型。
// 输入：statefulset 名称。
// 输出：节点类型（database/cache/mq/service）。
func workloadNodeType(workload k8sclient.DiscoveredWorkload) string {
	if strings.EqualFold(workload.Kind, "statefulset") {
		return classifyStatefulType(workload.Name)
	}
	return "service"
}
func classifyStatefulType(name string) string {
	lowerName := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(lowerName, "mysql"),
		strings.Contains(lowerName, "postgres"),
		strings.Contains(lowerName, "mongo"),
		strings.Contains(lowerName, "milvus"),
		strings.Contains(lowerName, "etcd"):
		return "database"
	case strings.Contains(lowerName, "redis"):
		return "cache"
	case strings.Contains(lowerName, "kafka"),
		strings.Contains(lowerName, "mq"):
		return "mq"
	default:
		return "service"
	}
}
