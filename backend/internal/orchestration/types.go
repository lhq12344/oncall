package orchestration

import "context"

type Intent string

const (
	IntentDialogue          Intent = "dialogue"
	IntentKnowledgeQuestion Intent = "knowledge_question"
	IntentEvidenceQuery     Intent = "evidence_query"
	IntentIncidentDiagnosis Intent = "incident_diagnosis"
	IntentChangeRequest     Intent = "change_request"
	IntentWorkflowControl   Intent = "workflow_control"
	IntentApprovalResponse  Intent = "approval_response"
	IntentResumeRequest     Intent = "resume_request"
	IntentUnclear           Intent = "unclear"
	IntentOutOfScope        Intent = "out_of_scope"
)

type Risk string

const (
	RiskNone               Risk = "none"
	RiskReadOnly           Risk = "read_only"
	RiskWrite              Risk = "write"
	RiskDestructive        Risk = "destructive"
	RiskCredentialOrSecret Risk = "credential_or_secret"
)

type RouteMode string

const (
	RouteDialogue        RouteMode = "dialogue"
	RouteKnowledge       RouteMode = "knowledge_rag"
	RouteEvidence        RouteMode = "evidence_query"
	RouteIncident        RouteMode = "incident_workflow"
	RouteChangePlan      RouteMode = "change_plan"
	RouteWorkflowControl RouteMode = "workflow_control"
	RouteApproval        RouteMode = "approval_response"
	RouteResume          RouteMode = "resume_request"
	RouteClarify         RouteMode = "clarify"
	RouteRefuse          RouteMode = "refuse"
)

type RouteInput struct {
	SessionID string
	Text      string
	Metadata  map[string]string
}

type ControlKind string

const (
	ControlNone     ControlKind = "none"
	ControlSlash    ControlKind = "slash"
	ControlResume   ControlKind = "resume"
	ControlApproval ControlKind = "approval"
	ControlCancel   ControlKind = "cancel"
)

type ControlDecision struct {
	Kind     ControlKind
	Command  string
	Argument string
}

type RouteDecision struct {
	Intent          Intent
	Mode            RouteMode
	Confidence      float64
	Risk            Risk
	ReasonCode      string
	NeedClarify     bool
	ClarifyQuestion string
	Control         ControlDecision
}

type RequestRouter interface {
	Route(context.Context, RouteInput) (RouteDecision, error)
}
