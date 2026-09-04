package prompt

import "context"

type SectionSource string

const (
	SourceSystem       SectionSource = "system"
	SourceRole         SectionSource = "role"
	SourceWorkflow     SectionSource = "workflow"
	SourcePolicy       SectionSource = "policy"
	SourceRuntime      SectionSource = "runtime_notice"
	SourceSkill        SectionSource = "skill"
	SourceMemory       SectionSource = "memory"
	SourceRAGEvidence  SectionSource = "rag_evidence"
	SourceTranscript   SectionSource = "transcript"
	SourceCurrentInput SectionSource = "current_input"
	SourceCustom       SectionSource = "custom"
)

type PromptSection struct {
	Name          string        `json:"name"`
	Source        SectionSource `json:"source"`
	Priority      int           `json:"priority"`
	Content       string        `json:"content"`
	TokenEstimate int           `json:"token_estimate"`
	OmittedReason string        `json:"omitted_reason,omitempty"`
	Trust         string        `json:"trust,omitempty"`
}

type SectionProvider interface {
	Sections(context.Context, PromptRequest) ([]PromptSection, error)
}

type StaticSectionProvider struct {
	Items []PromptSection
}

func (p StaticSectionProvider) Sections(context.Context, PromptRequest) ([]PromptSection, error) {
	out := make([]PromptSection, len(p.Items))
	copy(out, p.Items)
	return out, nil
}
