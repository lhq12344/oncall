package memory

import "strings"

type Policy struct{}

func (Policy) AllowCandidate(candidate Candidate) bool {
	if strings.TrimSpace(candidate.Content) == "" || strings.TrimSpace(candidate.Provenance) == "" {
		return false
	}
	if candidate.Confidence < 0.7 {
		return false
	}
	lower := strings.ToLower(candidate.Content)
	if strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "token") || strings.Contains(lower, "未验证") || strings.Contains(lower, "猜测") || strings.Contains(lower, "临时执行") || strings.Contains(lower, "未完成计划") {
		return false
	}
	return candidate.Kind == KindUserPreference || candidate.Kind == KindConfirmedEnvironment || candidate.Kind == KindVerifiedOpsConclusion || candidate.Kind == KindRecurringConstraint
}
