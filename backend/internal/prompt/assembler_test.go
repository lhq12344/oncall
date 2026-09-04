package prompt

import (
	"context"
	"testing"

	"go_agent/internal/model"
)

func TestAssemblerProducesStableSnapshotAndHash(t *testing.T) {
	req := PromptRequest{
		Role:         RoleDialogue,
		ModelProfile: model.Profile{ID: "m", ContextWindow: 1000},
		Environment:  EnvironmentContext{WorkDir: "/repo", OS: "test", Arch: "amd64", Shell: "sh", Date: "2026-09-03"},
		Options:      BuildOptions{KnowledgeSection: "external runbook"},
	}
	first, err := DefaultAssembler().Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("Assemble first: %v", err)
	}
	second, err := DefaultAssembler().Assemble(context.Background(), req)
	if err != nil {
		t.Fatalf("Assemble second: %v", err)
	}
	if first.Hash != second.Hash || first.Rendered != second.Rendered {
		t.Fatalf("snapshot not stable:\nfirst=%+v\nsecond=%+v", first, second)
	}
	foundUntrusted := false
	for _, section := range first.Sections {
		if section.Source == SourceRAGEvidence && section.Trust == "untrusted_evidence" {
			foundUntrusted = true
		}
		if section.TokenEstimate == 0 && section.OmittedReason == "" {
			t.Fatalf("section missing token estimate or omitted reason: %+v", section)
		}
	}
	if !foundUntrusted {
		t.Fatalf("RAG evidence should be marked untrusted: %+v", first.Sections)
	}
}

func TestAssemblerOmitsBySourceBudget(t *testing.T) {
	snapshot, err := NewAssembler(StaticSectionProvider{Items: []PromptSection{
		{Name: "system", Source: SourceSystem, Priority: 1, Content: "system"},
		{Name: "large", Source: SourceMemory, Priority: 2, Content: "this memory section is intentionally too large for the source budget"},
	}}).Assemble(context.Background(), PromptRequest{ModelProfile: model.Profile{ContextWindow: 100}, Budget: Budget{MaxTokens: 100, PerSourceTokens: map[SectionSource]int{SourceSystem: 100, SourceMemory: 1}}})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if snapshot.OmittedCount != 1 {
		t.Fatalf("omitted=%d, want 1", snapshot.OmittedCount)
	}
	if snapshot.Sections[1].OmittedReason != "source_budget_exceeded" {
		t.Fatalf("unexpected omitted reason: %+v", snapshot.Sections[1])
	}
}
