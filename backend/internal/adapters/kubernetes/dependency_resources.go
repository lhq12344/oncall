package kubernetes

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DependencyResources struct {
	Namespace string
	Workloads []DiscoveredWorkload
	Services  []DiscoveredService
	Warnings  []string
}

type DiscoveredWorkload struct {
	Namespace string
	Name      string
	Kind      string
	Labels    map[string]string
	Replicas  *int32
}

type DiscoveredService struct {
	Namespace string
	Name      string
	Selector  map[string]string
}

func (t *Monitor) DiscoverDependencyResources(ctx context.Context, namespace string) (*DependencyResources, error) {
	if !t.Available() {
		return nil, fmt.Errorf("k8s client unavailable for auto_discover")
	}

	queryNamespace := strings.TrimSpace(namespace)
	if queryNamespace == "" {
		queryNamespace = "infra"
	}
	if queryNamespace == "*" || strings.EqualFold(queryNamespace, "all") {
		queryNamespace = metav1.NamespaceAll
	}

	resources := &DependencyResources{Namespace: namespace}

	deployments, err := t.client.AppsV1().Deployments(queryNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		resources.Warnings = append(resources.Warnings, "list deployments failed: "+err.Error())
	} else {
		for _, deploy := range deployments.Items {
			resources.Workloads = append(resources.Workloads, DiscoveredWorkload{
				Namespace: deploy.Namespace,
				Name:      deploy.Name,
				Kind:      "deployment",
				Labels:    cloneStringMap(deploy.Spec.Template.Labels),
				Replicas:  cloneInt32Ptr(deploy.Spec.Replicas),
			})
		}
	}

	statefulSets, err := t.client.AppsV1().StatefulSets(queryNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		resources.Warnings = append(resources.Warnings, "list statefulsets failed: "+err.Error())
	} else {
		for _, statefulSet := range statefulSets.Items {
			resources.Workloads = append(resources.Workloads, DiscoveredWorkload{
				Namespace: statefulSet.Namespace,
				Name:      statefulSet.Name,
				Kind:      "statefulset",
				Labels:    cloneStringMap(statefulSet.Spec.Template.Labels),
				Replicas:  cloneInt32Ptr(statefulSet.Spec.Replicas),
			})
		}
	}

	services, err := t.client.CoreV1().Services(queryNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		resources.Warnings = append(resources.Warnings, "list services failed: "+err.Error())
	} else {
		for _, service := range services.Items {
			resources.Services = append(resources.Services, DiscoveredService{
				Namespace: service.Namespace,
				Name:      service.Name,
				Selector:  cloneStringMap(service.Spec.Selector),
			})
		}
	}

	return resources, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func cloneInt32Ptr(value *int32) *int32 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
