package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// K8sMonitorTool K8s 监控工具
type K8sMonitorTool struct {
	client *kubernetes.Clientset
	logger *zap.Logger
}

// NewK8sMonitorTool 创建 K8s 监控工具
func NewK8sMonitorTool(kubeconfig string, logger *zap.Logger) (tool.BaseTool, error) {
	var config *rest.Config
	var err error

	// 尝试使用 kubeconfig 文件
	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			// 降级：尝试使用 in-cluster 配置
			config, err = rest.InClusterConfig()
			if err != nil {
				if logger != nil {
					logger.Warn("failed to create k8s config, tool will return placeholder data",
						zap.Error(err))
				}
				// 返回一个没有客户端的工具（降级模式）
				return &K8sMonitorTool{
					client: nil,
					logger: logger,
				}, nil
			}
		}
	} else {
		// 尝试 in-cluster 配置
		config, err = rest.InClusterConfig()
		if err != nil {
			if logger != nil {
				logger.Warn("failed to create k8s config, tool will return placeholder data",
					zap.Error(err))
			}
			return &K8sMonitorTool{
				client: nil,
				logger: logger,
			}, nil
		}
	}

	// 创建 clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		if logger != nil {
			logger.Warn("failed to create k8s client, tool will return placeholder data",
				zap.Error(err))
		}
		return &K8sMonitorTool{
			client: nil,
			logger: logger,
		}, nil
	}

	if logger != nil {
		logger.Info("k8s client initialized successfully")
	}

	return &K8sMonitorTool{
		client: clientset,
		logger: logger,
	}, nil
}

func (t *K8sMonitorTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "k8s_monitor",
		Desc: "监控 Kubernetes 资源状态。查询 Pod、Node、Deployment、StatefulSet、Service 等资源的运行状态和健康情况。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"namespace": {
				Type:     schema.String,
				Desc:     "命名空间（默认 infra）",
				Required: false,
			},
			"resource_type": {
				Type:     schema.String,
				Desc:     "资源类型：pod/node/deployment/statefulset/service",
				Required: true,
			},
			"resource_name": {
				Type:     schema.String,
				Desc:     "资源名称（可选，不填则列出所有）",
				Required: false,
			},
		}),
	}, nil
}

