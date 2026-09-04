package improvement

import "fmt"

type ResolutionPath string

const (
	KnowledgeCandidatePath ResolutionPath = "KnowledgeCandidate"
	RetrievalFix           ResolutionPath = "RetrievalFix"
	IntentDataset          ResolutionPath = "IntentDataset"
	PromptEvalDataset      ResolutionPath = "PromptEvalDataset"
	ToolDefect             ResolutionPath = "ToolDefect"
	WorkflowDefect         ResolutionPath = "WorkflowDefect"
	EnvironmentIssue       ResolutionPath = "EnvironmentIssue"
	ClosedAsExpected       ResolutionPath = "ClosedAsExpected"
)

type TriageDecision struct {
	FailureCategory FailureCategory
	ResolutionPath  ResolutionPath
}

func Triage(item Case, decision TriageDecision) (Case, error) {
	if decision.FailureCategory == "" {
		return item, fmt.Errorf("failure category is required")
	}
	if decision.ResolutionPath == KnowledgeCandidatePath && !IsKnowledgeCategory(decision.FailureCategory) {
		return item, fmt.Errorf("only missing/stale knowledge can enter publish pipeline")
	}
	item.FailureCategory = decision.FailureCategory
	item.Status = ReviewTriaged
	return item, nil
}
