package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	aiindexer "go_agent/internal/ai/indexer"
	"go_agent/utility/common"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

const maxRepeatedIssueRetries = 3

var (
	repeatedIssueSpacePattern = regexp.MustCompile(`\s+`)
	repeatedIssueNoisePattern = regexp.MustCompile(`\b(plan_[a-z0-9_-]+|step[_\s-]*\d+|\d{4}-\d{2}-\d{2}[t\s]\d{2}:\d{2}:\d{2}[^\s]*|\d+)\b`)
)

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

type repeatedIssueDecision struct {
	Count   int
	Limit   int
	Reached bool
	Reason  string
}

// trackRepeatedIssue 记录同类问题的重试次数并返回阈值判定。
// 输入：ctx（工作流上下文）、issueType（问题类别）、reason（问题描述）。
// 输出：计数结果（当前次数、上限、是否达到上限、归一化原因）。
func trackRepeatedIssue(ctx context.Context, issueType, reason string) repeatedIssueDecision {
	key := normalizeRepeatedIssueKey(issueType, reason)
	if key == "" {
		return repeatedIssueDecision{Limit: maxRepeatedIssueRetries}
	}

	state := getIncidentState(ctx)
	state.RepeatedIssueRetryLimit = maxRepeatedIssueRetries
	if key == strings.TrimSpace(state.RepeatedIssueKey) {
		state.RepeatedIssueRetryCount++
	} else {
		state.RepeatedIssueKey = key
		state.RepeatedIssueReason = clipText(strings.TrimSpace(reason), 300)
		state.RepeatedIssueRetryCount = 1
		state.RepeatedIssueEscalated = false
	}
	if state.RepeatedIssueRetryCount >= maxRepeatedIssueRetries {
		state.RepeatedIssueEscalated = true
	}
	state.UpdatedAt = time.Now().Format(time.RFC3339)
	setIncidentState(ctx, state)

	return repeatedIssueDecision{
		Count:   state.RepeatedIssueRetryCount,
		Limit:   maxRepeatedIssueRetries,
		Reached: state.RepeatedIssueEscalated,
		Reason:  firstNonEmptyText(state.RepeatedIssueReason, strings.TrimSpace(reason)),
	}
}

