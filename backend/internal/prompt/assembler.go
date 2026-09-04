package prompt

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go_agent/internal/model"
)

type PromptRequest struct {
	Role         Role
	ModelProfile model.Profile
	Budget       Budget
	Options      BuildOptions
	Environment  EnvironmentContext
}

type Assembler struct {
	providers []SectionProvider
}

func NewAssembler(providers ...SectionProvider) *Assembler {
	return &Assembler{providers: providers}
}

func DefaultAssembler() *Assembler {
	return NewAssembler(baseProvider{})
}

func (a *Assembler) Assemble(ctx context.Context, req PromptRequest, extraProviders ...SectionProvider) (PromptSnapshot, error) {
	if a == nil {
		a = DefaultAssembler()
	}
	budget := req.Budget
	if budget.MaxTokens == 0 {
		budget = defaultBudget(req.ModelProfile.ContextWindow)
	}
	sections := make([]PromptSection, 0)
	providers := append([]SectionProvider{}, a.providers...)
	providers = append(providers, extraProviders...)
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		items, err := provider.Sections(ctx, req)
		if err != nil {
			return PromptSnapshot{}, err
		}
		sections = append(sections, items...)
	}
	sort.SliceStable(sections, func(i, j int) bool {
		if sections[i].Priority == sections[j].Priority {
			return sections[i].Name < sections[j].Name
		}
		return sections[i].Priority < sections[j].Priority
	})

	usedTotal := 0
	usedBySource := map[SectionSource]int{}
	renderedParts := make([]string, 0, len(sections))
	omitted := 0
	for i := range sections {
		content := strings.TrimSpace(sections[i].Content)
		sections[i].Content = content
		sections[i].TokenEstimate = EstimateTokens(content)
		if content == "" {
			sections[i].OmittedReason = "empty"
			omitted++
			continue
		}
		if limit := budget.PerSourceTokens[sections[i].Source]; limit > 0 && usedBySource[sections[i].Source]+sections[i].TokenEstimate > limit {
			sections[i].OmittedReason = "source_budget_exceeded"
			omitted++
			continue
		}
		if usedTotal+sections[i].TokenEstimate > budget.MaxTokens {
			sections[i].OmittedReason = "total_budget_exceeded"
			omitted++
			continue
		}
		usedTotal += sections[i].TokenEstimate
		usedBySource[sections[i].Source] += sections[i].TokenEstimate
		renderedParts = append(renderedParts, content)
	}

	snapshot := PromptSnapshot{Version: "prompt.snapshot/v1", Role: req.Role, MaxTokens: budget.MaxTokens, Sections: sections, Rendered: strings.Join(renderedParts, "\n\n"), OmittedCount: omitted}
	hash, err := snapshot.ComputeHash()
	if err != nil {
		return PromptSnapshot{}, fmt.Errorf("compute prompt hash: %w", err)
	}
	snapshot.Hash = hash
	return snapshot, nil
}

type baseProvider struct{}

func (baseProvider) Sections(_ context.Context, req PromptRequest) ([]PromptSection, error) {
	sections := []PromptSection{
		fromSection(IdentitySection(), SourceSystem),
		fromSection(SystemSection(), SourceSystem),
		fromSection(TaskExecutionSection(), SourceSystem),
		fromSection(ExecutingActionsSection(), SourcePolicy),
		fromSection(ToolUseSection(), SourcePolicy),
		fromSection(ToneStyleSection(), SourceSystem),
		fromSection(OutputEfficiencySection(), SourceSystem),
		fromSection(EnvironmentSection(req.Environment), SourceRuntime),
	}
	if guidance := strings.TrimSpace(DeferredToolGuidance(req.Role)); guidance != "" {
		sections = append(sections, PromptSection{Name: "DeferredToolGuidance", Source: SourcePolicy, Priority: 45, Content: guidance})
	}
	if role := RoleSection(req.Role); strings.TrimSpace(role.Content) != "" {
		sections = append(sections, fromSection(role, SourceRole))
	}
	if req.Options.CustomInstructions != "" {
		sections = append(sections, PromptSection{Name: "CustomInstructions", Source: SourceCustom, Priority: 80, Content: "# 项目自定义指令\n" + req.Options.CustomInstructions})
	}
	if req.Options.KnowledgeSection != "" {
		sections = append(sections, PromptSection{Name: "Knowledge", Source: SourceRAGEvidence, Priority: 85, Content: "# 知识补充\n" + req.Options.KnowledgeSection, Trust: "untrusted_evidence"})
	}
	if req.Options.MemorySection != "" {
		sections = append(sections, PromptSection{Name: "Memory", Source: SourceMemory, Priority: 95, Content: "# 长期记忆\n" + req.Options.MemorySection})
	}
	return sections, nil
}

func fromSection(section Section, source SectionSource) PromptSection {
	return PromptSection{Name: section.Name, Source: source, Priority: section.Priority, Content: section.Content}
}
