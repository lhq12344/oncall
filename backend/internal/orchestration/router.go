package orchestration

import "context"

type Router struct {
	ClarifyThreshold float64
}

func NewRouter(threshold float64) *Router {
	if threshold <= 0 {
		threshold = 0.85
	}
	return &Router{ClarifyThreshold: threshold}
}

func (r *Router) Route(_ context.Context, input RouteInput) (RouteDecision, error) {
	control := ParseControl(input.Text)
	intent, confidence, reason := ClassifyIntent(input.Text, control)
	risk := ClassifyRisk(input.Text, intent)
	decision := RouteDecision{Intent: intent, Confidence: confidence, Risk: risk, ReasonCode: reason, Control: control}
	switch intent {
	case IntentWorkflowControl:
		decision.Mode = RouteWorkflowControl
	case IntentApprovalResponse:
		decision.Mode = RouteApproval
	case IntentResumeRequest:
		decision.Mode = RouteResume
	case IntentOutOfScope:
		decision.Mode = RouteRefuse
	case IntentUnclear:
		decision.Mode = RouteClarify
		decision.NeedClarify = true
		decision.ClarifyQuestion = "请补充要处理的目标、范围和期望动作。"
	case IntentChangeRequest:
		decision.Mode = RouteChangePlan
	case IntentIncidentDiagnosis:
		decision.Mode = RouteIncident
	case IntentEvidenceQuery:
		decision.Mode = RouteEvidence
	case IntentKnowledgeQuestion:
		decision.Mode = RouteKnowledge
	default:
		if confidence < r.ClarifyThreshold {
			decision.Mode = RouteClarify
			decision.NeedClarify = true
			decision.ClarifyQuestion = "你希望我进行普通对话、知识查询、证据查询，还是故障处置？"
		} else {
			decision.Mode = RouteDialogue
		}
	}
	return decision, nil
}