// normalizeRepeatedIssueKey 归一化重复问题签名，避免时间戳/步骤号导致同问题无法聚合。
// 输入：issueType（问题类别）、reason（问题描述）。
// 输出：可用于重复计数的稳定 key。
func normalizeRepeatedIssueKey(issueType, reason string) string {
	raw := strings.ToLower(strings.TrimSpace(issueType + " " + reason))
	if raw == "" {
		return ""
	}
	raw = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ", "`", "", "\"", "", "'", "").Replace(raw)
	raw = repeatedIssueNoisePattern.ReplaceAllString(raw, "#")
	raw = repeatedIssueSpacePattern.ReplaceAllString(raw, " ")
	return clipText(strings.TrimSpace(raw), 220)
}

// clearRepeatedIssueState 清理重复问题计数状态。
// 输入：ctx（工作流上下文）。
// 输出：无。
func clearRepeatedIssueState(ctx context.Context) {
	state := getIncidentState(ctx)
	if strings.TrimSpace(state.RepeatedIssueKey) == "" &&
		strings.TrimSpace(state.RepeatedIssueReason) == "" &&
		state.RepeatedIssueRetryCount == 0 &&
		!state.RepeatedIssueEscalated {
		return
	}
	state.RepeatedIssueKey = ""
	state.RepeatedIssueReason = ""
	state.RepeatedIssueRetryCount = 0
	state.RepeatedIssueRetryLimit = maxRepeatedIssueRetries
	state.RepeatedIssueEscalated = false
	state.UpdatedAt = time.Now().Format(time.RFC3339)
	setIncidentState(ctx, state)
}

// buildFallbackPlan 统一提取人工兜底方案，优先使用 execution 输出，其次 proposal。
// 输入：manualPlan、proposal、executionPlan、state。
// 输出：可展示给用户的人工方案。
func buildFallbackPlan(manualPlan string, proposal *RemediationProposal, executionPlan *GeneratedExecutionPlan, state *IncidentState) string {
	fallback := strings.TrimSpace(manualPlan)
	if fallback == "" && proposal != nil {
		fallback = strings.TrimSpace(proposal.FallbackPlan)
	}
	if fallback == "" && executionPlan != nil {
		fallback = strings.TrimSpace(executionPlan.Description)
	}
	if fallback == "" && state != nil {
		fallback = strings.TrimSpace(firstNonEmptyText(
			state.ExecutionFallback,
			state.RemediationProposalFallback,
			state.FallbackPlan,
		))
	}
	if fallback == "" {
		fallback = "请根据技术报告中的执行计划和失败原因执行人工处置，并在完成后回填处理结果。"
	}
	return clipText(fallback, 800)
}

// buildRepeatedIssueStopEvent 在重复重试达到阈值时写入状态并结束自动循环。
// 输入：ctx、reason（重复问题摘要）、fallback（人工方案）。
// 输出：用于结束循环并进入最终报告的 AgentEvent。
func (a *executionGateAgent) buildRepeatedIssueStopEvent(ctx context.Context, reason, fallback string) *adk.AgentEvent {
	state := getIncidentState(ctx)
	retryCount := state.RepeatedIssueRetryCount
	if retryCount <= 0 {
		retryCount = maxRepeatedIssueRetries
	}
	stopReason := clipText(firstNonEmptyText(reason, state.RepeatedIssueReason), 600)
	fallback = buildFallbackPlan(fallback, nil, nil, state)

	state.ExecutionStatus = "manual_required"
	state.ExecutionSuccess = false
	state.ExecutionReason = clipText(fmt.Sprintf("同一问题重复重试 %d 次仍未解决：%s", retryCount, stopReason), 600)
	state.ExecutionFallback = fallback
	state.RepeatedIssueEscalated = true
	state.RepeatedIssueRetryLimit = maxRepeatedIssueRetries
	if state.RepeatedIssueRetryCount <= 0 {
		state.RepeatedIssueRetryCount = retryCount
	}
	if strings.TrimSpace(state.FinalStatus) == "" {
		state.FinalStatus = "unresolved"
	}
	appendIncidentExecutionLog(state, fmt.Sprintf("[execution_gate] 同一问题重试达到上限，停止自动执行：%s", stopReason))
	state.UpdatedAt = time.Now().Format(time.RFC3339)
	setIncidentState(ctx, state)

	message := fmt.Sprintf("同一问题已连续重试 %d 次仍未解决，停止自动调用并转人工处理。问题：%s。建议人工方案：%s", retryCount, stopReason, clipText(fallback, 220))
	return breakLoopEvent(a.name, message)
}

func (a *executionGateAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer generator.Close()

		var messages []adk.Message
		if input != nil {
			messages = input.Messages
		}

		proposal, _ := parseRemediationProposal(messages)
		executionPlan, _ := parseGeneratedExecutionPlan(messages)
		validation, hasValidation := parseValidationResult(messages)
		stepValidation, hasStepValidation := parseStepValidationResult(messages)
		execStatus := detectExecutionStatus(messages)
		execResult, hasExecResult := parseExecutionResult(messages)
		executedSteps := parseExecutedStepCount(execResult)
		executionStatusText := parseExecutionStatusText(execResult)
		manualPlan := parseExecutionManualPlan(execResult)
		diagnostic := parseExecutionDiagnosticInsight(execResult)

		if hasValidation && validation.Blocked {
			reason := formatReasons(validation.Reasons)
			fallback := ""
			planID := ""
			if proposal != nil {
				fallback = proposal.FallbackPlan
				planID = proposal.ProposalID
			}
			if executionPlan != nil && strings.TrimSpace(planID) == "" {
				planID = executionPlan.PlanID
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
			if proposal != nil {
				fallback = proposal.FallbackPlan
				planID = proposal.ProposalID
			}
			if executionPlan != nil && strings.TrimSpace(planID) == "" {
				planID = executionPlan.PlanID
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

		if hasStepValidation && stepValidation != nil && stepValidation.ShouldStop {
			reason := strings.TrimSpace(firstNonEmptyText(stepValidation.StopReason, stepValidation.Message))
			if strings.EqualFold(strings.TrimSpace(stepValidation.StopAction), "replan") {
				runtimeSummary := strings.TrimSpace(firstNonEmptyText(stepValidation.RuntimeSummary, stepValidation.MismatchReason, stepValidation.Actual))
				retryDecision := trackRepeatedIssue(ctx, "step_validation_replan", firstNonEmptyText(reason, runtimeSummary))
				if retryDecision.Reached {
					fallback := buildFallbackPlan(manualPlan, proposal, executionPlan, getIncidentState(ctx))
					generator.Send(a.buildRepeatedIssueStopEvent(ctx, retryDecision.Reason, fallback))
					return
				}
				message := fmt.Sprintf("执行校验发现当前现场与上游故障假设不一致，停止当前计划并回到 ops_agent 重新规划。原因：%s。", reason)
				if runtimeSummary != "" {
					message += " 最新运行时观测：" + clipText(runtimeSummary, 220)
				}
				generator.Send(assistantEvent(message))
				return
			}
			fallback := strings.TrimSpace(manualPlan)
			planID := ""
			if executionPlan != nil {
				planID = strings.TrimSpace(executionPlan.PlanID)
				if fallback == "" {
					fallback = strings.TrimSpace(executionPlan.Description)
				}
			}
			if planID == "" && proposal != nil {
				planID = strings.TrimSpace(proposal.ProposalID)
			}
			if fallback == "" && proposal != nil {
				fallback = strings.TrimSpace(proposal.FallbackPlan)
			}
			if fallback == "" {
				fallback = "请根据技术报告中的执行计划逐步人工执行，并在完成后反馈结果。"
			}
			message := fmt.Sprintf("连续步骤校验失败，已停止当前自动执行并转人工处理。原因：%s。建议人工方案：%s", reason, fallback)
			generator.Send(interruptEvent(ctx, &IncidentInterruptInfo{
				Type:         "manual_required",
				Reason:       reason,
				PlanID:       planID,
				FallbackPlan: fallback,
			}, message))
			return
		}

		if executionStatusText == "manual_required" {
			clearRepeatedIssueState(ctx)
			fallback := strings.TrimSpace(manualPlan)
			planID := ""
			if executionPlan != nil {
				planID = executionPlan.PlanID
				if fallback == "" {
					fallback = strings.TrimSpace(executionPlan.Description)
				}
			}
			if planID == "" && proposal != nil {
				planID = proposal.ProposalID
				if fallback == "" {
					fallback = proposal.FallbackPlan
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
			if hasExecResult && diagnostic.ActionableIssueCount > 0 {
				summary := firstNonEmptyText(
					joinExecutionIssueSummaries(diagnostic.Issues, 2),
					diagnostic.Summary,
				)
				retryDecision := trackRepeatedIssue(ctx, "execution_actionable_issue", summary)
				if retryDecision.Reached {
					fallback := buildFallbackPlan(manualPlan, proposal, executionPlan, getIncidentState(ctx))
					generator.Send(a.buildRepeatedIssueStopEvent(ctx, retryDecision.Reason, fallback))
					return
				}
				message := "执行阶段已完成既定检查，但仍发现未闭环问题，将回到 ops_agent 继续生成修复动作。"
				if summary != "" {
					message = fmt.Sprintf("执行阶段已完成既定检查，但仍发现未闭环问题：%s。将回到 ops_agent 继续生成修复动作。", clipText(summary, 220))
				}
				generator.Send(assistantEvent(message))
				return
			}
			// 防止 execution_agent 在未调用工具时直接输出 success。
			// 若存在结构化执行结果但 executed_steps 为空，视为未实际执行，回到 ops 重新生成可执行计划。
			if hasExecResult && executedSteps <= 0 {
				retryDecision := trackRepeatedIssue(ctx, "execution_empty_steps", "execution_agent 返回 success 但未产生 executed_steps")
				if retryDecision.Reached {
					fallback := buildFallbackPlan(manualPlan, proposal, executionPlan, getIncidentState(ctx))
					generator.Send(a.buildRepeatedIssueStopEvent(ctx, retryDecision.Reason, fallback))
					return
				}
				if a.logger != nil {
					a.logger.Warn("execution success without executed steps, fallback to replan",
						zap.String("execution_status", executionStatusText))
				}
				generator.Send(assistantEvent("未检测到执行步骤明细（executed_steps 为空），将回到 ops_agent 重新生成可执行计划。"))
				return
			}
			clearRepeatedIssueState(ctx)
			generator.Send(breakLoopEvent(a.name, "执行步骤已成功，停止重规划循环，进入策略复盘。"))
			return
		}

		if execStatus.ToolCannotFix {
			clearRepeatedIssueState(ctx)
			fallback := ""
			planID := ""
			if proposal != nil {
				fallback = proposal.FallbackPlan
				planID = proposal.ProposalID
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

		replanReason := strings.TrimSpace(firstNonEmptyText(execStatus.RawMessageHint, "执行未达成预期"))
		retryDecision := trackRepeatedIssue(ctx, "execution_replan", replanReason)
		if retryDecision.Reached {
			fallback := buildFallbackPlan(manualPlan, proposal, executionPlan, getIncidentState(ctx))
			generator.Send(a.buildRepeatedIssueStopEvent(ctx, retryDecision.Reason, fallback))
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
		state := getIncidentState(ctx)
		if resolved {
			if strings.TrimSpace(comment) == "" {
				comment = "用户已确认"
			}
			generator.Send(breakLoopEvent(a.name, fmt.Sprintf("收到确认：%s。结束执行循环，进入 strategy_agent 生成最终报告。", comment)))
			return
		}
		if state.RepeatedIssueEscalated {
			reason := firstNonEmptyText(state.RepeatedIssueReason, state.ExecutionReason, "同一问题重复重试后仍未解决")
			fallback := buildFallbackPlan("", nil, nil, state)
			generator.Send(breakLoopEvent(a.name, fmt.Sprintf("同一问题已达到自动重试上限（%d 次），不再继续自动执行。问题：%s。请按人工方案处理：%s", maxIntValue(state.RepeatedIssueRetryCount, maxRepeatedIssueRetries), clipText(reason, 220), clipText(fallback, 220))))
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

// maxIntValue 返回两个整数中的较大值。
// 输入：left、right。
// 输出：较大值。
func maxIntValue(left, right int) int {
	if left > right {
		return left
	}
	return right
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
		path, err := persistFinalOpsReport(ctx, state, summary)
		if err != nil {
			if a.logger != nil {
				a.logger.Warn("failed to persist incident final report", zap.Error(err))
			}
		} else {
			if a.logger != nil {
				a.logger.Info("incident final report persisted", zap.String("path", path))
			}
			if err := archiveFinalOpsReport(ctx, state, summary, path, a.logger); err != nil && a.logger != nil {
				a.logger.Warn("failed to archive incident final report to knowledge store", zap.Error(err))
			}
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
		case state.ExecutionSuccess && len(state.ExecutionIssues) == 0:
			finalStatus = "resolved"
		case state.ExecutionSuccess || strings.TrimSpace(state.ExecutionStatus) != "":
			finalStatus = "partially_resolved"
		default:
			finalStatus = "unresolved"
		}
	}
	if state.RepeatedIssueEscalated {
		finalStatus = "unresolved"
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

	facts := collectReadableIncidentFacts(state)
	lines := []string{
		"## 运维技术报告",
		fmt.Sprintf("- 生成时间：%s", time.Now().Format("2006-01-02 15:04:05")),
		fmt.Sprintf("- 最终状态：%s", finalStatus),
		fmt.Sprintf("- 是否已解决：%s", resolvedText),
	}
	if target := strings.TrimSpace(firstNonEmptyText(state.TargetNode, state.RootCause)); target != "" {
		lines = append(lines, fmt.Sprintf("- 关注对象：%s", humanizeIncidentTarget(target)))
	}

	lines = append(lines, "", "### 1) 发现的问题")
	problemLines := buildReadableProblemSection(state, facts)
	if len(problemLines) == 0 {
		lines = append(lines, "- 未提取到明确的问题摘要，请结合 RCA 与执行结果继续确认。")
	} else {
		for _, item := range problemLines {
			lines = append(lines, fmt.Sprintf("- %s", item))
		}
	}

	lines = append(lines, "", "### 2) 问题原因")
	causeLines := buildReadableCauseSection(state, facts)
	if len(causeLines) == 0 {
		lines = append(lines, "- 当前只能给出初步判断，仍需结合更多监控、日志和人工排查信息确认。")
	} else {
		for _, item := range causeLines {
			lines = append(lines, fmt.Sprintf("- %s", item))
		}
	}

	lines = append(lines, "", "### 3) 解决方法")
	methodLines := buildReadableMethodSection(state, facts)
	if len(methodLines) == 0 {
		lines = append(lines, "- 暂未形成明确的自动化处置方案，建议人工继续排查。")
	} else {
		for _, item := range methodLines {
			lines = append(lines, fmt.Sprintf("- %s", item))
		}
	}

	lines = append(lines, "", "### 4) 处理流程")
	processLines := buildReadableProcessSection(state)
	if len(processLines) == 0 {
		lines = append(lines, "- 本次流程未形成完整的自动化处置轨迹。")
	} else {
		for _, item := range processLines {
			lines = append(lines, fmt.Sprintf("- %s", item))
		}
	}

	lines = append(lines, "", "### 5) 处理结果")
	resultLines := buildReadableResultSection(state, facts, finalStatus)
	if len(resultLines) == 0 {
		lines = append(lines, "- 当前没有足够证据输出处理结果，请结合执行状态进一步确认。")
	} else {
		for _, item := range resultLines {
			lines = append(lines, fmt.Sprintf("- %s", item))
		}
	}

	lines = append(lines, "", "### 6) 结论与建议")
	conclusionLines := buildReadableConclusionSection(state, facts, finalStatus)
	if len(conclusionLines) == 0 {
		lines = append(lines, "- 本次处置已结束，但结论摘要缺失，请补充人工复盘。")
	} else {
		for _, item := range conclusionLines {
			lines = append(lines, fmt.Sprintf("- %s", item))
		}
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// incidentReadableFacts 汇总从观测、执行日志中抽取出的可读事实。
// 输入：无。
// 输出：供最终技术报告拼装的结构化事实。
type incidentReadableFacts struct {
	ObservationNotes    []string
	RestartedPods       []string
	PrometheusNoData    bool
	PrometheusDownCount int
	PrometheusDropped   int
	ElasticsearchNoHits bool
	AnomalyMessages     []string
	NodeReboot          bool
	NodeNotReady        bool
	GracefulTermination bool
	OOMDetected         bool
	PanicDetected       bool
}

// collectReadableIncidentFacts 从 IncidentState 和执行日志中抽取摘要事实。
// 输入：state（流程状态快照）。
// 输出：用于报告渲染的可读事实集合。
func collectReadableIncidentFacts(state *IncidentState) incidentReadableFacts {
	facts := incidentReadableFacts{}
	observationSeen := make(map[string]struct{}, 6)
	restartedSeen := make(map[string]struct{}, 8)
	anomalySeen := make(map[string]struct{}, 6)

	addObservation := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if _, ok := observationSeen[text]; ok {
			return
		}
		observationSeen[text] = struct{}{}
		facts.ObservationNotes = append(facts.ObservationNotes, text)
	}
	addRestartedPod := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := restartedSeen[name]; ok {
			return
		}
		restartedSeen[name] = struct{}{}
		facts.RestartedPods = append(facts.RestartedPods, name)
	}
	addAnomaly := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if _, ok := anomalySeen[text]; ok {
			return
		}
		anomalySeen[text] = struct{}{}
		facts.AnomalyMessages = append(facts.AnomalyMessages, text)
	}

	for _, sentence := range splitReadableSentences(state.ObservationSummary) {
		switch {
		case strings.Contains(sentence, "Prometheus") && strings.Contains(sentence, "0 条时间序列"):
			facts.PrometheusNoData = true
			addObservation("Prometheus 历史范围查询未返回时间序列，监控链路存在数据缺口")
		case strings.Contains(sentence, "Elasticsearch") && strings.Contains(sentence, "0 条日志"):
			facts.ElasticsearchNoHits = true
			addObservation("Elasticsearch 未检索到与本次事件直接相关的错误日志")
		case strings.Contains(sentence, "K8s") && strings.Contains(sentence, "Pod"):
			addObservation("已完成 Kubernetes Pod 状态快照采集")
		default:
			addObservation(clipText(sentence, 120))
		}
	}
	for _, sentence := range splitReadableSentences(state.RuntimeObservationSummary) {
		addObservation("最新运行时校验：" + clipText(sentence, 120))
	}

	for _, logLine := range state.ExecutionLogs {
		lower := strings.ToLower(logLine)
		if strings.Contains(lower, "rebooted") {
			facts.NodeReboot = true
		}
		if strings.Contains(lower, "nodenotready") {
			facts.NodeNotReady = true
		}
		if strings.Contains(lower, "signal terminated") || strings.Contains(lower, "graceful shutdown") {
			facts.GracefulTermination = true
		}
		if strings.Contains(lower, "oom") {
			facts.OOMDetected = true
		}
		if strings.Contains(lower, "panic") {
			facts.PanicDetected = true
		}
		if strings.Contains(lower, "connection timeout") {
			addAnomaly("检测到连接超时类异常")
		}

		obj, _, ok := findJSONObjectInText(logLine)
		if !ok || obj == nil {
			continue
		}

		switch {
		case hasAllKeys(obj, "namespace", "pods", "count"):
			pods, _ := obj["pods"].([]any)
			for _, item := range pods {
				pod, ok := item.(map[string]any)
				if !ok {
					continue
				}
				restartTotal := 0
				containers, _ := pod["containers"].([]any)
				for _, containerItem := range containers {
					container, ok := containerItem.(map[string]any)
					if !ok {
						continue
					}
					restartTotal += toInt(container["restart_count"])
				}
				if restartTotal > 0 {
					addRestartedPod(stringFromMap(pod, "name"))
				}
			}
		case hasAllKeys(obj, "type", "series", "count"):
			if strings.EqualFold(stringFromMap(obj, "type"), "range") && toInt(obj["count"]) == 0 {
				facts.PrometheusNoData = true
			}
		case hasAllKeys(obj, "active_targets", "dropped_targets"):
			facts.PrometheusDropped = toInt(obj["dropped_targets"])
			if healthSummary, ok := obj["health_summary"].(map[string]any); ok {
				facts.PrometheusDownCount = toInt(healthSummary["down"])
			}
		case hasAllKeys(obj, "index", "total_hits"):
			if toInt(obj["total_hits"]) == 0 {
				facts.ElasticsearchNoHits = true
			}
		case hasAllKeys(obj, "anomalies"):
			anomalies, _ := obj["anomalies"].([]any)
			for _, item := range anomalies {
				anomaly, ok := item.(map[string]any)
				if !ok {
					continue
				}
				message := firstNonEmptyText(stringFromMap(anomaly, "message"), stringFromMap(anomaly, "type"))
				if message != "" {
					addAnomaly(clipText(message, 120))
				}
			}
		case hasAllKeys(obj, "step_id", "valid"):
			actual := strings.ToLower(stringFromMap(obj, "actual"))
			if strings.Contains(actual, "rebooted") {
				facts.NodeReboot = true
			}
			if strings.Contains(actual, "nodenotready") {
				facts.NodeNotReady = true
			}
			if strings.Contains(actual, "signal terminated") || strings.Contains(actual, "graceful shutdown") {
				facts.GracefulTermination = true
			}
			if strings.Contains(actual, "oom") {
				facts.OOMDetected = true
			}
			if strings.Contains(actual, "panic") {
				facts.PanicDetected = true
			}
		}
	}

	return facts
}

// buildReadableProblemSection 生成人员可读的问题摘要。
// 输入：state（流程状态）、facts（抽取事实）。
// 输出：问题摘要条目。
func buildReadableProblemSection(state *IncidentState, facts incidentReadableFacts) []string {
	out := make([]string, 0, 6)
	seen := make(map[string]struct{}, 6)
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

	if len(facts.RestartedPods) > 0 {
		appendOnce(fmt.Sprintf("故障窗口内多个关键 Pod 出现重启，重点包括：%s", joinReadableList(facts.RestartedPods, 5)))
	}
	for _, issue := range limitReadableItems(state.ExecutionIssues, 3) {
		appendOnce("执行阶段新增发现：" + clipText(issue, 180))
	}
	for _, finding := range limitReadableItems(state.ExecutionFindings, 2) {
		appendOnce("现场核查发现：" + clipText(finding, 180))
	}
	if impact := strings.TrimSpace(state.Impact); impact != "" {
		appendOnce("影响范围：" + clipText(impact, 160))
	}
	for _, note := range facts.ObservationNotes {
		appendOnce(note)
	}
	if facts.PrometheusDownCount > 0 || facts.PrometheusDropped > 0 {
		appendOnce(fmt.Sprintf("Prometheus 抓取目标存在异常：down=%d，dropped=%d", facts.PrometheusDownCount, facts.PrometheusDropped))
	}
	if facts.ElasticsearchNoHits {
		appendOnce("日志平台未发现与本次故障直接对应的错误日志")
	}
	for _, anomaly := range facts.AnomalyMessages {
		appendOnce("附带异常信号：" + anomaly)
	}

	return out
}

// buildReadableCauseSection 生成人员可读的根因分析。
// 输入：state（流程状态）、facts（抽取事实）。
// 输出：根因分析条目。
func buildReadableCauseSection(state *IncidentState, facts incidentReadableFacts) []string {
	out := make([]string, 0, 5)
	appendOnce := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		for _, existing := range out {
			if existing == text {
				return
			}
		}
		out = append(out, text)
	}

	switch {
	case facts.NodeReboot || facts.NodeNotReady:
		appendOnce("综合 Kubernetes 事件判断，故障更像是节点重启或短暂 NotReady 引发的连带重启，而非单个业务容器独立崩溃")
		if rootCause := strings.TrimSpace(state.RootCause); rootCause != "" {
			appendOnce(fmt.Sprintf("RCA 初步聚焦到 %s，但结合节点事件，更接近“节点异常触发该组件被动重启”的场景", humanizeIncidentTarget(rootCause)))
		}
	case strings.TrimSpace(state.RootCause) != "":
		appendOnce("RCA 初步定位对象：" + humanizeIncidentTarget(state.RootCause))
	}

	if facts.GracefulTermination && !facts.OOMDetected && !facts.PanicDetected {
		appendOnce("关键组件历史日志显示更像是收到终止信号后正常退出，暂未发现明确的 OOM 或 panic 证据")
	}
	if facts.PrometheusNoData || facts.ElasticsearchNoHits {
		appendOnce("由于监控指标和日志证据不完整，本次根因判断主要依赖 Kubernetes 事件、Pod 状态和历史日志片段")
	}
	if len(state.Evidence) > 0 {
		appendOnce("RCA 已提供结构化证据链，用于支撑上述根因判断")
	}

	return out
}

// buildReadableMethodSection 生成人员可读的解决方法摘要。
// 输入：state（流程状态）、facts（抽取事实）。
// 输出：解决方法条目。
func buildReadableMethodSection(state *IncidentState, facts incidentReadableFacts) []string {
	out := make([]string, 0, 5)
	if summary := strings.TrimSpace(firstNonEmptyText(state.RemediationProposalSummary, state.PlanSummary)); summary != "" {
		out = append(out, "总体策略："+clipText(summary, 180))
	}

	categories := summarizeExecutionCategories(state.ExecutionPlanSteps)
	if len(categories) > 0 {
		for _, item := range categories {
			out = append(out, item)
		}
	} else if len(state.RemediationProposalActions) > 0 {
		for _, item := range summarizeActionFlow(state.RemediationProposalActions) {
			out = append(out, item)
		}
	}

	if len(out) == 0 && facts.NodeReboot {
		out = append(out, "通过核查节点事件、关键组件状态和历史日志，确认问题由节点级异常触发，当前以恢复确认和风险复核为主")
	}
	if len(state.ExecutionIssues) > 0 {
		out = append(out, "本轮 execution 主要完成核查与诊断，已识别新增异常，但尚未对这些新增问题执行进一步变更修复")
	}
	return limitReadableItems(out, 5)
}

// buildReadableProcessSection 生成处置流程摘要。
// 输入：state（流程状态）。
// 输出：流程条目。
func buildReadableProcessSection(state *IncidentState) []string {
	out := make([]string, 0, 5)
	if planID := strings.TrimSpace(state.ExecutionPlanID); planID != "" {
		stepCount := len(state.ExecutionPlanSteps)
		if stepCount > 0 {
			out = append(out, fmt.Sprintf("生成命令级执行计划 %s，风险等级 %s，计划共 %d 步", planID, firstNonEmptyText(state.ExecutionPlanRisk, "unknown"), stepCount))
		} else {
			out = append(out, fmt.Sprintf("生成命令级执行计划 %s，并完成执行前风险校验", planID))
		}
	}
	if state.ExecutionStepCount > 0 {
		out = append(out, fmt.Sprintf("自动执行/核查阶段实际完成 %d 个关键步骤", state.ExecutionStepCount))
	}
	if state.ValidationBlocked {
		out = append(out, fmt.Sprintf("执行前风险校验提示当前计划存在阻断风险，风险等级为 %s", firstNonEmptyText(state.ValidationRisk, "unknown")))
	}
	for _, item := range summarizeExecutionCategories(state.ExecutionPlanSteps) {
		out = append(out, "流程重点："+item)
	}
	return limitReadableItems(out, 5)
}

// buildReadableResultSection 生成处理结果摘要。
// 输入：state（流程状态）、facts（抽取事实）、finalStatus（最终状态）。
// 输出：处理结果条目。
func buildReadableResultSection(state *IncidentState, facts incidentReadableFacts, finalStatus string) []string {
	out := make([]string, 0, 5)
	switch strings.ToLower(strings.TrimSpace(finalStatus)) {
	case "resolved", "success", "fixed", "completed":
		out = append(out, "本次事件已恢复，当前未见持续性的服务异常扩散")
	case "partially_resolved":
		out = append(out, "本次事件已完成初步定位和核查，但仍有部分问题需要继续跟进")
	default:
		out = append(out, "本次事件尚未完全闭环，仍需人工继续处理")
	}
	if state.RepeatedIssueEscalated {
		retryCount := maxIntValue(state.RepeatedIssueRetryCount, maxRepeatedIssueRetries)
		out = append(out, fmt.Sprintf("同一问题重复重试 %d 次后仍未闭环，已停止自动调用并转人工处理", retryCount))
		if reason := strings.TrimSpace(state.RepeatedIssueReason); reason != "" {
			out = append(out, "重复问题摘要："+clipText(reason, 180))
		}
	}

	if facts.NodeReboot || facts.NodeNotReady {
		out = append(out, "处置结论显示，本次更符合节点异常导致关键组件被动重启的场景")
	}
	if len(state.ExecutionIssues) > 0 {
		for _, issue := range limitReadableItems(state.ExecutionIssues, 2) {
			out = append(out, "当前仍需跟进的问题："+clipText(issue, 180))
		}
	}
	if facts.PrometheusNoData || facts.ElasticsearchNoHits {
		out = append(out, "监控和日志证据链仍不完整，后续复盘需要补齐历史指标与日志采集")
	}
	if fallback := strings.TrimSpace(state.ExecutionFallback); fallback != "" && !strings.EqualFold(finalStatus, "resolved") {
		out = append(out, "当前建议的人工后续动作："+clipText(fallback, 180))
	}

	return limitReadableItems(out, 5)
}

// buildReadableConclusionSection 生成最终结论与建议。
// 输入：state（流程状态）、facts（抽取事实）、finalStatus（最终状态）。
// 输出：结论与建议条目。
func buildReadableConclusionSection(state *IncidentState, facts incidentReadableFacts, finalStatus string) []string {
	out := make([]string, 0, 4)
	if summary := sanitizeReadableSummary(state.FinalReport); summary != "" {
		out = append(out, "综合结论："+summary)
	} else {
		out = append(out, "综合结论："+synthesizeIncidentConclusion(state, facts, finalStatus))
	}
	if state.RepeatedIssueEscalated {
		retryCount := maxIntValue(state.RepeatedIssueRetryCount, maxRepeatedIssueRetries)
		out = append(out, fmt.Sprintf("自动化流程已触发同问题重试上限（%d 次），建议由运维人员接管并执行人工处置闭环", retryCount))
	}

	if facts.NodeReboot || facts.NodeNotReady {
		out = append(out, "建议后续优先排查宿主节点重启原因、节点稳定性以及相关宿主机资源/系统日志")
	}
	if facts.PrometheusNoData || facts.ElasticsearchNoHits {
		out = append(out, "建议补齐 Prometheus 历史指标采集和 Elasticsearch 日志链路，避免类似事件复盘时证据不足")
	}
	for _, recommendation := range limitReadableItems(state.ExecutionRecommendations, 2) {
		out = append(out, "针对执行阶段发现问题的建议："+clipText(recommendation, 180))
	}
	if fallback := strings.TrimSpace(state.ExecutionFallback); fallback != "" && !strings.EqualFold(finalStatus, "resolved") {
		out = append(out, "如需继续处置，可按人工兜底方案推进："+clipText(fallback, 180))
	}
	return limitReadableItems(out, 4)
}

// summarizeExecutionCategories 将执行计划步骤压缩为流程类别。
// 输入：steps（执行计划步骤摘要列表）。
// 输出：去重后的流程类别描述。
func summarizeExecutionCategories(steps []string) []string {
	if len(steps) == 0 {
		return nil
	}
	categories := make([]string, 0, 5)
	seen := make(map[string]struct{}, 5)
	appendCategory := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if _, ok := seen[text]; ok {
			return
		}
		seen[text] = struct{}{}
		categories = append(categories, text)
	}

	for _, step := range steps {
		desc := extractReadableStepDescription(step)
		lower := strings.ToLower(desc)
		switch {
		case strings.Contains(lower, "节点") || strings.Contains(lower, "node"):
			appendCategory("核查节点状态、节点事件和系统层异常")
		case strings.Contains(lower, "pod") || strings.Contains(lower, "infra"):
			appendCategory("核查关键 Pod 的状态、事件和重启时间线")
		case strings.Contains(lower, "日志") || strings.Contains(lower, "logs"):
			appendCategory("回溯关键组件历史日志，确认是否存在 OOM、panic 或被动终止")
		case strings.Contains(lower, "资源") || strings.Contains(lower, "top") || strings.Contains(lower, "cpu") || strings.Contains(lower, "内存"):
			appendCategory("复核节点与 Pod 资源使用情况，排查持续性资源压力")
		case strings.Contains(lower, "pv") || strings.Contains(lower, "pvc") || strings.Contains(lower, "存储"):
			appendCategory("补充检查存储状态，排除卷异常或容量问题")
		case strings.Contains(lower, "warning") || strings.Contains(lower, "事件摘要") || strings.Contains(lower, "all-namespaces"):
			appendCategory("汇总集群级告警和 Warning 事件，确认是否存在系统性异常")
		}
	}

	return limitReadableItems(categories, 5)
}

// summarizeActionFlow 将修复提案动作压缩为简明流程。
// 输入：actions（修复提案动作摘要列表）。
// 输出：流程条目。
func summarizeActionFlow(actions []string) []string {
	if len(actions) == 0 {
		return nil
	}
	out := make([]string, 0, len(actions))
	for _, action := range actions {
		text := extractReadableStepDescription(action)
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return limitReadableItems(out, 4)
}

// extractReadableStepDescription 从步骤摘要中提取面向人的描述文本。
// 输入：step（包含“步骤 X：xxx；...”的摘要）。
// 输出：不含命令和预期细节的描述。
func extractReadableStepDescription(step string) string {
	step = strings.TrimSpace(step)
	if step == "" {
		return ""
	}
	if index := strings.Index(step, "："); index >= 0 && index+len("：") < len(step) {
		step = step[index+len("："):]
	}
	if index := strings.Index(step, "；"); index >= 0 {
		step = step[:index]
	}
	return strings.TrimSpace(step)
}

// humanizeIncidentTarget 将资源标识转换为更自然的中文表达。
// 输入：target（原始资源标识，如 infra/etcd）。
// 输出：中文可读表达。
func humanizeIncidentTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	parts := strings.Split(target, "/")
	if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != "" {
		return fmt.Sprintf("%s 命名空间的 %s", strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
	}
	return target
}

// sanitizeReadableSummary 清理 strategy 阶段的 summary 文本。
// 输入：summary（原始摘要）。
// 输出：可直接展示的结论摘要。
func sanitizeReadableSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	if strings.HasPrefix(summary, "{") && strings.HasSuffix(summary, "}") {
		return ""
	}
	return clipText(summary, 220)
}

// synthesizeIncidentConclusion 在缺少 strategy summary 时生成兜底结论。
// 输入：state（流程状态）、facts（抽取事实）、finalStatus（最终状态）。
// 输出：面向运维人员的总结结论。
func synthesizeIncidentConclusion(state *IncidentState, facts incidentReadableFacts, finalStatus string) string {
	switch strings.ToLower(strings.TrimSpace(finalStatus)) {
	case "resolved", "success", "fixed", "completed":
		if facts.NodeReboot || facts.NodeNotReady {
			return "本次事件已恢复，综合判断为节点异常触发关键组件连带重启，当前重点应转向宿主机原因排查和监控补强。"
		}
		if rootCause := strings.TrimSpace(state.RootCause); rootCause != "" {
			return fmt.Sprintf("本次事件已恢复，当前根因初步定位为 %s，建议结合后续复盘继续验证。", humanizeIncidentTarget(rootCause))
		}
		return "本次事件已恢复，但仍建议补充复盘记录，避免同类问题再次发生。"
	case "partially_resolved":
		return "本次事件已完成初步定位与风险核查，但自动化处置尚未完全闭环，仍需继续跟进。"
	default:
		return "本次事件尚未闭环，请依据人工兜底方案继续处置，并补充更多监控与日志证据。"
	}
}

// splitReadableSentences 将摘要文本拆分为句子列表。
// 输入：text（原始摘要文本）。
// 输出：拆分后的句子数组。
func splitReadableSentences(text string) []string {
	return strings.FieldsFunc(strings.TrimSpace(text), func(r rune) bool {
		switch r {
		case '。', '！', '？', ';', '；', '\n':
			return true
		default:
			return false
		}
	})
}

// joinReadableList 将列表拼接为适合阅读的简短文本。
// 输入：items（原始列表）、limit（最大展示数量）。
// 输出：拼接后的文本。
func joinReadableList(items []string, limit int) string {
	items = limitReadableItems(items, limit)
	if len(items) == 0 {
		return ""
	}
	return strings.Join(items, "、")
}

// limitReadableItems 截断列表长度并清理空项。
// 输入：items（原始列表）、limit（最大保留数）。
// 输出：清洗后的列表。
func limitReadableItems(items []string, limit int) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
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

	if summary := strings.TrimSpace(firstNonEmptyText(state.RemediationProposalSummary, state.PlanSummary)); summary != "" {
		appendMethod("修复策略：" + summary)
	}
	if summary := strings.TrimSpace(state.ExecutionPlanDesc); summary != "" {
		appendMethod("命令执行计划：" + summary)
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
	if fallback := strings.TrimSpace(firstNonEmptyText(state.ExecutionFallback, state.RemediationProposalFallback, state.FallbackPlan)); fallback != "" {
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

// archiveFinalOpsReport 将完整技术报告以单文档形式归档到 ops 案例库。
// 输入：ctx、state、report、path（本地落盘路径）、logger。
// 输出：错误；成功时说明知识库归档完成。
func archiveFinalOpsReport(ctx context.Context, state *IncidentState, report, path string, logger *zap.Logger) error {
	report = strings.TrimSpace(report)
	if report == "" {
		return fmt.Errorf("empty report")
	}

	indexer, err := aiindexer.NewMilvusIndexerWithCollection(ctx, common.MilvusOpsCollection)
	if err != nil {
		return fmt.Errorf("init ops report indexer failed: %w", err)
	}

	sessionID := "aiops"
	if value, ok := adk.GetSessionValue(ctx, "session_id"); ok {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			sessionID = strings.TrimSpace(text)
		}
	}

	reportID := fmt.Sprintf("ops_report_%s_%d", sanitizeReportFileToken(sessionID), time.Now().UnixNano())
	title := buildFinalReportTitle(state)
	doc := &schema.Document{
		ID:      reportID,
		Content: report,
		MetaData: map[string]any{
			"type":         "ops_final_report",
			"title":        title,
			"session_id":   sessionID,
			"final_status": firstNonEmptyText(stateValue(state, func(s *IncidentState) string { return s.FinalStatus }), "unknown"),
			"root_cause":   stateValue(state, func(s *IncidentState) string { return s.RootCause }),
			"target_node":  stateValue(state, func(s *IncidentState) string { return s.TargetNode }),
			"report_path":  strings.TrimSpace(path),
			"updated_at":   time.Now().Format(time.RFC3339),
		},
	}

	_, err = indexer.Store(ctx, []*schema.Document{doc})
	if err != nil {
		return fmt.Errorf("store final report failed: %w", err)
	}
	if logger != nil {
		logger.Info("incident final report archived to knowledge store",
			zap.String("report_id", reportID),
			zap.String("title", title))
	}
	return nil
}

// buildFinalReportTitle 生成最终技术报告标题。
// 输入：state。
// 输出：适合归档与检索的标题文本。
func buildFinalReportTitle(state *IncidentState) string {
	if state == nil {
		return "运维技术报告"
	}
	target := humanizeIncidentTarget(firstNonEmptyText(state.TargetNode, state.RootCause))
	status := strings.TrimSpace(state.FinalStatus)
	switch {
	case target != "" && status != "":
		return fmt.Sprintf("%s 运维技术报告（%s）", target, status)
	case target != "":
		return fmt.Sprintf("%s 运维技术报告", target)
	default:
		return "运维技术报告"
	}
}

// stateValue 安全读取 IncidentState 的字符串字段。
// 输入：state、selector。
// 输出：去空白后的字段值。
func stateValue(state *IncidentState, selector func(*IncidentState) string) string {
	if state == nil || selector == nil {
		return ""
	}
	return strings.TrimSpace(selector(state))
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
