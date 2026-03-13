package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
)

type planValidatorAgent struct {
	name   string
	desc   string
	logger *zap.Logger
}

func newPlanValidatorAgent(logger *zap.Logger) adk.Agent {
	return &planValidatorAgent{
		name:   "pre_execution_validator",
		desc:   "执行前安全校验节点，检查命令风险并决定是否需要人工确认",
		logger: logger,
	}
}

func (a *planValidatorAgent) Name(_ context.Context) string {
	return a.name
}

func (a *planValidatorAgent) Description(_ context.Context) string {
	return a.desc
}

func (a *planValidatorAgent) Run(_ context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer generator.Close()

		result := a.validate(input)
		payload, err := json.Marshal(result)
		if err != nil {
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("marshal validation result failed: %w", err)})
			return
		}

		generator.Send(assistantEvent(string(payload)))
	}()

	return iterator
}

func (a *planValidatorAgent) validate(input *adk.AgentInput) *PlanValidationResult {
	result := &PlanValidationResult{
		Valid:     true,
		RiskLevel: "low",
	}
	if input == nil || len(input.Messages) == 0 {
		result.Valid = false
		result.Blocked = true
		result.RiskLevel = "high"
		result.Reasons = append(result.Reasons, "缺少执行计划输入")
		return result
	}

	plan, ok := parseOpsExecutionPlan(input.Messages)
	if !ok || plan == nil {
		result.Valid = true
		result.Blocked = false
		result.RiskLevel = "medium"
		result.Reasons = append(result.Reasons, "未检测到 ops_agent 的结构化计划，将由 execution_agent 自行生成计划")
		return result
	}

	result.PlanID = plan.PlanID

	if len(plan.Commands) == 0 {
		result.Valid = false
		result.Blocked = true
		result.RiskLevel = "high"
		result.Reasons = append(result.Reasons, "执行计划 commands 为空")
		return result
	}

	var (
		riskScore int
		blocked   bool
		confirm   bool
	)

	for _, step := range plan.Commands {
		command := strings.TrimSpace(step.Command)
		if command == "" {
			blocked = true
			result.Reasons = append(result.Reasons, fmt.Sprintf("步骤 %d 缺少 command", step.Step))
			continue
		}

		switch {
		case matchAny(command, absoluteForbiddenPatterns()...):
			blocked = true
			result.UnsafeCommands = append(result.UnsafeCommands, command)
			result.Reasons = append(result.Reasons, fmt.Sprintf("步骤 %d 命中禁止命令", step.Step))
			riskScore += 4
		case matchAny(command, highRiskPatterns()...):
			confirm = true
			result.ReviewCommands = append(result.ReviewCommands, command)
			result.Reasons = append(result.Reasons, fmt.Sprintf("步骤 %d 为高风险命令，需人工确认", step.Step))
			riskScore += 2
		default:
			if !matchAny(command, readOnlyPatterns()...) {
				riskScore++
			}
		}
	}

	result.Blocked = blocked
	result.RequiresConfirmation = !blocked && confirm
	result.Valid = !blocked
	switch {
	case blocked || riskScore >= 4:
		result.RiskLevel = "high"
	case riskScore >= 2:
		result.RiskLevel = "medium"
	default:
		result.RiskLevel = "low"
	}

	if len(result.Reasons) == 0 {
		result.Reasons = append(result.Reasons, "计划通过自动校验")
	}

	if a.logger != nil {
		a.logger.Info("plan validation completed",
			zap.String("plan_id", result.PlanID),
			zap.Bool("blocked", result.Blocked),
			zap.Bool("requires_confirmation", result.RequiresConfirmation),
			zap.String("risk_level", result.RiskLevel))
	}

	return result
}

type executionGateAgent struct {
	name   string
	desc   string
	logger *zap.Logger
}

