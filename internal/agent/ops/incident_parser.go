package ops

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
)

func parseRCAReport(messages []adk.Message) (*RCAReport, bool) {
	_, raw, ok := findLatestJSONObject(messages, "root_cause", "confidence")
	if !ok {
		return nil, false
	}

	var report RCAReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return nil, false
	}
	return &report, true
}

// parseRemediationProposal 解析 ops_agent 产出的修复提案。
// 输入：消息列表。
// 输出：结构化修复提案与是否解析成功。
func parseRemediationProposal(messages []adk.Message) (*RemediationProposal, bool) {
	_, raw, ok := findLatestJSONObject(messages, "actions")
	if ok {
		var proposal RemediationProposal
		if err := json.Unmarshal([]byte(raw), &proposal); err == nil {
			return &proposal, true
		}
	}

	obj, raw, ok := findLatestJSONObject(messages, "commands")
	if !ok {
		return nil, false
	}

	type legacyCommand struct {
		Step     int    `json:"step"`
		Goal     string `json:"goal"`
		Command  string `json:"command"`
		Expected string `json:"expected"`
		Rollback string `json:"rollback"`
		ReadOnly bool   `json:"read_only"`
	}
	type legacyPlan struct {
		PlanID       string          `json:"plan_id"`
		Summary      string          `json:"summary"`
		RootCause    string          `json:"root_cause"`
		TargetNode   string          `json:"target_node"`
		RiskLevel    string          `json:"risk_level"`
		Commands     []legacyCommand `json:"commands"`
		FallbackPlan string          `json:"fallback_plan"`
	}

	var legacy legacyPlan
	if err := json.Unmarshal([]byte(raw), &legacy); err == nil {
		proposal := &RemediationProposal{
			ProposalID:   strings.TrimSpace(legacy.PlanID),
			Summary:      strings.TrimSpace(legacy.Summary),
			RootCause:    strings.TrimSpace(legacy.RootCause),
			TargetNode:   strings.TrimSpace(legacy.TargetNode),
			RiskLevel:    strings.TrimSpace(legacy.RiskLevel),
			FallbackPlan: strings.TrimSpace(legacy.FallbackPlan),
			Actions:      make([]RemediationAction, 0, len(legacy.Commands)),
		}
		for _, command := range legacy.Commands {
			proposal.Actions = append(proposal.Actions, RemediationAction{
				Step:            command.Step,
				Goal:            strings.TrimSpace(command.Goal),
				CommandHint:     strings.TrimSpace(command.Command),
				SuccessCriteria: strings.TrimSpace(command.Expected),
				RollbackHint:    strings.TrimSpace(command.Rollback),
				ReadOnly:        command.ReadOnly,
			})
		}
		return proposal, true
	}

	proposal := &RemediationProposal{
		ProposalID:   stringFromMap(obj, "proposal_id"),
		Summary:      stringFromMap(obj, "summary"),
		RootCause:    stringFromMap(obj, "root_cause"),
		TargetNode:   stringFromMap(obj, "target_node"),
		RiskLevel:    stringFromMap(obj, "risk_level"),
		FallbackPlan: stringFromMap(obj, "fallback_plan"),
	}
	return proposal, true
}

func parseValidationResult(messages []adk.Message) (*PlanValidationResult, bool) {
	_, raw, ok := findLatestJSONObject(messages, "blocked", "risk_level")
	if !ok {
		return nil, false
	}

	var result PlanValidationResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, false
	}

	return &result, true
}

// parseGeneratedExecutionPlan 解析 execution_agent 生成的结构化执行计划。
// 输入：消息列表。
// 输出：执行计划及是否解析成功。
func parseGeneratedExecutionPlan(messages []adk.Message) (*GeneratedExecutionPlan, bool) {
	_, raw, ok := findLatestJSONObject(messages, "steps", "total_steps")
	if !ok {
		return nil, false
	}

	var plan GeneratedExecutionPlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return nil, false
	}
	return &plan, true
}

// parseStepValidationResult 解析 validate_result 工具输出。
// 输入：消息列表。
// 输出：步骤校验结果及是否解析成功。
func parseStepValidationResult(messages []adk.Message) (*StepValidationResult, bool) {
	_, raw, ok := findLatestJSONObject(messages, "valid", "should_stop")
	if !ok {
		return nil, false
	}

	var result StepValidationResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, false
	}
	return &result, true
}

func parseExecutionResult(messages []adk.Message) (map[string]any, bool) {
	obj, _, ok := findLatestJSONObject(messages, "execution_status")
	if !ok {
		return nil, false
	}
	return obj, true
}

// parseExecutedStepCount 提取执行结果中的已执行步骤数量。
// 输入：execution_agent 输出的结构化结果对象。
// 输出：执行步骤数；无法识别时返回 0。
func parseExecutedStepCount(result map[string]any) int {
	if result == nil {
		return 0
	}
	value, exists := result["executed_steps"]
	if !exists || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case []any:
		return len(typed)
	case []map[string]any:
		return len(typed)
	}
	return 0
}

