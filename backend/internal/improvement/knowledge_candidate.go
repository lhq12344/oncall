package improvement

import "time"

type KnowledgeStatus string
type KnowledgeType string

const (
	KDraft          KnowledgeStatus = "draft"
	KReviewed       KnowledgeStatus = "reviewed"
	KValidated      KnowledgeStatus = "validated"
	KIndexedStaging KnowledgeStatus = "indexed_staging"
	KEvaluated      KnowledgeStatus = "evaluated"
	KCanary         KnowledgeStatus = "canary"
	KPublished      KnowledgeStatus = "published"
	KSuperseded     KnowledgeStatus = "superseded"
	KExpired        KnowledgeStatus = "expired"
)

const (
	KnowledgeFact                  KnowledgeType = "fact"
	KnowledgeRunbook               KnowledgeType = "runbook"
	KnowledgeTroubleshooting       KnowledgeType = "troubleshooting"
	KnowledgeIncidentCase          KnowledgeType = "incident_case"
	KnowledgeOperationalConstraint KnowledgeType = "operational_constraint"
	KnowledgeQA                    KnowledgeType = "qa"
)

type KnowledgeCandidate struct {
	ID                  string
	CaseID              string
	RunID               string
	TraceID             string
	RetrievalSnapshotID string
	Type                KnowledgeType
	Status              KnowledgeStatus
	Owner               string
	Source              string
	Scope               string
	Version             string
	Validity            string
	RollbackVersion     string
	Content             string
	UpdatedAt           time.Time
}
