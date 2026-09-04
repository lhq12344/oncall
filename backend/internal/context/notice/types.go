package notice

import "time"

type Kind string

const (
	KindWorkflowState      Kind = "workflow_state"
	KindApprovalRecovery   Kind = "approval_recovery"
	KindCompaction         Kind = "compaction"
	KindMemoryRecall       Kind = "memory_recall"
	KindSkill              Kind = "skill"
	KindMCPServer          Kind = "mcp_server"
	KindHookOutput         Kind = "hook_output"
	KindDegradedCapability Kind = "degraded_capability"
	KindTeamMailbox        Kind = "team_mailbox"
)

type Trust string

const (
	TrustSystem            Trust = "system"
	TrustTrustedRuntime    Trust = "trusted_runtime"
	TrustUntrustedEvidence Trust = "untrusted_evidence"
)

type Lifecycle string

const (
	LifecycleEphemeral Lifecycle = "ephemeral"
	LifecycleRun       Lifecycle = "run"
	LifecycleSession   Lifecycle = "session"
)

type Notice struct {
	Kind      Kind      `json:"kind"`
	Trust     Trust     `json:"trust"`
	Source    string    `json:"source"`
	Lifecycle Lifecycle `json:"lifecycle"`
	Content   string    `json:"content"`
	Priority  int       `json:"priority"`
	DedupKey  string    `json:"dedup_key"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

func (n Notice) Expired(now time.Time) bool {
	return !n.ExpiresAt.IsZero() && !now.Before(n.ExpiresAt)
}
