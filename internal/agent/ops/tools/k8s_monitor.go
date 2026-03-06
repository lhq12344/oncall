package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
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
		Desc: "监控 Kubernetes 资源状态。查询 Pod、Node、Deployment 等资源的运行状态和健康情况。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"namespace": {
				Type:     schema.String,
				Desc:     "命名空间（默认 default）",
				Required: false,
			},
			"resource_type": {
				Type:     schema.String,
				Desc:     "资源类型：pod/node/deployment/service",
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

	// ���认命名空间
	if in.Namespace == "" {
		in.Namespace = "default"
	}

	in.ResourceType = strings.ToLower(strings.TrimSpace(in.ResourceType))
	if in.ResourceType == "" {
		return "", fmt.Errorf("resource_type is required")
	}

	// 如果客户端未初始化，返回降级数据
	if t.client == nil {
		return t.fallbackResponse(in.ResourceType, in.Namespace, in.ResourceName), nil
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
	case "service", "services":
		result, err = t.monitorServices(ctx, in.Namespace, in.ResourceName)
	default:
		return "", fmt.Errorf("unsupported resource type: %s", in.ResourceType)
	}

	if err != nil {
		if t.logger != nil {
			t.logger.Error("k8s monitor failed",
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
			zap.String("resource_type", in.ResourceType),
			zap.String("namespace", in.Namespace))
	}

	return string(output), nil
}

// monitorPods 监控 Pod 状态
func (t *K8sMonitorTool) monitorPods(ctx context.Context, namespace, name string) (interface{}, error) {
	if name != "" {
		// 查询单个 Pod
		pod, err := t.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
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
		"name":               pod.Name,
		"namespace":          pod.Namespace,
		"phase":              string(pod.Status.Phase),
		"node":               pod.Spec.NodeName,
		"ip":                 pod.Status.PodIP,
		"containers":         containerStatuses,
		"created_at":         pod.CreationTimestamp.Time.Format("2006-01-02 15:04:05"),
		"restart_policy":     string(pod.Spec.RestartPolicy),
		"service_account":    pod.Spec.ServiceAccountName,
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
			"os_image":           node.Status.NodeInfo.OSImage,
			"kernel_version":     node.Status.NodeInfo.KernelVersion,
			"container_runtime":  node.Status.NodeInfo.ContainerRuntimeVersion,
			"kubelet_version":    node.Status.NodeInfo.KubeletVersion,
		},
	}
}

// monitorDeployments 监控 Deployment 状态
func (t *K8sMonitorTool) monitorDeployments(ctx context.Context, namespace, name string) (interface{}, error) {
	if name != "" {
		deploy, err := t.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"name":               deploy.Name,
			"namespace":          deploy.Namespace,
			"replicas":           *deploy.Spec.Replicas,
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
			"replicas":           *deploy.Spec.Replicas,
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

// monitorServices 监控 Service 状态
func (t *K8sMonitorTool) monitorServices(ctx context.Context, namespace, name string) (interface{}, error) {
	if name != "" {
		svc, err := t.client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"name":        svc.Name,
			"namespace":   svc.Namespace,
			"type":        string(svc.Spec.Type),
			"cluster_ip":  svc.Spec.ClusterIP,
			"ports":       svc.Spec.Ports,
			"selector":    svc.Spec.Selector,
			"created_at":  svc.CreationTimestamp.Time.Format("2006-01-02 15:04:05"),
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
		"error":   "k8s_client_unavailable",
		"message": msg,
		"suggestion": "Please check K8s configuration and ensure the cluster is accessible",
	}

	output, _ := json.Marshal(result)
	return string(output)
}



