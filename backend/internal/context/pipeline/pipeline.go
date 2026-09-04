package pipeline

import (
	"context"
	"fmt"

	"go_agent/internal/model"
	"go_agent/internal/prompt"
)

type Request struct {
	Role         prompt.Role
	ModelProfile model.Profile
	Items        []Item
	Budget       Budget
}

type Pipeline struct {
	assembler *prompt.Assembler
}

func New(assembler *prompt.Assembler) *Pipeline {
	if assembler == nil {
		assembler = prompt.NewAssembler()
	}
	return &Pipeline{assembler: assembler}
}

func (p *Pipeline) Build(ctx context.Context, req Request) (Bundle, error) {
	if p == nil {
		p = New(nil)
	}
	sections := make([]prompt.PromptSection, 0, len(req.Items))
	for _, item := range req.Items {
		sections = append(sections, prompt.PromptSection{Name: item.Name, Source: toPromptSource(item.Source), Priority: sourceOrder(item.Source)*100 + item.Priority, Content: item.Content, Trust: item.Trust})
	}
	budget := prompt.Budget{MaxTokens: req.Budget.MaxTokens, PerSourceTokens: map[prompt.SectionSource]int{}}
	for source, limit := range req.Budget.PerSource {
		budget.PerSourceTokens[toPromptSource(source)] = limit
	}
	snapshot, err := p.assembler.Assemble(ctx, prompt.PromptRequest{Role: req.Role, ModelProfile: req.ModelProfile, Budget: budget}, prompt.StaticSectionProvider{Items: sections})
	if err != nil {
		return Bundle{}, fmt.Errorf("assemble context prompt: %w", err)
	}
	return Bundle{Snapshot: snapshot, Items: req.Items}, nil
}

func sourceOrder(source Source) int {
	switch source {
	case SourceSystemRole:
		return 1
	case SourceWorkflowPolicy:
		return 2
	case SourceRuntimeNotice:
		return 3
	case SourceSkill:
		return 4
	case SourceMemory:
		return 5
	case SourceRAGEvidence:
		return 6
	case SourceTranscript:
		return 7
	case SourceCurrentInput:
		return 8
	default:
		return 99
	}
}

func toPromptSource(source Source) prompt.SectionSource {
	switch source {
	case SourceSystemRole:
		return prompt.SourceSystem
	case SourceWorkflowPolicy:
		return prompt.SourcePolicy
	case SourceRuntimeNotice:
		return prompt.SourceRuntime
	case SourceSkill:
		return prompt.SourceSkill
	case SourceMemory:
		return prompt.SourceMemory
	case SourceRAGEvidence:
		return prompt.SourceRAGEvidence
	case SourceTranscript:
		return prompt.SourceTranscript
	case SourceCurrentInput:
		return prompt.SourceCurrentInput
	default:
		return prompt.SourceCustom
	}
}