func newExecutionGateAgent(logger *zap.Logger) adk.ResumableAgent {
	return &executionGateAgent{
		name:   "execution_gate",
		desc:   "根据执行结果决定中断、重规划或结束循环",
		logger: logger,
	}
}

func (a *executionGateAgent) Name(_ context.Context) string {
	return a.name
}

func (a *executionGateAgent) Description(_ context.Context) string {
	return a.desc
}

func (a *executionGateAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer generator.Close()

		var messages []adk.Message
		if input != nil {
			messages = input.Messages
		}

		plan, _ := parseOpsExecutionPlan(messages)
		validation, hasValidation := parseValidationResult(messages)
		execStatus := detectExecutionStatus(messages)
		execResult, hasExecResult := parseExecutionResult(messages)
		executedSteps := parseExecutedStepCount(execResult)
		executionStatusText := parseExecutionStatusText(execResult)
		manualPlan := parseExecutionManualPlan(execResult)

		if hasValidation && validation.Blocked {
			reason := formatReasons(validation.Reasons)
			fallback := ""
			planID := ""
			if plan != nil {
				fallback = plan.FallbackPlan
				planID = plan.PlanID
			}
			message := fmt.Sprintf("执行前校验阻断：%s。请先人工修正计划后再继续。", reason)
			generator.Send(interruptEvent(ctx, &IncidentInterruptInfo{
				Type:         "validator_blocked",
				Reason:       reason,
				PlanID:       planID,
				FallbackPlan: fallback,
			}, message))
			return
		}

		if hasValidation && validation.RequiresConfirmation {
			reason := formatReasons(validation.Reasons)
			fallback := ""
			planID := ""
			if plan != nil {
				fallback = plan.FallbackPlan
				planID = plan.PlanID
			}
			message := fmt.Sprintf("计划包含高风险命令，需要你确认后再执行。风险说明：%s。", reason)
			generator.Send(interruptEvent(ctx, &IncidentInterruptInfo{
				Type:         "approval_required",
				Reason:       reason,
				PlanID:       planID,
				FallbackPlan: fallback,
			}, message))
			return
		}

		if executionStatusText == "manual_required" {
			fallback := strings.TrimSpace(manualPlan)
			planID := ""
			if plan != nil {
				planID = plan.PlanID
				if fallback == "" {
					fallback = plan.FallbackPlan
				}
			}
			if fallback == "" {
				fallback = "请根据当前告警信息执行人工排查并反馈结果。"
			}
			message := fmt.Sprintf("自动工具未能完成修复，请按计划人工执行后回复确认。建议人工方案：%s", fallback)
			generator.Send(interruptEvent(ctx, &IncidentInterruptInfo{
				Type:         "manual_required",
				Reason:       strings.TrimSpace(execStatus.RawMessageHint),
				PlanID:       planID,
				FallbackPlan: fallback,
			}, message))
			return
		}

		if execStatus.Found && execStatus.Success {
			// 防止 execution_agent 在未调用工具时直接输出 success。
			// 若存在结构化执行结果但 executed_steps 为空，视为未实际执行，回到 ops 重新生成可执行计划。
			if hasExecResult && executedSteps <= 0 {
				if a.logger != nil {
					a.logger.Warn("execution success without executed steps, fallback to replan",
						zap.String("execution_status", executionStatusText))
				}
				generator.Send(assistantEvent("未检测到执行步骤明细（executed_steps 为空），将回到 ops_agent 重新生成可执行计划。"))
				return
			}
			generator.Send(breakLoopEvent(a.name, "执行步骤已成功，停止重规划循环，进入策略复盘。"))
			return
		}

		if execStatus.ToolCannotFix {
			fallback := ""
			planID := ""
			if plan != nil {
				fallback = plan.FallbackPlan
				planID = plan.PlanID
			}
			message := fmt.Sprintf("自动工具未能完成修复，请按计划人工执行后回复确认。建议人工方案：%s", fallback)
			generator.Send(interruptEvent(ctx, &IncidentInterruptInfo{
				Type:         "manual_required",
				Reason:       strings.TrimSpace(execStatus.RawMessageHint),
				PlanID:       planID,
				FallbackPlan: fallback,
			}, message))
			return
		}

		generator.Send(assistantEvent("执行未达成预期，回到 ops_agent 重新生成计划。"))
	}()

	return iterator
}

