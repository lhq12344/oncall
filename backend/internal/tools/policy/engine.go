package policy

import "context"

type Engine struct {
	Version string
}

func NewEngine(version string) *Engine {
	if version == "" {
		version = "tool.policy/v1"
	}
	return &Engine{Version: version}
}

func (e *Engine) Decide(_ context.Context, req Request) Decision {
	if e == nil {
		e = NewEngine("")
	}
	req.NormalizedTarget = firstNonEmpty(req.NormalizedTarget, normalizeTarget(req.Args))
	decision := Decision{PolicyVersion: e.Version, NormalizedTarget: req.NormalizedTarget}
	if req.Risk == RiskDestructive {
		decision.Effect = Ask
		decision.ReasonCode = "destructive_requires_approval"
		decision.ApprovalScope = "one_shot"
		return decision
	}
	if req.Risk == RiskHigh || req.Capability == "execution.mutation" {
		expected, err := BindApproval(req)
		if err != nil {
			decision.Effect = Deny
			decision.ReasonCode = "invalid_args"
			return decision
		}
		if req.Approved == nil || !req.Approved.SameApprovalTarget(expected) {
			decision.Effect = Ask
			decision.ReasonCode = "mutation_requires_matching_approval"
			decision.ApprovalScope = expected.Scope
			return decision
		}
	}
	decision.Effect = Allow
	decision.ReasonCode = "allowed_by_policy"
	return decision
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
