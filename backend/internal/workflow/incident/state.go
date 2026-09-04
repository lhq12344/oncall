package incident

import (
	"time"

	"go_agent/internal/evidence"
	"go_agent/internal/tools/policy"
)

type Terminal string

const (
	TerminalComplete Terminal = "complete"
	TerminalFailed   Terminal = "failed"
	TerminalWaiting  Terminal = "waiting_for_approval"
)

type State struct {
	SchemaVersion     string
	WorkflowVersion   string
	IncidentID        string
	Metadata          map[string]string
	Evidence          []evidence.Evidence
	Diagnosis         Diagnosis
	Plan              Plan
	Approvals         []policy.ApprovalSnapshot
	ExecutionReceipts map[string]Receipt
	Verification      Verification
	RetryCount        int
	ReplanCount       int
	RollbackState     string
	FinalReport       string
	ReviewCaseRefs    []string
	Terminal          Terminal
	UpdatedAt         time.Time
}

type Diagnosis struct {
	RootCause  string
	Confidence float64
	GatePassed bool
	Reasons    []string
}

type Plan struct {
	ID               string
	Revision         int
	SnapshotHash     string
	Steps            []Step
	Risk             string
	Validated        bool
	RequiresApproval bool
}

type Step struct {
	ID             string
	Description    string
	Command        string
	Args           []string
	Mutation       bool
	IdempotencyKey string
}

type Receipt struct {
	Key       string
	Status    string
	CreatedAt time.Time
}

type Verification struct {
	Success bool
	Status  string
	Reason  string
}

func NewState(workflowVersion string) State {
	return State{SchemaVersion: "incident.state/v1", WorkflowVersion: workflowVersion, ExecutionReceipts: map[string]Receipt{}, UpdatedAt: time.Now().UTC()}
}