func (a *executionGateAgent) Resume(ctx context.Context, info *adk.ResumeInfo, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer generator.Close()

		if info == nil || !info.WasInterrupted {
			generator.Send(assistantEvent("未检测到可恢复的中断状态，继续重规划。"))
			return
		}

		if !info.IsResumeTarget {
			generator.Send(interruptEvent(ctx, &IncidentInterruptInfo{
				Type:   "awaiting_user_confirmation",
				Reason: "仍在等待用户确认",
			}, "当前仍等待用户确认，请回复“确认已修复”或“继续重试”。"))
			return
		}

		approved, resolved, comment := parseResumeDecision(info.ResumeData)
		if resolved {
			if strings.TrimSpace(comment) == "" {
				comment = "用户已确认"
			}
			generator.Send(breakLoopEvent(a.name, fmt.Sprintf("收到确认：%s。结束执行循环，进入 strategy_agent 生成最终报告。", comment)))
			return
		}

		if approved {
			if strings.TrimSpace(comment) == "" {
				comment = "用户批准继续执行"
			}
			generator.Send(assistantEvent(fmt.Sprintf("收到批准：%s。继续执行自动修复流程。", comment)))
			return
		}

		if strings.TrimSpace(comment) == "" {
			comment = "用户要求继续重试"
		}
		generator.Send(assistantEvent(fmt.Sprintf("收到反馈：%s。将重新进入 ops_agent 生成新计划。", comment)))
	}()

	return iterator
}

type finalReportAgent struct {
	name   string
	desc   string
	logger *zap.Logger
}

func newFinalReportAgent(logger *zap.Logger) adk.Agent {
	return &finalReportAgent{
		name:   "final_reporter",
		desc:   "输出面向前端的运维处置总结",
		logger: logger,
	}
}

func (a *finalReportAgent) Name(_ context.Context) string {
	return a.name
}

func (a *finalReportAgent) Description(_ context.Context) string {
	return a.desc
}

func (a *finalReportAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer generator.Close()
		state := getIncidentState(ctx)
		summary := buildFinalOpsSummary(state)
		if a.logger != nil {
			a.logger.Info("incident final summary generated",
				zap.Int("summary_len", len([]rune(summary))))
		}
		if path, err := persistFinalOpsReport(ctx, state, summary); err != nil {
			if a.logger != nil {
				a.logger.Warn("failed to persist incident final report", zap.Error(err))
			}
		} else if a.logger != nil {
			a.logger.Info("incident final report persisted", zap.String("path", path))
		}
		generator.Send(assistantEvent(summary))
	}()

	return iterator
}

