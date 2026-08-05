package tools

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go_agent/internal/permissions"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

const (
	defaultBashTimeoutSeconds = 15
	maxBashTimeoutSeconds     = 90
	maxBashOutputRunes        = 8000
)

// 需要将BashApprovalInterruptInfo 注册到 gob 中，以支持 Eino 的工具中断机制正确序列化和反序列化审批信息。
func init() {
	gob.Register(&BashApprovalInterruptInfo{})
}

// BashApprovalTool 为对话 Agent 提供 Bash 命令执行能力：
// 只读命令可直接执行，变更/高风险命令执行前必须人工确认。
type BashApprovalTool struct {
	logger  *zap.Logger
	checker *permissions.Checker
}

// BashApprovalInterruptInfo 定义中断时返回给前端的审批信息。
type BashApprovalInterruptInfo struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Timeout int      `json:"timeout"`
	Reason  string   `json:"reason,omitempty"`
}

func (i *BashApprovalInterruptInfo) String() string {
	if i == nil {
		return "检测到高风险操作，等待用户确认。"
	}
	commandLine := strings.TrimSpace(i.Command + " " + strings.Join(i.Args, " "))
	if commandLine == "" {
		commandLine = "(empty)"
	}
	if i.Reason != "" {
		return fmt.Sprintf("待执行命令：%s；超时：%ds；执行原因：%s。请确认是否继续。", commandLine, i.Timeout, i.Reason)
	}
	return fmt.Sprintf("待执行命令：%s；超时：%ds。请确认是否继续。", commandLine, i.Timeout)
}

// BashExecuteResult 定义命令执行后的结构化结果。
type BashExecuteResult struct {
	Approved   bool     `json:"approved"`
	Resolved   bool     `json:"resolved"`
	Executed   bool     `json:"executed"`
	Success    bool     `json:"success"`
	Command    string   `json:"command"`
	Args       []string `json:"args,omitempty"`
	Timeout    int      `json:"timeout"`
	Output     string   `json:"output,omitempty"`
	Error      string   `json:"error,omitempty"`
	ExitCode   int      `json:"exit_code"`
	DurationMS int      `json:"duration_ms"`
	Comment    string   `json:"comment,omitempty"`
}

// NewBashApprovalTool 创建执行前需要人工确认的 Bash 工具。
// 输入：logger（可为空）。
// 输出：可注册到 Eino Agent 的 tool.BaseTool。
func NewBashApprovalTool(logger *zap.Logger) tool.BaseTool {
	return &BashApprovalTool{
		logger:  logger,
		checker: permissions.NewChecker(permissions.Options{}),
	}
}

func (t *BashApprovalTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "bash_execute_with_approval",
		Desc: "执行 Bash 命令：只读命令直接执行，变更或高风险命令会触发中断并等待用户审批。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"command": {
				Type:     schema.String,
				Desc:     "要执行的命令（白名单内）",
				Required: true,
			},
			"args": {
				Type:     schema.Array,
				ElemInfo: &schema.ParameterInfo{Type: schema.String},
				Desc:     "命令参数数组",
				Required: false,
			},
			"timeout": {
				Type:     schema.Integer,
				Desc:     "命令超时时间（秒），默认 15，最大 90",
				Required: false,
			},
			"reason": {
				Type:     schema.String,
				Desc:     "执行命令原因（用于审批说明）",
				Required: false,
			},
		}),
	}, nil
}

