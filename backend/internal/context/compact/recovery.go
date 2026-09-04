package compact

import "go_agent/internal/context/notice"

type RecoveryState struct {
	WorkflowState   string
	Approval        string
	ActiveSkills    []string
	RecentArtifacts []string
	RecentTools     []string
}

func RecoveryNotice(state RecoveryState) notice.Notice {
	content := "workflow=" + state.WorkflowState + "\napproval=" + state.Approval
	if len(state.ActiveSkills) > 0 {
		content += "\nskills=" + stringsJoin(state.ActiveSkills)
	}
	if len(state.RecentArtifacts) > 0 {
		content += "\nartifacts=" + stringsJoin(state.RecentArtifacts)
	}
	if len(state.RecentTools) > 0 {
		content += "\ntools=" + stringsJoin(state.RecentTools)
	}
	return notice.Notice{Kind: notice.KindCompaction, Trust: notice.TrustTrustedRuntime, Source: "context.compact", Lifecycle: notice.LifecycleRun, Content: content, Priority: 20, DedupKey: "context.compact.recovery"}
}

func stringsJoin(values []string) string {
	out := ""
	for i, value := range values {
		if i > 0 {
			out += ","
		}
		out += value
	}
	return out
}
