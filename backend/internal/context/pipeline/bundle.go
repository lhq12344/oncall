package pipeline

import "go_agent/internal/prompt"

type Source string

const (
	SourceSystemRole     Source = "system_role"
	SourceWorkflowPolicy Source = "workflow_policy"
	SourceRuntimeNotice  Source = "runtime_notice"
	SourceSkill          Source = "skill"
	SourceMemory         Source = "memory"
	SourceRAGEvidence    Source = "rag_evidence"
	SourceTranscript     Source = "transcript"
	SourceCurrentInput   Source = "current_input"
)

type Item struct {
	Source   Source
	Name     string
	Content  string
	Priority int
	Trust    string
}

type Bundle struct {
	Snapshot prompt.PromptSnapshot
	Items    []Item
}
