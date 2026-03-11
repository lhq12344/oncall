package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// RCAReport RCA -> Ops 的结构化契约。
type RCAReport struct {
	RootCause  string   `json:"root_cause"`
	TargetNode string   `json:"target_node"`
	Path       string   `json:"path"`
	Impact     string   `json:"impact"`
	Confidence float64  `json:"confidence"`
	Evidence   []string `json:"evidence"`
}

// OpsCommandStep Ops 输出的执行步骤。
type OpsCommandStep struct {
	Step     int    `json:"step"`
	Goal     string `json:"goal"`
	Command  string `json:"command"`
	Expected string `json:"expected"`
	Rollback string `json:"rollback"`
	ReadOnly bool   `json:"read_only"`
}

// OpsExecutionPlan Ops -> Execution 的结构化契约。
type OpsExecutionPlan struct {
	PlanID       string           `json:"plan_id"`
	Summary      string           `json:"summary"`
	RootCause    string           `json:"root_cause"`
	TargetNode   string           `json:"target_node"`
	RiskLevel    string           `json:"risk_level"`
	Commands     []OpsCommandStep `json:"commands"`
	FallbackPlan string           `json:"fallback_plan"`
}

// PlanValidationResult Validator 节点输出。
type PlanValidationResult struct {
	Valid                bool     `json:"valid"`
	Blocked              bool     `json:"blocked"`
	RequiresConfirmation bool     `json:"requires_confirmation"`
	RiskLevel            string   `json:"risk_level"`
	Reasons              []string `json:"reasons"`
	UnsafeCommands       []string `json:"unsafe_commands,omitempty"`
	ReviewCommands       []string `json:"review_commands,omitempty"`
	PlanID               string   `json:"plan_id,omitempty"`
}

// ExecutionStatus Gate 节点对 execution 输出的归一化判断。
type ExecutionStatus struct {
	Found          bool
	Success        bool
	ToolCannotFix  bool
	RawMessageHint string
}

// IncidentInterruptInfo 中断给用户的结构化信息。
type IncidentInterruptInfo struct {
	Type         string `json:"type"`
	Reason       string `json:"reason"`
	PlanID       string `json:"plan_id,omitempty"`
	FallbackPlan string `json:"fallback_plan,omitempty"`
}

func assistantEvent(content string) *adk.AgentEvent {
	return &adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{
				Message: schema.AssistantMessage(strings.TrimSpace(content), nil),
				Role:    schema.Assistant,
			},
		},
	}
}

func breakLoopEvent(agentName, content string) *adk.AgentEvent {
	event := assistantEvent(content)
	event.Action = adk.NewBreakLoopAction(agentName)
	return event
}

func interruptEvent(ctx context.Context, info *IncidentInterruptInfo, content string) *adk.AgentEvent {
	event := adk.Interrupt(ctx, info)
	event.Output = assistantEvent(content).Output
	return event
}

func formatReasons(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	return strings.Join(reasons, "; ")
}

func boolFromAny(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		switch s {
		case "true", "1", "yes", "y", "ok", "approved", "confirm", "confirmed", "resolved", "done":
			return true, true
		case "false", "0", "no", "n", "reject", "rejected", "unresolved", "pending":
			return false, true
		}
	}
	return false, false
}

func parseResumeDecision(data any) (approved bool, resolved bool, comment string) {
	switch v := data.(type) {
	case nil:
		return false, false, ""
	case string:
		text := strings.TrimSpace(v)
		lower := strings.ToLower(text)
		if strings.Contains(lower, "确认") || strings.Contains(lower, "resolved") ||
			strings.Contains(lower, "fixed") || strings.Contains(lower, "yes") {
			return true, true, text
		}
		if strings.Contains(lower, "未解决") || strings.Contains(lower, "retry") || strings.Contains(lower, "replan") {
			return false, false, text
		}
		return false, false, text
	case map[string]any:
		if b, ok := boolFromAny(v["approved"]); ok {
			approved = b
		}
		if b, ok := boolFromAny(v["resolved"]); ok {
			resolved = b
		}
		if msg, ok := v["comment"].(string); ok {
			comment = strings.TrimSpace(msg)
		}
		return approved, resolved, comment
	case map[string]string:
		tmp := make(map[string]any, len(v))
		for k, val := range v {
			tmp[k] = val
		}
		return parseResumeDecision(tmp)
	default:
		return false, false, fmt.Sprintf("%v", v)
	}
}
