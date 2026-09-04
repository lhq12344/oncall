package kubernetes

import "testing"

func TestNewClientsetFromConfigRejectsNil(t *testing.T) {
	t.Parallel()

	clientset, err := NewClientsetFromConfig(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
	if clientset != nil {
		t.Fatal("expected nil clientset for nil config")
	}
}