// parseExecutionStatusText 提取 execution_status 字段文本。
// 输入：execution_agent 输出的结构化结果对象。
// 输出：标准化后的状态字符串（小写）；缺失时返回空字符串。
func parseExecutionStatusText(result map[string]any) string {
	if result == nil {
		return ""
	}
	value, exists := result["execution_status"]
	if !exists || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.ToLower(strings.TrimSpace(text))
	}
	return ""
}

// parseExecutionManualPlan 提取人工兜底执行计划文本。
// 输入：execution_agent 输出的结构化结果对象。
// 输出：manual_plan 文本；缺失时返回空字符串。
func parseExecutionManualPlan(result map[string]any) string {
	if result == nil {
		return ""
	}
	value, exists := result["manual_plan"]
	if !exists || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

type executionDiagnosticInsight struct {
	OverallHealth        string
	Findings             []string
	Issues               []string
	Recommendations      []string
	ActionableIssueCount int
	Summary              string
}

// parseExecutionDiagnosticInsight 解析 execution_agent 产出的诊断摘要与新增问题。
// 输入：execution_agent 结构化结果对象。
// 输出：诊断健康度、执行发现、问题摘要、建议以及可动作问题计数。
func parseExecutionDiagnosticInsight(result map[string]any) executionDiagnosticInsight {
	insight := executionDiagnosticInsight{
		Findings:        make([]string, 0, 4),
		Issues:          make([]string, 0, 4),
		Recommendations: make([]string, 0, 4),
	}
	if result == nil {
		return insight
	}

	addUnique := func(target *[]string, text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		for _, existing := range *target {
			if existing == text {
				return
			}
		}
		*target = append(*target, text)
	}

	if steps, ok := result["executed_steps"].([]any); ok {
		for _, item := range steps {
			step, ok := item.(map[string]any)
			if !ok {
				continue
			}
			finding := strings.TrimSpace(stringFromMap(step, "findings"))
			if isExecutionAbnormalFinding(finding) {
				addUnique(&insight.Findings, finding)
			}
		}
	}

	diagnostic, ok := result["diagnostic_summary"].(map[string]any)
	if !ok {
		insight.Summary = firstNonEmptyText(joinExecutionIssueSummaries(insight.Findings, 2))
		return insight
	}

	insight.OverallHealth = strings.TrimSpace(stringFromMap(diagnostic, "overall_health"))
	if issues, ok := diagnostic["critical_issues"].([]any); ok {
		for _, item := range issues {
			issueObj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			severity := strings.ToLower(strings.TrimSpace(stringFromMap(issueObj, "severity")))
			component := strings.TrimSpace(stringFromMap(issueObj, "component"))
			issueText := strings.TrimSpace(stringFromMap(issueObj, "issue"))
			impact := strings.TrimSpace(stringFromMap(issueObj, "impact"))
			recommendation := strings.TrimSpace(stringFromMap(issueObj, "recommendation"))

			issueSummary := formatExecutionIssueSummary(severity, component, issueText, impact)
			addUnique(&insight.Issues, issueSummary)

			if recommendation != "" {
				if component != "" {
					addUnique(&insight.Recommendations, fmt.Sprintf("%s：%s", component, recommendation))
				} else {
					addUnique(&insight.Recommendations, recommendation)
				}
			}
			if isActionableExecutionIssue(severity, component, issueText) {
				insight.ActionableIssueCount++
			}
		}
	}

	if insight.ActionableIssueCount == 0 && len(insight.Issues) == 0 && len(insight.Findings) > 0 {
		insight.Summary = joinExecutionIssueSummaries(insight.Findings, 2)
	} else {
		insight.Summary = joinExecutionIssueSummaries(insight.Issues, 2)
	}
	return insight
}

// isExecutionAbnormalFinding 判断执行发现是否包含异常信号。
// 输入：执行步骤 findings 文本。
// 输出：true 表示值得继续写入图状态和最终报告。
func isExecutionAbnormalFinding(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	keywords := []string{
		"异常", "warning", "warn", "error", "backoff", "imagepull", "notready",
		"失败", "无法", "重启", "告警", "偏移", "不可达", "timeout", "超时",
	}
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

// formatExecutionIssueSummary 生成适合报告展示的问题摘要。
// 输入：严重级别、组件、问题描述、影响描述。
// 输出：单行问题摘要。
func formatExecutionIssueSummary(severity, component, issue, impact string) string {
	parts := make([]string, 0, 3)
	if component != "" && issue != "" {
		parts = append(parts, fmt.Sprintf("%s：%s", component, issue))
	} else if issue != "" {
		parts = append(parts, issue)
	} else if component != "" {
		parts = append(parts, component)
	}
	if impact != "" {
		parts = append(parts, "影响："+impact)
	}
	text := strings.TrimSpace(strings.Join(parts, "；"))
	switch severity {
	case "critical", "high":
		return "高风险问题：" + text
	case "medium":
		return "中风险问题：" + text
	case "low":
		return "低风险问题：" + text
	default:
		return text
	}
}

// isActionableExecutionIssue 判断诊断问题是否需要回到 ops_agent 继续规划。
// 输入：严重级别、组件、问题描述。
// 输出：true 表示当前问题不应直接视为执行成功闭环。
func isActionableExecutionIssue(severity, component, issue string) bool {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical", "high", "medium":
		return true
	}

	lowerIssue := strings.ToLower(strings.TrimSpace(issue))
	lowerComponent := strings.ToLower(strings.TrimSpace(component))
	if lowerIssue == "" {
		return false
	}
	if strings.Contains(lowerIssue, "crashloopbackoff") ||
		strings.Contains(lowerIssue, "imagepullbackoff") ||
		strings.Contains(lowerIssue, "notready") ||
		strings.Contains(lowerIssue, "unavailable") ||
		strings.Contains(lowerIssue, "无法连接") ||
		strings.Contains(lowerIssue, "could not be established") {
		return !strings.Contains(lowerComponent, "test")
	}
	return false
}

// joinExecutionIssueSummaries 将问题摘要拼接为简短句子。
// 输入：issues 为问题数组，limit 为最大条数。
// 输出：拼接后的摘要。
func joinExecutionIssueSummaries(issues []string, limit int) string {
	if len(issues) == 0 {
		return ""
	}
	if limit <= 0 || limit > len(issues) {
		limit = len(issues)
	}
	parts := make([]string, 0, limit)
	for _, issue := range issues[:limit] {
		text := strings.TrimSpace(issue)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "；")
}

func parseStrategyReport(messages []adk.Message) (map[string]any, bool) {
	obj, _, ok := findLatestJSONObject(messages, "final_status")
	if !ok {
		return nil, false
	}
	return obj, true
}

func detectExecutionStatus(messages []adk.Message) ExecutionStatus {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg == nil {
			continue
		}

		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}

		lower := strings.ToLower(content)

		// 优先读取结构化字段
		if obj, _, ok := findJSONObjectInText(content, "success"); ok {
			if success, ok := boolFromAny(obj["success"]); ok {
				return ExecutionStatus{
					Found:          true,
					Success:        success,
					ToolCannotFix:  containsToolFailureKeywords(lower),
					RawMessageHint: content,
				}
			}
		}

		// 退化到关键词判断
		if strings.Contains(lower, "execution_status") || strings.Contains(lower, "执行") ||
			strings.Contains(lower, "failed") || strings.Contains(lower, "success") {
			status := ExecutionStatus{
				Found:          true,
				Success:        !containsFailureKeywords(lower),
				ToolCannotFix:  containsToolFailureKeywords(lower),
				RawMessageHint: content,
			}
			if containsFailureKeywords(lower) {
				status.Success = false
			}
			if strings.Contains(lower, "success") || strings.Contains(lower, "成功") {
				status.Success = true
			}
			return status
		}
	}

	return ExecutionStatus{}
}

