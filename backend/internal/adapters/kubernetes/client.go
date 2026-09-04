package kubernetes

import (
	"fmt"
	"strings"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func NewClientset(kubeconfig string) (*clientset.Clientset, error) {
	config, err := ConfigFromPathOrCluster(kubeconfig)
	if err != nil {
		return nil, err
	}
	return NewClientsetFromConfig(config)
}

func ConfigFromPathOrCluster(kubeconfig string) (*rest.Config, error) {
	var firstErr error
	if strings.TrimSpace(kubeconfig) != "" {
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err == nil && config != nil {
			return config, nil
		}
		firstErr = err
	}

	config, err := rest.InClusterConfig()
	if err == nil && config != nil {
		return config, nil
	}
	if firstErr != nil {
		return nil, fmt.Errorf("load kubeconfig failed: %w; in-cluster config failed: %v", firstErr, err)
	}
	return nil, fmt.Errorf("in-cluster config failed: %w", err)
}

func NewClientsetFromConfig(config *rest.Config) (*clientset.Clientset, error) {
	if config == nil {
		return nil, fmt.Errorf("kubernetes config is nil")
	}
	client, err := clientset.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes clientset: %w", err)
	}
	return client, nil
}