// buildFinalOpsSummary 生成最终运维总结文本。
// 输入：流程状态快照。
// 输出：可直接返回给前端的总结文本。
func buildFinalOpsSummary(state *IncidentState) string {
	if state == nil {
		return "运维流程已结束，但未读取到结构化状态。请结合工具日志确认最终处理结果。"
	}

	finalStatus := strings.TrimSpace(state.FinalStatus)
	if finalStatus == "" {
		switch {
		case state.ExecutionSuccess:
			finalStatus = "resolved"
		case strings.TrimSpace(state.ExecutionStatus) != "":
			finalStatus = "partially_resolved"
		default:
			finalStatus = "unresolved"
		}
	}

	resolvedText := "待确认"
	switch strings.ToLower(strings.TrimSpace(finalStatus)) {
	case "resolved", "success", "fixed", "completed":
		resolvedText = "是"
	case "partially_resolved":
		resolvedText = "部分解决"
	default:
		resolvedText = "否"
	}

	lines := []string{
		"## 运维技术报告",
		fmt.Sprintf("- 生成时间：%s", time.Now().Format("2006-01-02 15:04:05")),
		fmt.Sprintf("- 最终状态：%s", finalStatus),
		fmt.Sprintf("- 是否已解决：%s", resolvedText),
	}

	lines = append(lines, "", "### 1) 问题发现")
	if rootCause := strings.TrimSpace(state.RootCause); rootCause != "" {
		lines = append(lines, fmt.Sprintf("- 根因判断：%s", rootCause))
	}
	if impact := strings.TrimSpace(state.Impact); impact != "" {
		lines = append(lines, fmt.Sprintf("- 影响范围：%s", impact))
	}
	if summary := strings.TrimSpace(state.ObservationSummary); summary != "" {
		lines = append(lines, fmt.Sprintf("- 观测摘要：%s", summary))
	}
	for _, errText := range collectIncidentErrors(state) {
		lines = append(lines, fmt.Sprintf("- 错误发现：%s", errText))
	}

	lines = append(lines, "", "### 2) 执行日志汇总")
	if len(state.ExecutionLogs) == 0 {
		lines = append(lines, "- 未采集到执行日志。")
	} else {
		for idx, logLine := range state.ExecutionLogs {
			lines = append(lines, fmt.Sprintf("%d. %s", idx+1, strings.TrimSpace(logLine)))
		}
		if len(state.ExecutionLogs) >= maxIncidentExecutionLogs {
			lines = append(lines, fmt.Sprintf("- 注：仅保留最近 %d 条执行日志。", maxIncidentExecutionLogs))
		}
	}

	lines = append(lines, "", "### 3) 解决方法")
	methods := collectIncidentMethods(state)
	if len(methods) == 0 {
		lines = append(lines, "- 暂未形成可执行的自动修复方法，请人工介入。")
	} else {
		for _, method := range methods {
			lines = append(lines, fmt.Sprintf("- %s", method))
		}
	}

	lines = append(lines, "", "### 4) 结论")
	if report := strings.TrimSpace(state.FinalReport); report != "" {
		lines = append(lines, fmt.Sprintf("- 复盘结论：%s", report))
	}
	if fallback := strings.TrimSpace(state.ExecutionFallback); fallback != "" && !strings.EqualFold(finalStatus, "resolved") {
		lines = append(lines, fmt.Sprintf("- 后续建议：%s", fallback))
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// collectIncidentErrors 汇总错误发现信息。
// 输入：state（流程状态快照）。
// 输出：去重后的错误列表。
func collectIncidentErrors(state *IncidentState) []string {
	if state == nil {
		return nil
	}
	out := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	appendOnce := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if _, ok := seen[text]; ok {
			return
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}

	for _, errText := range state.ObservationErrors {
		appendOnce(errText)
	}
	keywords := []string{"error", "failed", "异常", "失败", "超时", "not found", "panic", "blocked"}
	for _, logLine := range state.ExecutionLogs {
		lower := strings.ToLower(logLine)
		for _, keyword := range keywords {
			if strings.Contains(lower, keyword) {
				appendOnce(clipText(logLine, 260))
				break
			}
		}
	}
	return out
}

// collectIncidentMethods 汇总当前处置方法。
// 输入：state（流程状态快照）。
// 输出：方法列表（去重）。
func collectIncidentMethods(state *IncidentState) []string {
	if state == nil {
		return nil
	}
	methods := make([]string, 0, 6)
	seen := make(map[string]struct{}, 6)
	appendMethod := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if _, ok := seen[text]; ok {
			return
		}
		seen[text] = struct{}{}
		methods = append(methods, text)
	}

	if summary := strings.TrimSpace(state.PlanSummary); summary != "" {
		appendMethod("执行计划：" + summary)
	}
	if state.ExecutionStepCount > 0 {
		appendMethod(fmt.Sprintf("自动执行步骤：%d 步", state.ExecutionStepCount))
	}
	if reason := strings.TrimSpace(state.ExecutionReason); reason != "" {
		appendMethod("执行说明：" + reason)
	}
	if report := strings.TrimSpace(state.FinalReport); report != "" {
		appendMethod("策略复盘：" + report)
	}
	if fallback := strings.TrimSpace(state.ExecutionFallback); fallback != "" {
		appendMethod("人工兜底方案：" + fallback)
	}
	return methods
}

// persistFinalOpsReport 将最终技术报告落盘到 logs/ops_reports。
// 输入：ctx（工作流上下文）、state（流程状态）、report（报告正文）。
// 输出：落盘文件路径。
func persistFinalOpsReport(ctx context.Context, state *IncidentState, report string) (string, error) {
	report = strings.TrimSpace(report)
	if report == "" {
		return "", fmt.Errorf("empty report")
	}

	sessionID := "aiops"
	if value, ok := adk.GetSessionValue(ctx, "session_id"); ok {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			sessionID = strings.TrimSpace(text)
		}
	}
	status := ""
	if state != nil {
		status = strings.TrimSpace(state.FinalStatus)
	}
	if status == "" {
		status = "unknown"
	}

	dir := filepath.Join("logs", "ops_reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir failed: %w", err)
	}

	now := time.Now()
	fileName := fmt.Sprintf("%s_%s_%s.md",
		sanitizeReportFileToken(sessionID),
		sanitizeReportFileToken(status),
		now.Format("20060102_150405"))
	path := filepath.Join(dir, fileName)

	header := []string{
		"---",
		fmt.Sprintf("session_id: %s", sessionID),
		fmt.Sprintf("final_status: %s", status),
		fmt.Sprintf("generated_at: %s", now.Format(time.RFC3339)),
		"---",
		"",
	}
	content := strings.Join(header, "\n") + report + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write report failed: %w", err)
	}
	return path, nil
}