func (t *K8sMonitorTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Namespace    string `json:"namespace"`
		ResourceType string `json:"resource_type"`
		ResourceName string `json:"resource_name"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	// 确认命名空间
	if in.Namespace == "" {
		in.Namespace = "infra"
	}
	in.Namespace = strings.TrimSpace(in.Namespace)

	in.ResourceType = strings.ToLower(strings.TrimSpace(in.ResourceType))
	if in.ResourceType == "" {
		return "", fmt.Errorf("resource_type is required")
	}
	in.ResourceName = strings.TrimSpace(in.ResourceName)

	callCount := increaseToolCallCount(ctx, "k8s_monitor")
	cacheKey := strings.ToLower(in.Namespace) + "|" + in.ResourceType + "|" + strings.ToLower(in.ResourceName)
	if cached, ok := getCachedToolResult(ctx, "k8s_monitor", cacheKey); ok {
		if t.logger != nil {
			t.logger.Info("k8s monitor cache hit",
				zap.String("agent", currentAgentForLog(ctx, "ops_agent")),
				zap.String("resource_type", in.ResourceType),
				zap.String("namespace", in.Namespace),
				zap.String("resource_name", in.ResourceName),
				zap.Int("call_count", callCount))
		}
		return cached, nil
	}

	// 如果客户端未初始化，返回降级数据
	if t.client == nil {
		output := t.fallbackResponse(in.ResourceType, in.Namespace, in.ResourceName)
		setCachedToolResult(ctx, "k8s_monitor", cacheKey, output)
		return output, nil
	}

	// 根据资源类型查询
	var result interface{}
	var err error

	switch in.ResourceType {
	case "pod", "pods":
		result, err = t.monitorPods(ctx, in.Namespace, in.ResourceName)
	case "node", "nodes":
		result, err = t.monitorNodes(ctx, in.ResourceName)
	case "deployment", "deployments":
		result, err = t.monitorDeployments(ctx, in.Namespace, in.ResourceName)
	case "statefulset", "statefulsets":
		result, err = t.monitorStatefulSets(ctx, in.Namespace, in.ResourceName)
	case "service", "services":
		result, err = t.monitorServices(ctx, in.Namespace, in.ResourceName)
	default:
		return "", fmt.Errorf("unsupported resource type: %s", in.ResourceType)
	}

	if err != nil {
		if t.logger != nil {
			t.logger.Error("k8s monitor failed",
				zap.String("agent", currentAgentForLog(ctx, "ops_agent")),
				zap.String("resource_type", in.ResourceType),
				zap.String("namespace", in.Namespace),
				zap.Error(err))
		}
		return "", fmt.Errorf("failed to monitor %s: %w", in.ResourceType, err)
	}

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("k8s monitor completed",
			zap.String("agent", currentAgentForLog(ctx, "ops_agent")),
			zap.String("resource_type", in.ResourceType),
			zap.String("namespace", in.Namespace),
			zap.Int("call_count", callCount))
	}

	setCachedToolResult(ctx, "k8s_monitor", cacheKey, string(output))
	return string(output), nil
}

// monitorPods 监控 Pod 状态
func (t *K8sMonitorTool) monitorPods(ctx context.Context, namespace, name string) (interface{}, error) {
	if name != "" {
		// 查询单个 Pod
		pod, err := t.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return nil, err
			}

			// 降级：当精确名称不存在时，返回命名空间内模糊匹配结果，避免工具直接报错中断链路。
			pods, listErr := t.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
			if listErr != nil {
				return nil, listErr
			}

			matched := make([]map[string]interface{}, 0)
			for _, item := range pods.Items {
				if !matchPodByHint(&item, name) {
					continue
				}
				matched = append(matched, t.formatPodInfo(&item))
			}

			return map[string]interface{}{
				"namespace":      namespace,
				"requested_name": name,
				"matched_exact":  false,
				"count":          len(matched),
				"pods":           matched,
				"message":        "指定 Pod 不存在，已返回按名称/标签模糊匹配结果",
			}, nil
		}
		return t.formatPodInfo(pod), nil
	}

	// 列出所有 Pod
	pods, err := t.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(pods.Items))
	for _, pod := range pods.Items {
		result = append(result, t.formatPodInfo(&pod))
	}

	return map[string]interface{}{
		"namespace": namespace,
		"count":     len(result),
		"pods":      result,
	}, nil
}

// matchPodByHint 判断 Pod 是否与用户输入名称提示匹配。
// 输入：pod 对象、用户输入的资源名提示。
// 输出：true 表示名称或常见标签命中；false 表示不命中。
func matchPodByHint(pod *corev1.Pod, hint string) bool {
	if pod == nil {
		return false
	}
	hint = strings.ToLower(strings.TrimSpace(hint))
	if hint == "" {
		return false
	}

	podName := strings.ToLower(strings.TrimSpace(pod.Name))
	if podName == hint || strings.Contains(podName, hint) {
		return true
	}

	candidateKeys := []string{"app", "app.kubernetes.io/name", "app.kubernetes.io/instance"}
	for _, key := range candidateKeys {
		value, ok := pod.Labels[key]
		if !ok {
			continue
		}
		labelValue := strings.ToLower(strings.TrimSpace(value))
		if labelValue == hint || strings.Contains(labelValue, hint) {
			return true
		}
	}

	return false
}

// formatPodInfo 格式化 Pod 信息
func (t *K8sMonitorTool) formatPodInfo(pod *corev1.Pod) map[string]interface{} {
	// 计算容器状态
	containerStatuses := make([]map[string]interface{}, 0, len(pod.Status.ContainerStatuses))
	for _, cs := range pod.Status.ContainerStatuses {
		status := map[string]interface{}{
			"name":          cs.Name,
			"ready":         cs.Ready,
			"restart_count": cs.RestartCount,
			"image":         cs.Image,
		}

		// 添加状态详情
		if cs.State.Running != nil {
			status["state"] = "running"
			status["started_at"] = cs.State.Running.StartedAt.Time.Format("2006-01-02 15:04:05")
		} else if cs.State.Waiting != nil {
			status["state"] = "waiting"
			status["reason"] = cs.State.Waiting.Reason
			status["message"] = cs.State.Waiting.Message
		} else if cs.State.Terminated != nil {
			status["state"] = "terminated"
			status["reason"] = cs.State.Terminated.Reason
			status["exit_code"] = cs.State.Terminated.ExitCode
		}

		containerStatuses = append(containerStatuses, status)
	}

	return map[string]interface{}{
		"name":            pod.Name,
		"namespace":       pod.Namespace,
		"phase":           string(pod.Status.Phase),
		"node":            pod.Spec.NodeName,
		"ip":              pod.Status.PodIP,
		"containers":      containerStatuses,
		"created_at":      pod.CreationTimestamp.Time.Format("2006-01-02 15:04:05"),
		"restart_policy":  string(pod.Spec.RestartPolicy),
		"service_account": pod.Spec.ServiceAccountName,
	}
}

// monitorNodes 监控 Node 状态
func (t *K8sMonitorTool) monitorNodes(ctx context.Context, name string) (interface{}, error) {
	if name != "" {
		node, err := t.client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return t.formatNodeInfo(node), nil
	}

	nodes, err := t.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		result = append(result, t.formatNodeInfo(&node))
	}

	return map[string]interface{}{
		"count": len(result),
		"nodes": result,
	}, nil
}

// formatNodeInfo 格式化 Node 信息
func (t *K8sMonitorTool) formatNodeInfo(node *corev1.Node) map[string]interface{} {
	// 节点状态
	conditions := make([]map[string]interface{}, 0, len(node.Status.Conditions))
	for _, cond := range node.Status.Conditions {
		conditions = append(conditions, map[string]interface{}{
			"type":    string(cond.Type),
			"status":  string(cond.Status),
			"reason":  cond.Reason,
			"message": cond.Message,
		})
	}

	// 资源容量和可分配
	capacity := node.Status.Capacity
	allocatable := node.Status.Allocatable

	return map[string]interface{}{
		"name":       node.Name,
		"conditions": conditions,
		"capacity": map[string]interface{}{
			"cpu":    capacity.Cpu().String(),
			"memory": capacity.Memory().String(),
			"pods":   capacity.Pods().String(),
		},
		"allocatable": map[string]interface{}{
			"cpu":    allocatable.Cpu().String(),
			"memory": allocatable.Memory().String(),
			"pods":   allocatable.Pods().String(),
		},
		"node_info": map[string]interface{}{
			"os_image":          node.Status.NodeInfo.OSImage,
			"kernel_version":    node.Status.NodeInfo.KernelVersion,
			"container_runtime": node.Status.NodeInfo.ContainerRuntimeVersion,
			"kubelet_version":   node.Status.NodeInfo.KubeletVersion,
		},
	}
}

// monitorDeployments 监控 Deployment 状态
func (t *K8sMonitorTool) monitorDeployments(ctx context.Context, namespace, name string) (interface{}, error) {
	if name != "" {
		deploy, err := t.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return nil, err
			}

			// 降级：当精确名称不存在时，返回命名空间内模糊匹配结果，避免直接报错中断链路。
			deploys, listErr := t.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
			if listErr != nil {
				return nil, listErr
			}

			matched := make([]map[string]interface{}, 0)
			for _, item := range deploys.Items {
				if !matchDeploymentByHint(&item, name) {
					continue
				}
				matched = append(matched, map[string]interface{}{
					"name":               item.Name,
					"replicas":           desiredReplicas(item.Spec.Replicas),
					"ready_replicas":     item.Status.ReadyReplicas,
					"available_replicas": item.Status.AvailableReplicas,
				})
			}

			return map[string]interface{}{
				"namespace":      namespace,
				"requested_name": name,
				"matched_exact":  false,
				"count":          len(matched),
				"deployments":    matched,
				"message":        "指定 Deployment 不存在，已返回按名称/标签模糊匹配结果",
			}, nil
		}
		return map[string]interface{}{
			"name":               deploy.Name,
			"namespace":          deploy.Namespace,
			"replicas":           desiredReplicas(deploy.Spec.Replicas),
			"ready_replicas":     deploy.Status.ReadyReplicas,
			"available_replicas": deploy.Status.AvailableReplicas,
			"updated_replicas":   deploy.Status.UpdatedReplicas,
			"created_at":         deploy.CreationTimestamp.Time.Format("2006-01-02 15:04:05"),
		}, nil
	}

	deploys, err := t.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(deploys.Items))
	for _, deploy := range deploys.Items {
		result = append(result, map[string]interface{}{
			"name":               deploy.Name,
			"replicas":           desiredReplicas(deploy.Spec.Replicas),
			"ready_replicas":     deploy.Status.ReadyReplicas,
			"available_replicas": deploy.Status.AvailableReplicas,
		})
	}

	return map[string]interface{}{
		"namespace":   namespace,
		"count":       len(result),
		"deployments": result,
	}, nil
}

// monitorStatefulSets 监控 StatefulSet 状态。
// 输入：ctx、namespace、name（可选；为空时返回列表）。
// 输出：单个 StatefulSet 详情或列表聚合信息。
func (t *K8sMonitorTool) monitorStatefulSets(ctx context.Context, namespace, name string) (interface{}, error) {
	if name != "" {
		statefulSet, err := t.client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return nil, err
			}

			// 降级：当精确名称不存在时，返回命名空间内模糊匹配结果，避免直接报错中断链路。
			statefulSets, listErr := t.client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
			if listErr != nil {
				return nil, listErr
			}

			matched := make([]map[string]interface{}, 0)
			for _, item := range statefulSets.Items {
				if !matchStatefulSetByHint(&item, name) {
					continue
				}
				matched = append(matched, map[string]interface{}{
					"name":             item.Name,
					"replicas":         desiredReplicas(item.Spec.Replicas),
					"ready_replicas":   item.Status.ReadyReplicas,
					"current_replicas": item.Status.CurrentReplicas,
					"service_name":     item.Spec.ServiceName,
				})
			}

			return map[string]interface{}{
				"namespace":      namespace,
				"requested_name": name,
				"matched_exact":  false,
				"count":          len(matched),
				"statefulsets":   matched,
				"message":        "指定 StatefulSet 不存在，已返回按名称/ServiceName 模糊匹配结果",
			}, nil
		}
		return map[string]interface{}{
			"name":             statefulSet.Name,
			"namespace":        statefulSet.Namespace,
			"replicas":         desiredReplicas(statefulSet.Spec.Replicas),
			"ready_replicas":   statefulSet.Status.ReadyReplicas,
			"current_replicas": statefulSet.Status.CurrentReplicas,
			"updated_replicas": statefulSet.Status.UpdatedReplicas,
			"current_revision": statefulSet.Status.CurrentRevision,
			"update_revision":  statefulSet.Status.UpdateRevision,
			"service_name":     statefulSet.Spec.ServiceName,
			"pod_management":   string(statefulSet.Spec.PodManagementPolicy),
			"created_at":       statefulSet.CreationTimestamp.Time.Format("2006-01-02 15:04:05"),
		}, nil
	}

	statefulSets, err := t.client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(statefulSets.Items))
	for _, statefulSet := range statefulSets.Items {
		result = append(result, map[string]interface{}{
			"name":             statefulSet.Name,
			"replicas":         desiredReplicas(statefulSet.Spec.Replicas),
			"ready_replicas":   statefulSet.Status.ReadyReplicas,
			"current_replicas": statefulSet.Status.CurrentReplicas,
			"service_name":     statefulSet.Spec.ServiceName,
		})
	}

	return map[string]interface{}{
		"namespace":    namespace,
		"count":        len(result),
		"statefulsets": result,
	}, nil
}

// desiredReplicas 返回期望副本数。
// 输入：K8s workload 的 replicas 指针。
// 输出：副本数，空指针时返回 0。
func desiredReplicas(replicas *int32) int32 {
	if replicas == nil {
		return 0
	}
	return *replicas
}

// monitorServices 监控 Service 状态
func (t *K8sMonitorTool) monitorServices(ctx context.Context, namespace, name string) (interface{}, error) {
	if name != "" {
		svc, err := t.client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return nil, err
			}

			// 降级：当精确名称不存在时，返回命名空间内模糊匹配结果，避免直接报错中断链路。
			svcs, listErr := t.client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
			if listErr != nil {
				return nil, listErr
			}

			matched := make([]map[string]interface{}, 0)
			for _, item := range svcs.Items {
				if !matchServiceByHint(&item, name) {
					continue
				}
				matched = append(matched, map[string]interface{}{
					"name":       item.Name,
					"type":       string(item.Spec.Type),
					"cluster_ip": item.Spec.ClusterIP,
					"selector":   item.Spec.Selector,
				})
			}

			return map[string]interface{}{
				"namespace":      namespace,
				"requested_name": name,
				"matched_exact":  false,
				"count":          len(matched),
				"services":       matched,
				"message":        "指定 Service 不存在，已返回按名称/selector 模糊匹配结果",
			}, nil
		}
		return map[string]interface{}{
			"name":       svc.Name,
			"namespace":  svc.Namespace,
			"type":       string(svc.Spec.Type),
			"cluster_ip": svc.Spec.ClusterIP,
			"ports":      svc.Spec.Ports,
			"selector":   svc.Spec.Selector,
			"created_at": svc.CreationTimestamp.Time.Format("2006-01-02 15:04:05"),
		}, nil
	}

	svcs, err := t.client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(svcs.Items))
	for _, svc := range svcs.Items {
		result = append(result, map[string]interface{}{
			"name":       svc.Name,
			"type":       string(svc.Spec.Type),
			"cluster_ip": svc.Spec.ClusterIP,
		})
	}

	return map[string]interface{}{
		"namespace": namespace,
		"count":     len(result),
		"services":  result,
	}, nil
}

// fallbackResponse 降级响应
func (t *K8sMonitorTool) fallbackResponse(resourceType, namespace, name string) string {
	msg := fmt.Sprintf("K8s client not available. Cannot query %s in namespace %s", resourceType, namespace)
	if name != "" {
		msg += fmt.Sprintf(" (name: %s)", name)
	}

	result := map[string]interface{}{
		"error":      "k8s_client_unavailable",
		"message":    msg,
		"suggestion": "Please check K8s configuration and ensure the cluster is accessible",
	}

	output, _ := json.Marshal(result)
	return string(output)
}

// matchStatefulSetByHint 判断 StatefulSet 是否与用户输入名称提示匹配。
// 输入：statefulSet 对象、用户输入的资源名提示。
// 输出：true 表示名称或 ServiceName 命中；false 表示不命中。
func matchStatefulSetByHint(statefulSet *appsv1.StatefulSet, hint string) bool {
	if statefulSet == nil {
		return false
	}
	hint = strings.ToLower(strings.TrimSpace(hint))
	if hint == "" {
		return false
	}

	name := strings.ToLower(strings.TrimSpace(statefulSet.Name))
	if name == hint || strings.Contains(name, hint) {
		return true
	}

	serviceName := strings.ToLower(strings.TrimSpace(statefulSet.Spec.ServiceName))
	return serviceName == hint || strings.Contains(serviceName, hint)
}

// matchDeploymentByHint 判断 Deployment 是否与用户输入名称提示匹配。
// 输入：deployment 对象、用户输入的资源名提示。
// 输出：true 表示名称或常见标签命中；false 表示不命中。
func matchDeploymentByHint(deployment *appsv1.Deployment, hint string) bool {
	if deployment == nil {
		return false
	}
	hint = strings.ToLower(strings.TrimSpace(hint))
	if hint == "" {
		return false
	}

	name := strings.ToLower(strings.TrimSpace(deployment.Name))
	if name == hint || strings.Contains(name, hint) {
		return true
	}

	candidateKeys := []string{"app", "app.kubernetes.io/name", "app.kubernetes.io/instance"}
	for _, key := range candidateKeys {
		value, ok := deployment.Labels[key]
		if !ok {
			continue
		}
		labelValue := strings.ToLower(strings.TrimSpace(value))
		if labelValue == hint || strings.Contains(labelValue, hint) {
			return true
		}
	}

	return false
}

// matchServiceByHint 判断 Service 是否与用户输入名称提示匹配。
// 输入：service 对象、用户输入的资源名提示。
// 输出：true 表示名称或 selector 命中；false 表示不命中。
func matchServiceByHint(service *corev1.Service, hint string) bool {
	if service == nil {
		return false
	}
	hint = strings.ToLower(strings.TrimSpace(hint))
	if hint == "" {
		return false
	}

	name := strings.ToLower(strings.TrimSpace(service.Name))
	if name == hint || strings.Contains(name, hint) {
		return true
	}

	for _, value := range service.Spec.Selector {
		selectorValue := strings.ToLower(strings.TrimSpace(value))
		if selectorValue == hint || strings.Contains(selectorValue, hint) {
			return true
		}
	}

	return false
}
