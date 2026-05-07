package ops

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

const incidentStateSessionKey = "incident_graph_state"

const (
	maxIncidentUserInputRunes = 1600
	maxIncidentStateRunes     = 2400
	maxIncidentExecutionLogs  = 200
)

func init() {
	gob.Register(&IncidentState{})
}

type IncidentState struct {
	ObservationCollected      bool     `json:"observation_collected,omitempty"`
	ObservationNamespace      string   `json:"observation_namespace,omitempty"`
	ObservationCollectedAt    string   `json:"observation_collected_at,omitempty"`
	ObservationTimeRange      string   `json:"observation_time_range,omitempty"`
	ObservationSummary        string   `json:"observation_summary,omitempty"`
	ObservationErrors         []string `json:"observation_errors,omitempty"`
	ObservationRefreshNeeded  bool     `json:"observation_refresh_needed,omitempty"`
	ObservationRefreshReason  string   `json:"observation_refresh_reason,omitempty"`
	RuntimeObservationSummary string   `json:"runtime_observation_summary,omitempty"`

	RootCause  string   `json:"root_cause,omitempty"`
	TargetNode string   `json:"target_node,omitempty"`
	Path       string   `json:"path,omitempty"`
	Impact     string   `json:"impact,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	Evidence   []string `json:"evidence,omitempty"`

	PlanID       string `json:"plan_id,omitempty"`      // 兼容旧字段：映射 remediation proposal id
	PlanSummary  string `json:"plan_summary,omitempty"` // 兼容旧字段：映射 remediation proposal summary
	PlanRisk     string `json:"plan_risk,omitempty"`    // 兼容旧字段：映射 remediation proposal risk
	FallbackPlan string `json:"fallback_plan,omitempty"`

	RemediationProposalID       string   `json:"remediation_proposal_id,omitempty"`
	RemediationProposalSummary  string   `json:"remediation_proposal_summary,omitempty"`
	RemediationProposalRisk     string   `json:"remediation_proposal_risk,omitempty"`
	RemediationProposalFallback string   `json:"remediation_proposal_fallback,omitempty"`
	RemediationProposalActions  []string `json:"remediation_proposal_actions,omitempty"`

	ValidationBlocked bool   `json:"validation_blocked,omitempty"`
	ValidationRisk    string `json:"validation_risk,omitempty"`

	ExecutionStatus          string   `json:"execution_status,omitempty"`
	ExecutionSuccess         bool     `json:"execution_success,omitempty"`
	ExecutionStepCount       int      `json:"execution_step_count,omitempty"`
	ExecutionReason          string   `json:"execution_reason,omitempty"`
	ExecutionFallback        string   `json:"execution_fallback,omitempty"`
	ExecutionOverallHealth   string   `json:"execution_overall_health,omitempty"`
	ExecutionFindings        []string `json:"execution_findings,omitempty"`
	ExecutionIssues          []string `json:"execution_issues,omitempty"`
	ExecutionRecommendations []string `json:"execution_recommendations,omitempty"`
	ExecutionPlanID          string   `json:"execution_plan_id,omitempty"`
	ExecutionPlanDesc        string   `json:"execution_plan_desc,omitempty"`
	ExecutionPlanRisk        string   `json:"execution_plan_risk,omitempty"`
	ExecutionPlanSteps       []string `json:"execution_plan_steps,omitempty"`
	ExecutionLogs            []string `json:"execution_logs,omitempty"`
	RepeatedIssueKey         string   `json:"repeated_issue_key,omitempty"`
	RepeatedIssueReason      string   `json:"repeated_issue_reason,omitempty"`
	RepeatedIssueRetryCount  int      `json:"repeated_issue_retry_count,omitempty"`
	RepeatedIssueRetryLimit  int      `json:"repeated_issue_retry_limit,omitempty"`
	RepeatedIssueEscalated   bool     `json:"repeated_issue_escalated,omitempty"`

	FinalStatus string `json:"final_status,omitempty"`
	FinalReport string `json:"final_report,omitempty"`

	UpdatedAt string `json:"updated_at,omitempty"`
}

type stateBridgeAgent struct {
	name   string
	desc   string
	stage  string
	inner  adk.Agent
	logger *zap.Logger
}

func wrapWithIncidentState(stage string, inner adk.Agent, logger *zap.Logger) adk.Agent {
	if inner == nil {
		return nil
	}
	return &stateBridgeAgent{
		name:   inner.Name(context.Background()),
		desc:   inner.Description(context.Background()),
		stage:  stage,
		inner:  inner,
		logger: logger,
	}
}

func (a *stateBridgeAgent) Name(_ context.Context) string {
	return a.name
}

func (a *stateBridgeAgent) Description(_ context.Context) string {
	return a.desc
}

func (a *stateBridgeAgent) Run(ctx context.Context, input *adk.AgentInput, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	agentName := strings.TrimSpace(a.name)
	if agentName != "" {
		adk.AddSessionValue(ctx, "current_agent", agentName)
	}
	iter := a.inner.Run(ctx, input, opts...)
	return a.track(ctx, iter)
}

func (a *stateBridgeAgent) Resume(ctx context.Context, info *adk.ResumeInfo, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	agentName := strings.TrimSpace(a.name)
	if agentName != "" {
		adk.AddSessionValue(ctx, "current_agent", agentName)
	}
	ra, ok := a.inner.(adk.ResumableAgent)
	if !ok {
		iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
		go func() {
			defer generator.Close()
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("agent %s is not resumable", a.name)})
		}()
		return iterator
	}
	return a.track(ctx, ra.Resume(ctx, info, opts...))
}

func (a *stateBridgeAgent) track(ctx context.Context, iter *adk.AsyncIterator[*adk.AgentEvent]) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer generator.Close()
		for {
			event, ok := iter.Next()
			if !ok {
				break
			}
			if event == nil {
				continue
			}
			a.captureState(ctx, event)
			generator.Send(event)
		}
	}()
	return iterator
}

func (a *stateBridgeAgent) captureState(ctx context.Context, event *adk.AgentEvent) {
	if event == nil {
		return
	}
	state := getIncidentState(ctx)
	agentName := strings.TrimSpace(event.AgentName)
	if agentName == "" {
		agentName = strings.TrimSpace(a.name)
	}

	if event.Err != nil {
		appendIncidentExecutionLog(state, fmt.Sprintf("[%s] 事件错误：%s", agentName, clipText(event.Err.Error(), 600)))
	}

	if event.Output != nil && event.Output.MessageOutput != nil && !event.Output.MessageOutput.IsStreaming && event.Output.MessageOutput.Message != nil {
		msg := event.Output.MessageOutput.Message
		if msg != nil {
			for _, call := range msg.ToolCalls {
				toolName := strings.TrimSpace(call.Function.Name)
				if toolName == "" {
					toolName = "(unknown)"
				}
				args := clipText(strings.TrimSpace(call.Function.Arguments), 280)
				if args == "" {
					args = "{}"
				}
				appendIncidentExecutionLog(state, fmt.Sprintf("[%s] 调用工具：%s args=%s", agentName, toolName, args))
			}
			if content := strings.TrimSpace(msg.Content); content != "" {
				appendIncidentExecutionLog(state, fmt.Sprintf("[%s] 输出：%s", agentName, clipText(content, 600)))
			}
			a.updateByStage(state, msg)
		}
	}

	if event.Action != nil && event.Action.Interrupted != nil {
		if info, ok := event.Action.Interrupted.Data.(*IncidentInterruptInfo); ok && info != nil {
			if strings.EqualFold(strings.TrimSpace(info.Type), "manual_required") {
				state.ExecutionStatus = "manual_required"
			}
			state.ExecutionReason = clipText(info.Reason, 600)
			if strings.TrimSpace(info.FallbackPlan) != "" {
				state.ExecutionFallback = clipText(info.FallbackPlan, 800)
			}
			appendIncidentExecutionLog(state, fmt.Sprintf("[%s] 中断：type=%s reason=%s", agentName, strings.TrimSpace(info.Type), clipText(info.Reason, 300)))
		} else if detail := strings.TrimSpace(fmt.Sprintf("%v", event.Action.Interrupted.Data)); detail != "" {
			appendIncidentExecutionLog(state, fmt.Sprintf("[%s] 中断：%s", agentName, clipText(detail, 300)))
		}
	}

	state.UpdatedAt = time.Now().Format(time.RFC3339)
	setIncidentState(ctx, state)
}

func (a *stateBridgeAgent) updateByStage(state *IncidentState, msg *schema.Message) {
	if state == nil || msg == nil {
		return
	}
	messages := []adk.Message{msg}
	switch a.stage {
	case "rca":
		report, ok := parseRCAReport(messages)
		if !ok || report == nil {
			return
		}
		state.RootCause = strings.TrimSpace(report.RootCause)
		state.TargetNode = strings.TrimSpace(report.TargetNode)
		state.Path = strings.TrimSpace(report.Path)
		state.Impact = strings.TrimSpace(report.Impact)
		state.Confidence = report.Confidence
		state.Evidence = report.Evidence
	case "ops":
		proposal, ok := parseRemediationProposal(messages)
		if ok && proposal != nil {
			state.RemediationProposalID = strings.TrimSpace(proposal.ProposalID)
			state.RemediationProposalSummary = clipText(strings.TrimSpace(proposal.Summary), 600)
			state.RemediationProposalRisk = strings.TrimSpace(proposal.RiskLevel)
			state.RemediationProposalFallback = clipText(strings.TrimSpace(proposal.FallbackPlan), 800)
			state.RemediationProposalActions = summarizeRemediationActions(proposal)

			state.PlanID = state.RemediationProposalID
			state.PlanSummary = state.RemediationProposalSummary
			state.PlanRisk = state.RemediationProposalRisk
			state.FallbackPlan = state.RemediationProposalFallback
		}
	case "execution":
		validation, ok := parseValidationResult(messages)
		if ok && validation != nil {
			state.ValidationBlocked = validation.Blocked
			state.ValidationRisk = strings.TrimSpace(validation.RiskLevel)
		}
		plan, ok := parseGeneratedExecutionPlan(messages)
		if ok && plan != nil {
			state.ExecutionPlanID = strings.TrimSpace(plan.PlanID)
			state.ExecutionPlanDesc = clipText(strings.TrimSpace(plan.Description), 600)
			state.ExecutionPlanRisk = strings.TrimSpace(plan.RiskLevel)
			state.ExecutionPlanSteps = summarizeGeneratedExecutionPlan(plan)
		}
		stepValidation, ok := parseStepValidationResult(messages)
		if ok && stepValidation != nil && stepValidation.ShouldStop {
			if strings.EqualFold(strings.TrimSpace(stepValidation.StopAction), "replan") {
				state.ExecutionStatus = "replan_required"
				state.ObservationRefreshNeeded = true
				state.ObservationRefreshReason = clipText(firstNonEmptyText(stepValidation.StopReason, stepValidation.MismatchReason, stepValidation.Message), 600)
				state.RuntimeObservationSummary = clipText(firstNonEmptyText(stepValidation.RuntimeSummary, stepValidation.MismatchReason, stepValidation.Actual), 800)
			} else {
				state.ExecutionStatus = "manual_required"
			}
			state.ExecutionReason = clipText(firstNonEmptyText(stepValidation.StopReason, stepValidation.Message), 600)
		}
		status := detectExecutionStatus(messages)
		if status.Found {
			if status.Success {
				state.ExecutionStatus = "success"
			} else {
				state.ExecutionStatus = "failed"
			}
			state.ExecutionSuccess = status.Success
			state.ExecutionReason = clipText(status.RawMessageHint, 600)
		}
		result, ok := parseExecutionResult(messages)
		if ok && result != nil {
			if value, exists := result["execution_status"]; exists {
				if statusText, ok := value.(string); ok && strings.TrimSpace(statusText) != "" {
					nextStatus := strings.TrimSpace(statusText)
					if !(strings.EqualFold(state.ExecutionStatus, "replan_required") && strings.EqualFold(nextStatus, "manual_required")) {
						state.ExecutionStatus = nextStatus
					}
				}
			}
			state.ExecutionStepCount = parseExecutedStepCount(result)
			if value, exists := result["failed_reason"]; exists {
				if reason, ok := value.(string); ok && strings.TrimSpace(reason) != "" {
					state.ExecutionReason = clipText(reason, 600)
				}
			}
			if value, exists := result["manual_plan"]; exists {
				if manualPlan, ok := value.(string); ok && strings.TrimSpace(manualPlan) != "" {
					state.ExecutionFallback = clipText(manualPlan, 800)
				}
			}
			diagnostic := parseExecutionDiagnosticInsight(result)
			if health := strings.TrimSpace(diagnostic.OverallHealth); health != "" {
				state.ExecutionOverallHealth = clipText(health, 120)
			}
			if len(diagnostic.Findings) > 0 {
				state.ExecutionFindings = latestIncidentLogs(diagnostic.Findings, 5)
			}
			if len(diagnostic.Issues) > 0 {
				state.ExecutionIssues = latestIncidentLogs(diagnostic.Issues, 5)
			}
			if len(diagnostic.Recommendations) > 0 {
				state.ExecutionRecommendations = latestIncidentLogs(diagnostic.Recommendations, 5)
			}
			if diagnostic.ActionableIssueCount > 0 && strings.EqualFold(strings.TrimSpace(state.ExecutionStatus), "success") {
				state.ExecutionStatus = "replan_required"
				state.ExecutionSuccess = false
				state.ExecutionReason = clipText(firstNonEmptyText(
					joinExecutionIssueSummaries(diagnostic.Issues, 2),
					diagnostic.Summary,
					state.ExecutionReason,
				), 600)
			}
		}
	case "strategy":
		report, ok := parseStrategyReport(messages)
		if !ok || report == nil {
			return
		}
		if value, exists := report["final_status"]; exists {
			if finalStatus, ok := value.(string); ok && strings.TrimSpace(finalStatus) != "" {
				state.FinalStatus = strings.TrimSpace(finalStatus)
			}
		}
		if value, exists := report["summary"]; exists {
			if summary, ok := value.(string); ok && strings.TrimSpace(summary) != "" {
				state.FinalReport = clipText(summary, 800)
			}
		}
	}
}

func incidentHistoryRewriter(ctx context.Context, entries []*adk.HistoryEntry) ([]adk.Message, error) {
	out := make([]adk.Message, 0, 3)
	lastUser := findLastUserInput(entries)
	if lastUser != nil {
		latestQuestion := clipText(lastUser.Content, maxIncidentUserInputRunes)
		if strings.TrimSpace(latestQuestion) != "" {
			out = append(out, schema.UserMessage(latestQuestion))
		}
	}

	state := getIncidentState(ctx)
	if state == nil {
		return out, nil
	}
	stateText := renderIncidentState(state)
	if strings.TrimSpace(stateText) != "" {
		out = append(out, schema.UserMessage(stateText))
	}
	return out, nil
}

func findLastUserInput(entries []*adk.HistoryEntry) adk.Message {
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry == nil || !entry.IsUserInput || entry.Message == nil {
			continue
		}
		if entry.Message.Role != schema.User {
			continue
		}
		return entry.Message
	}
	return nil
}

func renderIncidentState(state *IncidentState) string {
	if state == nil {
		return ""
	}
	payload := map[string]any{
		"observation_collected":         state.ObservationCollected,
		"observation_namespace":         state.ObservationNamespace,
		"observation_collected_at":      state.ObservationCollectedAt,
		"observation_time_range":        state.ObservationTimeRange,
		"observation_summary":           state.ObservationSummary,
		"observation_errors":            state.ObservationErrors,
		"observation_refresh_needed":    state.ObservationRefreshNeeded,
		"observation_refresh_reason":    state.ObservationRefreshReason,
		"runtime_observation_summary":   state.RuntimeObservationSummary,
		"root_cause":                    state.RootCause,
		"target_node":                   state.TargetNode,
		"path":                          state.Path,
		"impact":                        state.Impact,
		"confidence":                    state.Confidence,
		"plan_id":                       state.PlanID,
		"plan_summary":                  state.PlanSummary,
		"plan_risk":                     state.PlanRisk,
		"fallback_plan":                 state.FallbackPlan,
		"remediation_proposal_id":       state.RemediationProposalID,
		"remediation_proposal_summary":  state.RemediationProposalSummary,
		"remediation_proposal_risk":     state.RemediationProposalRisk,
		"remediation_proposal_fallback": state.RemediationProposalFallback,
		"remediation_proposal_actions":  latestIncidentLogs(state.RemediationProposalActions, 4),
		"validation_blocked":            state.ValidationBlocked,
		"validation_risk":               state.ValidationRisk,
		"execution_status":              state.ExecutionStatus,
		"execution_success":             state.ExecutionSuccess,
		"execution_step_count":          state.ExecutionStepCount,
		"execution_reason":              state.ExecutionReason,
		"execution_fallback":            state.ExecutionFallback,
		"execution_overall_health":      state.ExecutionOverallHealth,
		"execution_findings":            latestIncidentLogs(state.ExecutionFindings, 4),
		"execution_issues":              latestIncidentLogs(state.ExecutionIssues, 4),
		"execution_recommendations":     latestIncidentLogs(state.ExecutionRecommendations, 4),
		"execution_plan_id":             state.ExecutionPlanID,
		"execution_plan_desc":           state.ExecutionPlanDesc,
		"execution_plan_risk":           state.ExecutionPlanRisk,
		"execution_plan_steps":          latestIncidentLogs(state.ExecutionPlanSteps, 4),
		"execution_log_count":           len(state.ExecutionLogs),
		"latest_execution_logs":         latestIncidentLogs(state.ExecutionLogs, 5),
		"repeated_issue_reason":         state.RepeatedIssueReason,
		"repeated_issue_retry_count":    state.RepeatedIssueRetryCount,
		"repeated_issue_retry_limit":    state.RepeatedIssueRetryLimit,
		"repeated_issue_escalated":      state.RepeatedIssueEscalated,
		"final_status":                  state.FinalStatus,
		"final_report":                  state.FinalReport,
		"updated_at":                    state.UpdatedAt,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return clipText("当前工作流结构化状态（Graph State）：\n"+string(body), maxIncidentStateRunes)
}

func clipText(text string, maxRunes int) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if maxRunes <= 0 {
		maxRunes = 512
	}
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	return string(runes[:maxRunes]) + "..."
}

func getIncidentState(ctx context.Context) *IncidentState {
	value, ok := adk.GetSessionValue(ctx, incidentStateSessionKey)
	if !ok || value == nil {
		return &IncidentState{}
	}
	switch typed := value.(type) {
	case *IncidentState:
		copyState := *typed
		return &copyState
	case IncidentState:
		copyState := typed
		return &copyState
	default:
		return &IncidentState{}
	}
}

func setIncidentState(ctx context.Context, state *IncidentState) {
	if state == nil {
		return
	}
	adk.AddSessionValue(ctx, incidentStateSessionKey, state)
}

// appendIncidentExecutionLog 追加执行日志并控制总量。
// 输入：state（流程状态）、entry（日志文本）。
// 输出：无。
func appendIncidentExecutionLog(state *IncidentState, entry string) {
	if state == nil {
		return
	}
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return
	}
	state.ExecutionLogs = append(state.ExecutionLogs, entry)
	if len(state.ExecutionLogs) > maxIncidentExecutionLogs {
		state.ExecutionLogs = append([]string(nil), state.ExecutionLogs[len(state.ExecutionLogs)-maxIncidentExecutionLogs:]...)
	}
}

// latestIncidentLogs 返回日志尾部 N 条。
// 输入：logs（日志列表）、limit（条数上限）。
// 输出：尾部日志切片。
func latestIncidentLogs(logs []string, limit int) []string {
	if len(logs) == 0 {
		return nil
	}
	if limit <= 0 || len(logs) <= limit {
		return append([]string(nil), logs...)
	}
	return append([]string(nil), logs[len(logs)-limit:]...)
}

// summarizeRemediationActions 将修复提案动作转换为简明文本。
// 输入：修复提案。
// 输出：动作摘要列表。
func summarizeRemediationActions(proposal *RemediationProposal) []string {
	if proposal == nil || len(proposal.Actions) == 0 {
		return nil
	}
	out := make([]string, 0, len(proposal.Actions))
	for _, action := range proposal.Actions {
		line := fmt.Sprintf("步骤 %d：%s", action.Step, firstNonEmptyText(action.Goal, "未命名动作"))
		if rationale := strings.TrimSpace(action.Rationale); rationale != "" {
			line += fmt.Sprintf("；理由=%s", rationale)
		}
		if hint := strings.TrimSpace(action.CommandHint); hint != "" {
			line += fmt.Sprintf("；命令提示=%s", hint)
		}
		if success := strings.TrimSpace(action.SuccessCriteria); success != "" {
			line += fmt.Sprintf("；成功判据=%s", success)
		}
		if rollback := strings.TrimSpace(action.RollbackHint); rollback != "" {
			line += fmt.Sprintf("；回退=%s", rollback)
		}
		out = append(out, clipText(line, 320))
	}
	return out
}

// summarizeGeneratedExecutionPlan 将结构化计划转换为可展示的步骤摘要。
// 输入：execution_agent 生成的计划。
// 输出：步骤摘要列表。
func summarizeGeneratedExecutionPlan(plan *GeneratedExecutionPlan) []string {
	if plan == nil || len(plan.Steps) == 0 {
		return nil
	}
	out := make([]string, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		command := strings.TrimSpace(step.Command)
		if len(step.Args) > 0 {
			command = strings.TrimSpace(command + " " + strings.Join(step.Args, " "))
		}
		line := fmt.Sprintf("步骤 %d：%s", step.StepID, firstNonEmptyText(step.Description, "执行命令"))
		if command != "" {
			line += fmt.Sprintf("；命令=%s", command)
		}
		if expected := strings.TrimSpace(step.ExpectedResult); expected != "" {
			line += fmt.Sprintf("；预期=%s", expected)
		}
		if rollback := strings.TrimSpace(step.RollbackCommand); rollback != "" {
			if len(step.RollbackArgs) > 0 {
				rollback = strings.TrimSpace(rollback + " " + strings.Join(step.RollbackArgs, " "))
			}
			line += fmt.Sprintf("；回滚=%s", rollback)
		}
		out = append(out, clipText(line, 320))
	}
	return out
}

// firstNonEmptyText 返回第一个非空文本。
// 输入：候选文本列表。
// 输出：第一个去空白后非空的文本；若都为空返回空字符串。
func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}
