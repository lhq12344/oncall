package orchestration

import "strings"

func ClassifyRisk(text string, intent Intent) Risk {
	lower := strings.ToLower(text)
	if intent == IntentOutOfScope || containsAny(lower, "secret", "password", "token", "密钥", "凭据") {
		return RiskCredentialOrSecret
	}
	if containsAny(lower, "delete", "删除", "drop", "destroy") {
		return RiskDestructive
	}
	if intent == IntentChangeRequest || containsAny(lower, "回滚", "重启", "扩容", "缩容", "restart", "rollback", "apply", "patch") {
		return RiskWrite
	}
	if intent == IntentEvidenceQuery || intent == IntentKnowledgeQuestion || intent == IntentIncidentDiagnosis {
		return RiskReadOnly
	}
	return RiskNone
}
