package policy

type Effect string

const (
	Allow Effect = "allow"
	Ask   Effect = "ask"
	Deny  Effect = "deny"
)

type Decision struct {
	Effect           Effect
	ReasonCode       string
	MatchedRule      string
	PolicyVersion    string
	NormalizedTarget string
	ApprovalScope    string
}
