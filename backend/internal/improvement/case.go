package improvement

import "time"

type FailureCategory string

const (
	MissingKnowledge        FailureCategory = "missing_knowledge"
	StaleKnowledge          FailureCategory = "stale_knowledge"
	ChunkingFailure         FailureCategory = "chunking_failure"
	QueryRewriteFailure     FailureCategory = "query_rewrite_failure"
	RetrievalFailure        FailureCategory = "retrieval_failure"
	RerankFailure           FailureCategory = "rerank_failure"
	IntentFailure           FailureCategory = "intent_failure"
	PromptFailure           FailureCategory = "prompt_failure"
	ModelFailure            FailureCategory = "model_failure"
	ToolFailure             FailureCategory = "tool_failure"
	WorkflowFailure         FailureCategory = "workflow_failure"
	EnvironmentOrPermission FailureCategory = "environment_or_permission"
	OutOfScope              FailureCategory = "user_request_out_of_scope"
)

type ReviewStatus string

const (
	ReviewNew     ReviewStatus = "new"
	ReviewTriaged ReviewStatus = "triaged"
	ReviewClosed  ReviewStatus = "closed"
)

type Case struct {
	ID                  string
	RunID               string
	TraceID             string
	SessionID           string
	NormalizedQuestion  string
	Risk                string
	RetrievalSnapshotID string
	FailureCategory     FailureCategory
	Priority            float64
	Status              ReviewStatus
	ArtifactRefs        []string
	CreatedAt           time.Time
}

func IsKnowledgeCategory(category FailureCategory) bool {
	return category == MissingKnowledge || category == StaleKnowledge
}
