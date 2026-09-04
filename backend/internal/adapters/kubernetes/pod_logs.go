package kubernetes

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type PodLogReader struct {
	client *kubernetes.Clientset
}

type PodLogTarget struct {
	Namespace  string
	Name       string
	UID        string
	NodeName   string
	Labels     map[string]string
	Containers []PodLogContainer
	Metadata   PodLogMetadata
}

type PodLogContainer struct {
	Name string
	Type string
}

type PodLogMetadata struct {
	App       string
	Workload  string
	OwnerKind string
	OwnerName string
}

func NewPodLogReader(kubeconfig string) (*PodLogReader, error) {
	clientset, err := NewClientset(kubeconfig)
	if err != nil {
		return nil, err
	}
	return NewPodLogReaderWithClient(clientset), nil
}

func NewPodLogReaderWithClient(clientset *kubernetes.Clientset) *PodLogReader {
	return &PodLogReader{client: clientset}
}

func (r *PodLogReader) Available() bool {
	return r != nil && r.client != nil
}

func (r *PodLogReader) ResolveNamespaces(ctx context.Context, namespaces []string) ([]string, error) {
	if !r.Available() {
		return nil, fmt.Errorf("k8s client unavailable")
	}
	if len(namespaces) == 1 && namespaces[0] == "*" {
		list, err := r.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list namespaces failed: %w", err)
		}
		out := make([]string, 0, len(list.Items))
		for _, item := range list.Items {
			out = append(out, item.Name)
		}
		return out, nil
	}
	return append([]string(nil), namespaces...), nil
}

func (r *PodLogReader) ListPods(ctx context.Context, namespace string) ([]PodLogTarget, error) {
	if !r.Available() {
		return nil, fmt.Errorf("k8s client unavailable")
	}
	pods, err := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods in namespace %s failed: %w", namespace, err)
	}

	out := make([]PodLogTarget, 0, len(pods.Items))
	for i := range pods.Items {
		target, err := r.buildPodLogTarget(ctx, &pods.Items[i])
		if err != nil {
			return nil, err
		}
		out = append(out, target)
	}
	return out, nil
}

func (r *PodLogReader) StreamPodLogs(ctx context.Context, pod PodLogTarget, container PodLogContainer, since *time.Time, tailLines int64) (io.ReadCloser, error) {
	if !r.Available() {
		return nil, fmt.Errorf("k8s client unavailable")
	}
	logOptions := &corev1.PodLogOptions{
		Container:  container.Name,
		Timestamps: true,
	}
	if since != nil && !since.IsZero() {
		logOptions.SinceTime = &metav1.Time{Time: *since}
	} else if tailLines > 0 {
		logOptions.TailLines = &tailLines
	}
	return r.client.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOptions).Stream(ctx)
}

func (r *PodLogReader) buildPodLogTarget(ctx context.Context, pod *corev1.Pod) (PodLogTarget, error) {
	if pod == nil {
		return PodLogTarget{}, nil
	}
	metadata, err := r.resolvePodLogMetadata(ctx, pod)
	if err != nil {
		metadata = PodLogMetadata{App: extractPodAppName(pod), Workload: firstNonEmptyLogText(extractPodAppName(pod), pod.Name)}
	}
	return PodLogTarget{
		Namespace:  pod.Namespace,
		Name:       pod.Name,
		UID:        string(pod.UID),
		NodeName:   pod.Spec.NodeName,
		Labels:     cloneStringMap(pod.Labels),
		Containers: podContainersForLogging(pod),
		Metadata:   metadata,
	}, nil
}

func podContainersForLogging(pod *corev1.Pod) []PodLogContainer {
	if pod == nil {
		return nil
	}
	containers := make([]PodLogContainer, 0, len(pod.Spec.InitContainers)+len(pod.Spec.Containers))
	for _, container := range pod.Spec.InitContainers {
		containers = append(containers, PodLogContainer{Name: container.Name, Type: "init"})
	}
	for _, container := range pod.Spec.Containers {
		containers = append(containers, PodLogContainer{Name: container.Name, Type: "app"})
	}
	return containers
}

func (r *PodLogReader) resolvePodLogMetadata(ctx context.Context, pod *corev1.Pod) (PodLogMetadata, error) {
	metadata := PodLogMetadata{App: extractPodAppName(pod)}
	ownerKind, ownerName, err := r.resolvePodOwner(ctx, pod)
	if err != nil {
		metadata.Workload = firstNonEmptyLogText(metadata.App, pod.Name)
		return metadata, err
	}
	metadata.OwnerKind = ownerKind
	metadata.OwnerName = ownerName
	metadata.Workload = firstNonEmptyLogText(ownerName, metadata.App, pod.Name)
	return metadata, nil
}

func extractPodAppName(pod *corev1.Pod) string {
	if pod == nil {
		return ""
	}
	candidateKeys := []string{"app", "app.kubernetes.io/name", "app.kubernetes.io/instance", "app.kubernetes.io/component", "k8s-app", "component"}
	for _, key := range candidateKeys {
		if value, ok := pod.Labels[key]; ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (r *PodLogReader) resolvePodOwner(ctx context.Context, pod *corev1.Pod) (string, string, error) {
	if pod == nil {
		return "", "", nil
	}
	owner := metav1.GetControllerOf(pod)
	if owner == nil {
		return "", "", nil
	}
	if owner.Kind != "ReplicaSet" {
		return owner.Kind, owner.Name, nil
	}
	replicaSet, err := r.client.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
	if err != nil {
		return owner.Kind, owner.Name, err
	}
	parent := metav1.GetControllerOf(replicaSet)
	if parent != nil && strings.TrimSpace(parent.Name) != "" {
		return parent.Kind, parent.Name, nil
	}
	return owner.Kind, owner.Name, nil
}

func firstNonEmptyLogText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
