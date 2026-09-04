package toolregistry

import (
	"context"
	"testing"
	"time"
)

func TestRegistryResolveReturnsDescriptors(t *testing.T) {
	registry := NewRegistry(Dependencies{})
	tools, err := registry.Resolve(context.Background(), ToolQuery{Agent: AgentExecution, Capability: Capability("execution.mutation")})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected execution mutation tools")
	}
	for _, item := range tools {
		if item.Descriptor.ID == "" || item.Descriptor.Version == "" || item.Descriptor.Timeout <= 0 || item.Descriptor.Concurrency != 1 {
			t.Fatalf("descriptor missing required metadata: %+v", item.Descriptor)
		}
	}
}

func TestDefaultDescriptorIsDeferredAndBudgeted(t *testing.T) {
	descriptor := DefaultDescriptor("tool")
	if descriptor.Exposure != ToolExposureDeferredGateway || descriptor.Output != OutputInlineRedacted || descriptor.Timeout != 30*time.Second {
		t.Fatalf("unexpected descriptor: %+v", descriptor)
	}
}
