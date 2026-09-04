package prompt

import "strings"

type Budget struct {
	MaxTokens       int
	PerSourceTokens map[SectionSource]int
}

func EstimateTokens(content string) int {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return 0
	}
	// Conservative deterministic estimate; avoids model-specific tokenizer dependency
	// while keeping budget tests stable.
	return (len([]rune(trimmed)) + 3) / 4
}

func defaultBudget(maxTokens int) Budget {
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	return Budget{MaxTokens: maxTokens, PerSourceTokens: map[SectionSource]int{
		SourceSystem:       maxTokens,
		SourceRole:         maxTokens,
		SourceWorkflow:     maxTokens / 4,
		SourcePolicy:       maxTokens / 4,
		SourceRuntime:      maxTokens / 5,
		SourceSkill:        maxTokens / 4,
		SourceMemory:       maxTokens / 5,
		SourceRAGEvidence:  maxTokens / 3,
		SourceTranscript:   maxTokens / 3,
		SourceCurrentInput: maxTokens / 3,
		SourceCustom:       maxTokens / 4,
	}}
}
