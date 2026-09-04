package orchestration

import "strings"

func ParseControl(text string) ControlDecision {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ControlDecision{Kind: ControlNone}
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(trimmed, "/") {
		fields := strings.Fields(trimmed)
		cmd := strings.TrimPrefix(fields[0], "/")
		arg := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
		kind := ControlSlash
		switch cmd {
		case "resume":
			kind = ControlResume
		case "cancel", "stop":
			kind = ControlCancel
		case "approve", "approved", "reject", "deny":
			kind = ControlApproval
		}
		return ControlDecision{Kind: kind, Command: cmd, Argument: arg}
	}
	if strings.Contains(lower, "checkpoint") && (strings.Contains(lower, "approve") || strings.Contains(lower, "approved") || strings.Contains(lower, "reject") || strings.Contains(lower, "deny")) {
		return ControlDecision{Kind: ControlApproval, Command: "approval", Argument: trimmed}
	}
	return ControlDecision{Kind: ControlNone}
}
