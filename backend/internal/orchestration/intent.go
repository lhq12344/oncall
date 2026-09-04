package orchestration

import "strings"

func ClassifyIntent(text string, control ControlDecision) (Intent, float64, string) {
	if control.Kind != ControlNone {
		switch control.Kind {
		case ControlResume:
			return IntentResumeRequest, 1, "deterministic_resume"
		case ControlApproval:
			return IntentApprovalResponse, 1, "deterministic_approval"
		default:
			return IntentWorkflowControl, 1, "deterministic_control"
		}
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" || strings.Contains(lower, "处理一下那个问题") || lower == "fix it" || lower == "看一下" {
		return IntentUnclear, 0.4, "low_information"
	}
	if containsAny(lower, "密钥", "secret", "password", "token", "凭据", "credential") && containsAny(lower, "导出", "dump", "print", "show", "发送") {
		return IntentOutOfScope, 0.95, "credential_request"
	}
	if containsAny(lower, "回滚", "重启", "扩容", "缩容", "delete", "restart", "rollback", "apply", "patch") {
		return IntentChangeRequest, 0.92, "mutation_language"
	}
	if containsAny(lower, "告警", "故障", "5xx", "异常", "incident", "diagnose", "诊断", "排障") {
		return IntentIncidentDiagnosis, 0.9, "incident_language"
	}
	if containsAny(lower, "日志", "指标", "prometheus", "k8s", "kubectl", "namespace", "pod", "查看", "查询", "查一下") {
		return IntentEvidenceQuery, 0.88, "evidence_language"
	}
	if containsAny(lower, "怎么", "如何", "为什么", "知识", "文档", "runbook", "通常") {
		return IntentKnowledgeQuestion, 0.86, "knowledge_language"
	}
	return IntentDialogue, 0.75, "default_dialogue"
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}