// sanitizeReportFileToken 清理文件名中的非法字符。
// 输入：text（原始文本）。
// 输出：可用于文件名的安全文本。
func sanitizeReportFileToken(text string) string {
	text = strings.TrimSpace(strings.ToLower(text))
	if text == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		" ", "_",
		"\t", "_",
		"\n", "_",
	)
	text = replacer.Replace(text)
	text = regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(text, "_")
	text = strings.Trim(text, "_")
	if text == "" {
		return "unknown"
	}
	return text
}

func absoluteForbiddenPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)\brm\s+-rf\s+/`),
		regexp.MustCompile(`(?i)\bmkfs\b`),
		regexp.MustCompile(`(?i)\bdd\s+if=`),
		regexp.MustCompile(`(?i)\bshutdown\b|\breboot\b`),
		regexp.MustCompile(`:\(\)\s*\{\s*:\|:&\s*\};:`),
	}
}

func highRiskPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bsystemctl\s+(restart|stop)\b`),
		regexp.MustCompile(`(?i)\bdocker\s+(stop|rm|restart)\b`),
		regexp.MustCompile(`(?i)\bkubectl\s+(delete|scale|rollout\s+restart)\b`),
		regexp.MustCompile(`(?i)\bkill\s+-?\d*\b`),
		regexp.MustCompile(`(?i)\biptables\b`),
	}
}

func readOnlyPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*kubectl\s+(get|describe|logs)\b`),
		regexp.MustCompile(`(?i)^\s*(ls|df|du|cat|tail|grep|ps|top|free|uptime)\b`),
		regexp.MustCompile(`(?i)^\s*curl\s+-I\b`),
	}
}

func matchAny(command string, rules ...*regexp.Regexp) bool {
	for _, rule := range rules {
		if rule.MatchString(command) {
			return true
		}
	}
	return false
}