func containsFailureKeywords(lower string) bool {
	return strings.Contains(lower, "failed") ||
		strings.Contains(lower, "error") ||
		strings.Contains(lower, "执行失败") ||
		strings.Contains(lower, "\"success\":false")
}

func containsToolFailureKeywords(lower string) bool {
	return strings.Contains(lower, "command not in whitelist") ||
		strings.Contains(lower, "unsafe arguments") ||
		strings.Contains(lower, "需要人工") ||
		strings.Contains(lower, "manual") ||
		strings.Contains(lower, "tool cannot") ||
		strings.Contains(lower, "无法自动修复")
}

func findLatestJSONObject(messages []adk.Message, requiredKeys ...string) (map[string]any, string, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg == nil || strings.TrimSpace(msg.Content) == "" {
			continue
		}

		obj, raw, ok := findJSONObjectInText(msg.Content, requiredKeys...)
		if ok {
			return obj, raw, true
		}
	}
	return nil, "", false
}

func findJSONObjectInText(content string, requiredKeys ...string) (map[string]any, string, bool) {
	candidates := extractJSONCandidates(content)
	for i := len(candidates) - 1; i >= 0; i-- {
		raw := strings.TrimSpace(candidates[i])
		if raw == "" {
			continue
		}

		obj := map[string]any{}
		if err := json.Unmarshal([]byte(raw), &obj); err != nil {
			continue
		}

		if hasAllKeys(obj, requiredKeys...) {
			return obj, raw, true
		}
	}
	return nil, "", false
}

func extractJSONCandidates(text string) []string {
	var (
		candidates []string
		start      = -1
		depth      = 0
		inString   = false
		escaped    = false
	)

	for index, r := range text {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inString:
			escaped = true
		case r == '"':
			inString = !inString
		case !inString && r == '{':
			if depth == 0 {
				start = index
			}
			depth++
		case !inString && r == '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 && index >= start {
				candidates = append(candidates, text[start:index+1])
				start = -1
			}
		}
	}

	return candidates
}

func hasAllKeys(obj map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := obj[key]; !ok {
			return false
		}
	}
	return true
}

func stringFromMap(obj map[string]any, key string) string {
	value, ok := obj[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
