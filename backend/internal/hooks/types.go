package hooks

import "time"

// EventName identifies an OnCall lifecycle event that hook rules can observe.
type EventName string

const (
	EventSessionStart    EventName = "session_start"
	EventSessionEnd      EventName = "session_end"
	EventTurnStart       EventName = "turn_start"
	EventTurnEnd         EventName = "turn_end"
	EventAgentStart      EventName = "agent_start"
	EventAgentEnd        EventName = "agent_end"
	EventAgentError      EventName = "agent_error"
	EventToolPreUse      EventName = "tool_pre_use"
	EventToolPostUse     EventName = "tool_post_use"
	EventToolError       EventName = "tool_error"
	EventApprovalRequest EventName = "approval_requested"
	EventResumeRequest   EventName = "resume_requested"
)

// ActionType is intentionally limited to safe observation/audit actions.
// Arbitrary command execution is kept as an unsupported type so configuration
// mistakes fail closed instead of creating a permission bypass.
type ActionType string

const (
	ActionLog     ActionType = "log"
	ActionMessage ActionType = "message"
	ActionWebhook ActionType = "webhook"
	ActionAudit   ActionType = "audit"
	ActionCommand ActionType = "command"
)

// Action describes the side effect a hook performs when it matches.
type Action struct {
	Type      ActionType        `json:"type" yaml:"type"`
	Message   string            `json:"message,omitempty" yaml:"message,omitempty"`
	URL       string            `json:"url,omitempty" yaml:"url,omitempty"`
	Method    string            `json:"method,omitempty" yaml:"method,omitempty"`
	Headers   map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body      string            `json:"body,omitempty" yaml:"body,omitempty"`
	Timeout   time.Duration     `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	TimeoutMS int               `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	MaxBytes  int               `json:"max_bytes,omitempty" yaml:"max_bytes,omitempty"`
}

// Hook is a single rule. Reject only has effect for tool_pre_use hooks.
type Hook struct {
	ID           string    `json:"id" yaml:"id"`
	Event        EventName `json:"event" yaml:"event"`
	Condition    string    `json:"if,omitempty" yaml:"if,omitempty"`
	Action       Action    `json:"action" yaml:"action"`
	Reject       bool      `json:"reject,omitempty" yaml:"reject,omitempty"`
	RejectReason string    `json:"reject_reason,omitempty" yaml:"reject_reason,omitempty"`
	Once         bool      `json:"once,omitempty" yaml:"once,omitempty"`
	Async        bool      `json:"async,omitempty" yaml:"async,omitempty"`
	OnError      string    `json:"on_error,omitempty" yaml:"on_error,omitempty"`
	Enabled      *bool     `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

// HookContext is the normalized event payload used by all hook points.
type HookContext struct {
	EventName    EventName      `json:"event"`
	SessionID    string         `json:"session_id,omitempty"`
	CheckpointID string         `json:"checkpoint_id,omitempty"`
	AgentName    string         `json:"agent_name,omitempty"`
	Component    string         `json:"component,omitempty"`
	ToolName     string         `json:"tool_name,omitempty"`
	ToolArgs     map[string]any `json:"tool_args,omitempty"`
	Result       string         `json:"result,omitempty"`
	Error        string         `json:"error,omitempty"`
	Message      string         `json:"message,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type HookResult struct {
	HookID  string    `json:"hook_id"`
	Event   EventName `json:"event"`
	Output  string    `json:"output,omitempty"`
	Success bool      `json:"success"`
	Reject  bool      `json:"reject,omitempty"`
	Async   bool      `json:"async,omitempty"`
}

type Notification struct {
	Result  HookResult  `json:"result"`
	Context HookContext `json:"context"`
	At      time.Time   `json:"at"`
}

type Config struct {
	Enabled             bool     `json:"enabled" yaml:"enabled"`
	Hooks               []Hook   `json:"hooks" yaml:"hooks"`
	WebhookAllowedHosts []string `json:"webhook_allowed_hosts" yaml:"webhook_allowed_hosts"`
	MaxPayloadBytes     int      `json:"max_payload_bytes" yaml:"max_payload_bytes"`
	MaxNotifications    int      `json:"max_notifications" yaml:"max_notifications"`
	DefaultTimeoutMS    int      `json:"default_timeout_ms" yaml:"default_timeout_ms"`
}

func BoolPtr(v bool) *bool { return &v }