func (t *BashApprovalTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
		Timeout int      `json:"timeout"`
		Reason  string   `json:"reason"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	in.Command = strings.TrimSpace(in.Command)
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Command == "" {
		return "", fmt.Errorf("command is required")
	}
	if in.Timeout <= 0 {
		in.Timeout = defaultBashTimeoutSeconds
	}
	if in.Timeout > maxBashTimeoutSeconds {
		in.Timeout = maxBashTimeoutSeconds
	}

	permArgs := bashPermissionArgs(in.Command, in.Args)
	decision := t.permissionChecker().Check("bash_execute_with_approval", permArgs)
	if decision.Effect == permissions.Deny {
		result := BashExecuteResult{
			Approved: false,
			Resolved: false,
			Executed: false,
			Success:  false,
			Command:  in.Command,
			Args:     in.Args,
			Timeout:  in.Timeout,
			Error:    "permission denied: " + decision.Reason,
			ExitCode: -2,
		}
		return marshalBashExecuteResult(result)
	}

	if decision.Effect == permissions.Allow {
		result := t.executeCommand(ctx, in.Command, in.Args, in.Timeout)
		result.Approved = true
		result.Resolved = false
		result.Executed = true
		if t.logger != nil {
			t.logger.Info("dialogue bash command auto executed",
				zap.String("command", in.Command),
				zap.Int("args_count", len(in.Args)),
				zap.Bool("success", result.Success),
				zap.Int("duration_ms", result.DurationMS))
		}
		return marshalBashExecuteResult(result)
	}

	// 首次执行：仅变更/高风险命令触发中断，等待前端通过 chat_resume_stream 提交审批结果。
	wasInterrupted, _, _ := tool.GetInterruptState[any](ctx)
	if !wasInterrupted {
		return "", tool.Interrupt(ctx, &BashApprovalInterruptInfo{
			Command: in.Command,
			Args:    in.Args,
			Timeout: in.Timeout,
			Reason:  firstNonEmptyBash(in.Reason, decision.Reason),
		})
	}

	// 恢复执行：仅当当前工具是 Resume 目标，且携带了审批数据，才允许继续。
	isResumeTarget, hasData, resumeData := tool.GetResumeContext[map[string]any](ctx)
	if !isResumeTarget || !hasData {
		return "", tool.Interrupt(ctx, &BashApprovalInterruptInfo{
			Command: in.Command,
			Args:    in.Args,
			Timeout: in.Timeout,
			Reason:  firstNonEmptyBash(in.Reason, decision.Reason),
		})
	}

	approved, resolved, allowAlways, comment := parseBashApprovalDecision(resumeData)

	// 用户标记“已修复”时，按业务语义直接跳过命令执行并返回说明。
	if resolved {
		result := BashExecuteResult{
			Approved: true,
			Resolved: true,
			Executed: false,
			Success:  true,
			Command:  in.Command,
			Args:     in.Args,
			Timeout:  in.Timeout,
			ExitCode: 0,
			Comment:  comment,
		}
		return marshalBashExecuteResult(result)
	}

	// 用户未批准时，不执行命令，直接返回拒绝结果。
	if !approved {
		result := BashExecuteResult{
			Approved: false,
			Resolved: false,
			Executed: false,
			Success:  false,
			Command:  in.Command,
			Args:     in.Args,
			Timeout:  in.Timeout,
			Error:    "command execution rejected by user",
			ExitCode: -1,
			Comment:  comment,
		}
		return marshalBashExecuteResult(result)
	}

	if allowAlways {
		if err := t.permissionChecker().AllowAlways("bash_execute_with_approval", permArgs); err != nil && t.logger != nil {
			t.logger.Warn("failed to persist dialogue bash allow-always permission", zap.Error(err))
		}
	}

	result := t.executeCommand(ctx, in.Command, in.Args, in.Timeout)
	result.Approved = true
	result.Resolved = false
	result.Executed = true
	result.Comment = comment

	if t.logger != nil {
		t.logger.Info("dialogue bash command executed",
			zap.String("command", in.Command),
			zap.Int("args_count", len(in.Args)),
			zap.Bool("success", result.Success),
			zap.Int("duration_ms", result.DurationMS))
	}
	return marshalBashExecuteResult(result)
}

// validateArgs 执行参数安全校验。
// 输入：args 命令参数列表。
// 输出：校验错误（若安全返回 nil）。
func (t *BashApprovalTool) validateArgs(args []string) error {
	dangerousFragments := []string{
		";", "&&", "||", "|", "`", "$(", ">", "<", "\n", "\r",
		"rm -rf", "mkfs", "shutdown", "reboot",
	}

	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		for _, fragment := range dangerousFragments {
			if strings.Contains(trimmed, fragment) {
				return fmt.Errorf("unsafe argument detected: %s", fragment)
			}
		}
	}
	return nil
}

