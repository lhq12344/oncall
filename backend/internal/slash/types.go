package slash

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type CommandType string

const (
	TypeLocal        CommandType = "local"
	TypePrompt       CommandType = "prompt"
	TypeOpsWorkflow  CommandType = "ops_workflow"
	TypeClientAction CommandType = "client_action"
	TypeDeferred     CommandType = "deferred"
)

type CommandSource string

const (
	SourceBuiltin   CommandSource = "builtin"
	SourceProject   CommandSource = "project"
	SourceMewCompat CommandSource = "mew_compat"
)

type Message struct {
	Role      string
	Content   string
	Timestamp time.Time
}

type StatusSnapshot struct {
	SessionID           string
	ChatRunnerReady     bool
	OpsRunnerReady      bool
	DialogueAgentReady  bool
	OpsAgentReady       bool
	KnowledgeAgentReady bool
	K8sAvailable        bool
	PrometheusAvailable bool
	ESAvailable         bool
	LoadedCommands      int
	UserCommands        int
	WorkDir             string
	HooksEnabled        bool
	HookRules           int
	HookNotifications   int
}

type Context struct {
	Ctx            context.Context
	Args           string
	SessionID      string
	WorkDir        string
	Registry       *Registry
	Status         func() StatusSnapshot
	RecentMessages func(limit int) []Message
	LastError      func() string
	Now            func() time.Time
}

func (c *Context) currentStatus() StatusSnapshot {
	if c != nil && c.Status != nil {
		return c.Status()
	}
	return StatusSnapshot{SessionID: cSessionID(c), WorkDir: cWorkDir(c)}
}

func cSessionID(c *Context) string {
	if c == nil {
		return ""
	}
	return c.SessionID
}

func cWorkDir(c *Context) string {
	if c == nil {
		return ""
	}
	return c.WorkDir
}

func (c *Context) recentMessages(limit int) []Message {
	if c != nil && c.RecentMessages != nil {
		return c.RecentMessages(limit)
	}
	return nil
}

func (c *Context) lastError() string {
	if c != nil && c.LastError != nil {
		return strings.TrimSpace(c.LastError())
	}
	for _, msg := range c.recentMessages(20) {
		text := strings.TrimSpace(msg.Content)
		lower := strings.ToLower(text)
		if strings.Contains(lower, "error:") || strings.Contains(lower, "[error]") || strings.Contains(lower, "错误") || strings.Contains(lower, "异常") {
			return text
		}
	}
	return ""
}

type Result struct {
	Type     CommandType
	Content  string
	Prompt   string
	Action   string
	Payload  map[string]any
	Persist  bool
	Metadata map[string]any
}

type Handler func(*Context) (Result, error)

type Command struct {
	Name         string
	Description  string
	ArgumentHint string
	Aliases      []string
	Type         CommandType
	Source       CommandSource
	SourcePath   string
	Handler      Handler
	Builtin      bool
}

type CommandInfo struct {
	Name         string      `json:"name"`
	Aliases      []string    `json:"aliases,omitempty"`
	Description  string      `json:"description"`
	ArgumentHint string      `json:"argument_hint,omitempty"`
	Type         CommandType `json:"type"`
	Source       string      `json:"source"`
}

func (c Command) Info() CommandInfo {
	aliases := append([]string(nil), c.Aliases...)
	sort.Strings(aliases)
	return CommandInfo{
		Name:         c.Name,
		Aliases:      aliases,
		Description:  c.Description,
		ArgumentHint: c.ArgumentHint,
		Type:         c.Type,
		Source:       string(c.Source),
	}
}

func normalizeName(name string) string {
	name = strings.TrimSpace(strings.TrimPrefix(name, "/"))
	name = strings.ToLower(name)
	return name
}

func normalizeAlias(alias string) string {
	return normalizeName(alias)
}

func ensureHandler(cmd Command) error {
	if strings.TrimSpace(cmd.Name) == "" {
		return fmt.Errorf("command name is required")
	}
	if cmd.Type == "" {
		return fmt.Errorf("command type is required for %s", cmd.Name)
	}
	if cmd.Handler == nil {
		return fmt.Errorf("handler is required for %s", cmd.Name)
	}
	return nil
}
