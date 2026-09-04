package pipeline

import (
	"context"
	"strings"
	"testing"

	"go_agent/internal/model"
	"go_agent/internal/prompt"
)

func TestPipelineBuildsFixedSourceOrder(t *testing.T) {
	bundle, err := New(nil).Build(context.Background(), Request{
		Role:         prompt.RoleDialogue,
		ModelProfile: model.Profile{ContextWindow: 1000},
		Budget:       DefaultBudget(1000),
		Items: []Item{
			{Source: SourceCurrentInput, Name: "input", Content: "current"},
			{Source: SourceMemory, Name: "memory", Content: "memory"},
			{Source: SourceRuntimeNotice, Name: "notice", Content: "notice"},
			{Source: SourceWorkflowPolicy, Name: "policy", Content: "policy"},
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	rendered := bundle.Snapshot.Rendered
	policy := strings.Index(rendered, "policy")
	notice := strings.Index(rendered, "notice")
	memory := strings.Index(rendered, "memory")
	current := strings.Index(rendered, "current")
	if !(policy < notice && notice < memory && memory < current) {
		t.Fatalf("unexpected render order: %q", rendered)
	}
}

func TestPipelineUsesModelContextWindowForPromptBudget(t *testing.T) {
	bundle, err := New(nil).Build(context.Background(), Request{
		Role:         prompt.RoleDialogue,
		ModelProfile: model.Profile{ContextWindow: 12},
		Items: []Item{
			{Source: SourceSystemRole, Name: "system", Content: "system"},
			{Source: SourceTranscript, Name: "transcript", Content: "this transcript should be omitted because model budget is tiny"},
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if bundle.Snapshot.MaxTokens != 12 || bundle.Snapshot.OmittedCount == 0 {
		t.Fatalf("expected model-derived budget omission, got %+v", bundle.Snapshot)
	}
}
