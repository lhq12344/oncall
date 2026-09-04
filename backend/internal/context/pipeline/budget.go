package pipeline

type Budget struct {
	MaxTokens int
	PerSource map[Source]int
}

func DefaultBudget(maxTokens int) Budget {
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	return Budget{MaxTokens: maxTokens, PerSource: map[Source]int{
		SourceSystemRole:     maxTokens,
		SourceWorkflowPolicy: maxTokens / 4,
		SourceRuntimeNotice:  maxTokens / 5,
		SourceSkill:          maxTokens / 4,
		SourceMemory:         maxTokens / 5,
		SourceRAGEvidence:    maxTokens / 3,
		SourceTranscript:     maxTokens / 3,
		SourceCurrentInput:   maxTokens / 3,
	}}
}
