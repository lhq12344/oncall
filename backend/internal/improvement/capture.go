package improvement

import "context"

type CaptureReason string

const (
	ReasonWeakEvidence      CaptureReason = "weak_evidence"
	ReasonEvidenceGateFail  CaptureReason = "evidence_gate_fail"
	ReasonAnswerGateFail    CaptureReason = "answer_gate_fail"
	ReasonUserDownvote      CaptureReason = "user_downvote"
	ReasonRetrievalDegraded CaptureReason = "retrieval_degraded"
	ReasonWorkflowFailure   CaptureReason = "workflow_failure"
	ReasonToolDegraded      CaptureReason = "tool_degraded"
	ReasonHighValueSuccess  CaptureReason = "high_value_success"
)

type ReviewCase struct {
	ID                  string
	RunID               string
	TraceID             string
	SessionID           string
	RetrievalSnapshotID string
	Reason              CaptureReason
	Status              string
}

type Store interface {
	Save(context.Context, ReviewCase) error
}

func Capture(ctx context.Context, store Store, item ReviewCase) error {
	if store == nil {
		return nil
	}
	if item.Status == "" {
		item.Status = "new"
	}
	return store.Save(ctx, item)
}