func (t *BashApprovalTool) permissionChecker() *permissions.Checker {
	if t.checker == nil {
		t.checker = permissions.NewChecker(permissions.Options{})
	}
	return t.checker
}

func bashPermissionArgs(command string, args []string) map[string]any {
	return map[string]any{
		"command": strings.TrimSpace(command),
		"args":    append([]string(nil), args...),
	}
}

func firstNonEmptyBash(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (t *BashApprovalTool) requiresApproval(command string, args []string) bool {
	command = normalizeBashToken(command)

	switch command {
	case "ls", "cat", "tail", "head", "grep", "ps", "top", "free", "df", "du", "uptime", "date", "echo", "ping", "netstat", "ss":
		return false
	case "journalctl":
		return journalctlRequiresApproval(args)
	case "kubectl":
		return !isReadOnlyKubectl(args)
	case "docker":
		return !isReadOnlyDocker(args)
	case "systemctl":
		return !isReadOnlySystemctl(args)
	default:
		return true
	}
}

func journalctlRequiresApproval(args []string) bool {
	for _, arg := range args {
		normalized := normalizeBashToken(arg)
		if normalized == "--rotate" || normalized == "--flush" || normalized == "--sync" {
			return true
		}
		if strings.HasPrefix(normalized, "--vacuum-") {
			return true
		}
	}
	return false
}

func isReadOnlyKubectl(args []string) bool {
	tokens := normalizeBashTokens(args)
	if len(tokens) == 0 {
		return true
	}

	readOnlyVerbs := map[string]struct{}{
		"get":           {},
		"describe":      {},
		"logs":          {},
		"top":           {},
		"api-resources": {},
		"api-versions":  {},
		"cluster-info":  {},
		"version":       {},
		"explain":       {},
		"events":        {},
	}
	groupedReadOnlyVerbs := map[string]map[string]struct{}{
		"auth": {
			"can-i": {},
		},
		"config": {
			"current-context": {},
			"get-contexts":    {},
			"view":            {},
		},
		"rollout": {
			"history": {},
			"status":  {},
		},
	}
	mutatingVerbs := map[string]struct{}{
		"annotate":     {},
		"apply":        {},
		"attach":       {},
		"autoscale":    {},
		"cordon":       {},
		"cp":           {},
		"create":       {},
		"delete":       {},
		"drain":        {},
		"edit":         {},
		"exec":         {},
		"expose":       {},
		"label":        {},
		"patch":        {},
		"port-forward": {},
		"replace":      {},
		"rollout":      {},
		"run":          {},
		"scale":        {},
		"set":          {},
		"taint":        {},
		"uncordon":     {},
	}

	for index, token := range tokens {
		if _, ok := readOnlyVerbs[token]; ok {
			return true
		}
		if subs, ok := groupedReadOnlyVerbs[token]; ok {
			if index+1 < len(tokens) {
				_, ok = subs[tokens[index+1]]
				return ok
			}
			return false
		}
		if _, ok := mutatingVerbs[token]; ok {
			if token == "rollout" {
				if index+1 < len(tokens) {
					_, ok := groupedReadOnlyVerbs[token][tokens[index+1]]
					return ok
				}
			}
			return false
		}
	}

	return false
}

func isReadOnlyDocker(args []string) bool {
	tokens := normalizeBashTokens(args)
	if len(tokens) == 0 {
		return true
	}

	readOnlyTopLevel := map[string]struct{}{
		"images":  {},
		"info":    {},
		"inspect": {},
		"logs":    {},
		"ps":      {},
		"stats":   {},
		"version": {},
	}
	readOnlyGrouped := map[string]map[string]struct{}{
		"container": {
			"inspect": {},
			"logs":    {},
			"ls":      {},
		},
		"image": {
			"history": {},
			"inspect": {},
			"ls":      {},
		},
		"network": {
			"inspect": {},
			"ls":      {},
		},
		"system": {
			"df": {},
		},
		"volume": {
			"inspect": {},
			"ls":      {},
		},
	}

	for index, token := range tokens {
		if _, ok := readOnlyTopLevel[token]; ok {
			return true
		}
		if subs, ok := readOnlyGrouped[token]; ok {
			if index+1 < len(tokens) {
				_, ok = subs[tokens[index+1]]
				return ok
			}
			return false
		}
	}

	return false
}

func isReadOnlySystemctl(args []string) bool {
	tokens := normalizeBashTokens(args)
	if len(tokens) == 0 {
		return true
	}

	readOnlyVerbs := map[string]struct{}{
		"cat":               {},
		"is-active":         {},
		"is-enabled":        {},
		"is-failed":         {},
		"list-dependencies": {},
		"list-unit-files":   {},
		"list-units":        {},
		"show":              {},
		"status":            {},
	}

	for _, token := range tokens {
		if _, ok := readOnlyVerbs[token]; ok {
			return true
		}
	}
	return false
}

func normalizeBashTokens(args []string) []string {
	tokens := make([]string, 0, len(args))
	for _, arg := range args {
		token := normalizeBashToken(arg)
		if token == "" || strings.HasPrefix(token, "-") {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func normalizeBashToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// executeCommand 执行单条命令并返回结构化结果。
// 输入：ctx、command、args、timeoutSec。
// 输出：BashExecuteResult（无论成功失败都返回结构化数据）。
func (t *BashApprovalTool) executeCommand(ctx context.Context, command string, args []string, timeoutSec int) BashExecuteResult {
	start := time.Now()
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, command, args...)
	output, err := cmd.CombinedOutput()
	duration := int(time.Since(start).Milliseconds())

	result := BashExecuteResult{
		Success:    err == nil,
		Command:    command,
		Args:       args,
		Timeout:    timeoutSec,
		Output:     truncateRunes(string(output), maxBashOutputRunes),
		ExitCode:   0,
		DurationMS: duration,
	}

	if err != nil {
		result.Error = err.Error()
		result.Success = false
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}

	return result
}

func marshalBashExecuteResult(result BashExecuteResult) (string, error) {
	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal bash result: %w", err)
	}
	return string(out), nil
}

func truncateRunes(text string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "\n... (truncated)"
}

func parseBashApprovalDecision(data map[string]any) (approved bool, resolved bool, allowAlways bool, comment string) {
	if data == nil {
		return false, false, false, ""
	}
	if b, ok := boolFromBashAny(data["approved"]); ok {
		approved = b
	}
	if b, ok := boolFromBashAny(data["resolved"]); ok {
		resolved = b
	}
	for _, key := range []string{"always_allow", "allow_always", "remember", "dont_ask_again"} {
		if b, ok := boolFromBashAny(data[key]); ok {
			allowAlways = b
			break
		}
	}
	if msg, ok := data["comment"].(string); ok {
		comment = strings.TrimSpace(msg)
	}
	return approved, resolved, allowAlways, comment
}

func boolFromBashAny(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		normalized := strings.ToLower(strings.TrimSpace(v))
		switch normalized {
		case "true", "1", "yes", "y", "ok", "approved", "confirm", "confirmed", "resolved", "done":
			return true, true
		case "false", "0", "no", "n", "reject", "rejected", "unresolved", "pending":
			return false, true
		}
	}
	return false, false
}
