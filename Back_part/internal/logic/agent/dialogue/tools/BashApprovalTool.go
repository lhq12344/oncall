package tools

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

const (
	defaultBashTimeoutSeconds = 15
	maxBashTimeoutSeconds     = 90
	maxBashOutputRunes        = 8000
)

type bashApprovalArgs struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Script  string   `json:"script"`
	Timeout int      `json:"timeout"`
	Reason  string   `json:"reason"`
}

var readOnlyBashCommands = map[string]struct{}{
	"ls":         {},
	"cat":        {},
	"tail":       {},
	"head":       {},
	"grep":       {},
	"ps":         {},
	"top":        {},
	"free":       {},
	"df":         {},
	"du":         {},
	"uptime":     {},
	"date":       {},
	"echo":       {},
	"netstat":    {},
	"ss":         {},
	"journalctl": {},
}

// 需要将BashApprovalInterruptInfo 注册到 gob 中，以支持 Eino 的工具中断机制正确序列化和反序列化审批信息。
func init() {
	gob.Register(&BashApprovalInterruptInfo{})
}

// BashApprovalTool 为对话 Agent 提供「执行前人工确认」的 Bash 命令执行能力。
type BashApprovalTool struct {
	logger          *zap.Logger
	allowedCommands map[string]struct{}
}

// BashApprovalInterruptInfo 定义中断时返回给前端的审批信息。
type BashApprovalInterruptInfo struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Script  string   `json:"script,omitempty"`
	Timeout int      `json:"timeout"`
	Reason  string   `json:"reason,omitempty"`
}

func (i *BashApprovalInterruptInfo) String() string {
	if i == nil {
		return "检测到高风险操作，等待用户确认。"
	}
	commandLine := strings.TrimSpace(i.Command + " " + strings.Join(i.Args, " "))
	if strings.TrimSpace(i.Script) != "" {
		commandLine = strings.TrimSpace(i.Script)
	}
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
	Script     string   `json:"script,omitempty"`
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
	allowed := make(map[string]struct{}, len(readOnlyBashCommands))
	for command := range readOnlyBashCommands {
		allowed[command] = struct{}{}
	}

	return &BashApprovalTool{
		logger:          logger,
		allowedCommands: allowed,
	}
}

func (t *BashApprovalTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "bash_execute_with_approval",
		Desc: "执行结构化只读诊断命令（执行前会触发中断并等待用户审批）。不支持自由 shell 脚本、写操作或外部网络请求。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"command": {
				Type:     schema.String,
				Desc:     "要执行的只读命令（白名单内）",
				Required: true,
			},
			"args": {
				Type:     schema.Array,
				ElemInfo: &schema.ParameterInfo{Type: schema.String},
				Desc:     "命令参数数组",
				Required: false,
			},
			"script": {
				Type:     schema.String,
				Desc:     "已禁用。请使用 command 和 args 传入结构化只读命令",
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

// InvokableRun 执行已通过白名单校验的命令并返回结构化结果。
// 中断/审批逻辑由 ApprovalMiddleware 在调用此方法前处理，此处仅负责纯执行。
func (t *BashApprovalTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	in, err := parseBashApprovalArgs(argumentsInJSON)
	if err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	in.Command = strings.TrimSpace(in.Command)
	in.Script = strings.TrimSpace(in.Script)
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Command == "" {
		return "", fmt.Errorf("command is required")
	}
	if in.Script != "" {
		return "", fmt.Errorf("script execution is disabled; use command and args")
	}
	if strings.ContainsAny(in.Command, " \t\r\n") {
		return "", fmt.Errorf("command must be a single executable name")
	}
	if in.Timeout <= 0 {
		in.Timeout = defaultBashTimeoutSeconds
	}
	if in.Timeout > maxBashTimeoutSeconds {
		in.Timeout = maxBashTimeoutSeconds
	}

	if _, ok := t.allowedCommands[in.Command]; !ok {
		return "", fmt.Errorf("command not in whitelist: %s", in.Command)
	}
	if err := t.validateArgs(in.Args); err != nil {
		return "", err
	}

	result := t.executeCommand(ctx, in.Command, in.Args, in.Timeout)
	result.Approved = true
	result.Executed = true

	if t.logger != nil {
		t.logger.Info("bash command executed",
			zap.String("command", in.Command),
			zap.Int("args_count", len(in.Args)),
			zap.Bool("success", result.Success),
			zap.Int("duration_ms", result.DurationMS))
	}
	return marshalBashExecuteResult(result)
}

func parseBashApprovalArgs(payload string) (bashApprovalArgs, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "{}" || strings.EqualFold(payload, "null") {
		return bashApprovalArgs{}, nil
	}

	var in bashApprovalArgs
	if err := json.Unmarshal([]byte(payload), &in); err == nil {
		return in, nil
	}

	decoder := json.NewDecoder(strings.NewReader(payload))
	merged := bashApprovalArgs{}
	decodedAny := false
	for {
		var next bashApprovalArgs
		err := decoder.Decode(&next)
		if err == nil {
			mergeBashApprovalArgs(&merged, next)
			decodedAny = true
			continue
		}
		if err.Error() == "EOF" {
			break
		}
		if !decodedAny {
			return bashApprovalArgs{}, err
		}

		remaining := strings.TrimSpace(readDecoderRemainder(decoder))
		if remaining == "" {
			break
		}
		return bashApprovalArgs{}, err
	}
	if !decodedAny {
		return bashApprovalArgs{}, fmt.Errorf("empty arguments")
	}
	return merged, nil
}

func mergeBashApprovalArgs(target *bashApprovalArgs, next bashApprovalArgs) {
	if target == nil {
		return
	}
	if strings.TrimSpace(next.Command) != "" {
		target.Command = next.Command
	}
	if len(next.Args) > 0 {
		target.Args = next.Args
	}
	if strings.TrimSpace(next.Script) != "" {
		target.Script = next.Script
	}
	if next.Timeout > 0 {
		target.Timeout = next.Timeout
	}
	if strings.TrimSpace(next.Reason) != "" {
		target.Reason = next.Reason
	}
}

func readDecoderRemainder(decoder *json.Decoder) string {
	if decoder == nil {
		return ""
	}
	buffered := decoder.Buffered()
	if buffered == nil {
		return ""
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(buffered)
	return buf.String()
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

func parseBashApprovalDecision(data map[string]any) (approved bool, resolved bool, comment string) {
	if data == nil {
		return false, false, ""
	}
	if b, ok := boolFromBashAny(data["approved"]); ok {
		approved = b
	}
	if b, ok := boolFromBashAny(data["resolved"]); ok {
		resolved = b
	}
	if msg, ok := data["comment"].(string); ok {
		comment = strings.TrimSpace(msg)
	}
	return approved, resolved, comment
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
