package skills

import "go_agent/internal/context/notice"

func ActivationNotice(skill Skill) notice.Notice {
	return notice.Notice{Kind: notice.KindSkill, Trust: notice.TrustUntrustedEvidence, Source: "skill:" + skill.Metadata.Name, Lifecycle: notice.LifecycleSession, Content: skill.Content, Priority: 40, DedupKey: "skill:" + skill.Metadata.Name}
}
