package ops

import (
	"context"
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

type IncidentState struct {
	ObservationCollected bool     `json:"observation_collected,omitempty"`
	ObservationNamespace string   `json:"observation_namespace,omitempty"`
	ObservationSummary   string   `json:"observation_summary,omitempty"`
	ObservationErrors    []string `json:"observation_errors,omitempty"`

	RootCause  string   `json:"root_cause,omitempty"`
	TargetNode string   `json:"target_node,omitempty"`
	Path       string   `json:"path,omitempty"`
	Impact     string   `json:"impact,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	Evidence   []string `json:"evidence,omitempty"`

	PlanID       string `json:"plan_id,omitempty"`
	PlanSummary  string `json:"plan_summary,omitempty"`
	PlanRisk     string `json:"plan_risk,omitempty"`
	FallbackPlan string `json:"fallback_plan,omitempty"`

	ValidationBlocked bool   `json:"validation_blocked,omitempty"`
	ValidationRisk    string `json:"validation_risk,omitempty"`

	ExecutionStatus    string   `json:"execution_status,omitempty"`
	ExecutionSuccess   bool     `json:"execution_success,omitempty"`
	ExecutionStepCount int      `json:"execution_step_count,omitempty"`
	ExecutionReason    string   `json:"execution_reason,omitempty"`
	ExecutionFallback  string   `json:"execution_fallback,omitempty"`
	ExecutionLogs      []string `json:"execution_logs,omitempty"`

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
		plan, ok := parseOpsExecutionPlan(messages)
		if ok && plan != nil {
			state.PlanID = strings.TrimSpace(plan.PlanID)
			state.PlanSummary = strings.TrimSpace(plan.Summary)
			state.PlanRisk = strings.TrimSpace(plan.RiskLevel)
			state.FallbackPlan = strings.TrimSpace(plan.FallbackPlan)
		}
		validation, ok := parseValidationResult(messages)
		if ok && validation != nil {
			state.ValidationBlocked = validation.Blocked
			state.ValidationRisk = strings.TrimSpace(validation.RiskLevel)
		}
	case "execution":
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
					state.ExecutionStatus = strings.TrimSpace(statusText)
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
		"observation_collected": state.ObservationCollected,
		"observation_namespace": state.ObservationNamespace,
		"observation_summary":   state.ObservationSummary,
		"observation_errors":    state.ObservationErrors,
		"root_cause":            state.RootCause,
		"target_node":           state.TargetNode,
		"path":                  state.Path,
		"impact":                state.Impact,
		"confidence":            state.Confidence,
		"plan_id":               state.PlanID,
		"plan_summary":          state.PlanSummary,
		"plan_risk":             state.PlanRisk,
		"validation_blocked":    state.ValidationBlocked,
		"validation_risk":       state.ValidationRisk,
		"execution_status":      state.ExecutionStatus,
		"execution_success":     state.ExecutionSuccess,
		"execution_step_count":  state.ExecutionStepCount,
		"execution_reason":      state.ExecutionReason,
		"execution_fallback":    state.ExecutionFallback,
		"execution_log_count":   len(state.ExecutionLogs),
		"latest_execution_logs": latestIncidentLogs(state.ExecutionLogs, 5),
		"final_status":          state.FinalStatus,
		"final_report":          state.FinalReport,
		"updated_at":            state.UpdatedAt,
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
