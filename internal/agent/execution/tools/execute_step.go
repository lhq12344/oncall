package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// ExecuteStepTool 执行步骤工具
type ExecuteStepTool struct {
	logger    *zap.Logger
	whitelist *CommandWhitelist
}

// CommandWhitelist 命令白名单
type CommandWhitelist struct {
	AllowedCommands map[string]bool
}

// ExecutionResult 执行结果
type ExecutionResult struct {
	StepID     int    `json:"step_id"`
	Success    bool   `json:"success"`
	Skipped    bool   `json:"skipped,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Command    string `json:"command,omitempty"`
	Output     string `json:"output"`
	Error      string `json:"error"`
	ExitCode   int    `json:"exit_code"`
	Duration   int    `json:"duration"` // 毫秒
	ExecutedAt string `json:"executed_at"`
}

func NewExecuteStepTool(logger *zap.Logger) tool.BaseTool {
	// 初始化命令白名单
	whitelist := &CommandWhitelist{
		AllowedCommands: map[string]bool{
			"bash":      true,
			"kubectl":   true,
			"systemctl": true,
			"docker":    true,
			"echo":      true,
			"cat":       true,
			"ls":        true,
			"ps":        true,
			"curl":      true,
			"wget":      true,
			"ping":      true,
			"netstat":   true,
			"ss":        true,
			"top":       true,
			"df":        true,
			"du":        true,
			"free":      true,
			"uptime":    true,
		},
	}

	return &ExecuteStepTool{
		logger:    logger,
		whitelist: whitelist,
	}
}

func (t *ExecuteStepTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "execute_step",
		Desc: "执行单个步骤。命令必须通过白名单验证，支持超时控制。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"step_id": {
				Type:     schema.Integer,
				Desc:     "步骤 ID",
				Required: true,
			},
			"command": {
				Type:     schema.String,
				Desc:     "要执行的命令",
				Required: true,
			},
			"args": {
				Type:     schema.Array,
				Desc:     "命令参数数组",
				Required: false,
			},
			"script": {
				Type:     schema.String,
				Desc:     "Bash 脚本/整行命令（command=bash 时使用）",
				Required: false,
			},
			"timeout": {
				Type:     schema.Integer,
				Desc:     "超时时间（秒），默认 30",
				Required: false,
			},
			"dry_run": {
				Type:     schema.Boolean,
				Desc:     "是否为演练模式（不实际执行），默认 false",
				Required: false,
			},
		}),
	}, nil
}

func (t *ExecuteStepTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		StepID  int      `json:"step_id"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
		Script  string   `json:"script"`
		Timeout int      `json:"timeout"`
		DryRun  bool     `json:"dry_run"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	in.Command = strings.TrimSpace(in.Command)
	if in.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	// 兼容“上游报告直接给整行命令”场景：当 command 包含空格且未提供 args 时，自动切换到 bash -lc。
	if strings.TrimSpace(in.Script) == "" && len(in.Args) == 0 && strings.Contains(in.Command, " ") {
		in.Script = in.Command
		in.Command = "bash"
	}

	in.Script = strings.TrimSpace(in.Script)
	isBashMode := strings.EqualFold(in.Command, "bash")
	if isBashMode && in.Script == "" && len(in.Args) > 0 {
		// 兼容旧调用：command=bash, args=["kubectl get pods -n infra"]。
		in.Script = strings.TrimSpace(strings.Join(in.Args, " "))
		in.Args = nil
	}
	if isBashMode && in.Script == "" {
		return "", fmt.Errorf("script is required when command is bash")
	}
	if !isBashMode && in.Script != "" {
		return "", fmt.Errorf("script only supported when command is bash")
	}

	// 默认超时 30 秒
	if in.Timeout <= 0 {
		in.Timeout = 30
	}

	// 强制执行顺序：必须先生成包含 command/args/expected/rollback 的计划，再执行步骤。
	if ok, _ := hasPreparedExecutionPlan(ctx); !ok {
		return "", fmt.Errorf("execution plan not prepared: call generate_plan first with intent/context before execute_step")
	}
	if ok, _ := hasValidatedExecutionPlan(ctx); !ok {
		return "", fmt.Errorf("execution plan not validated: call validate_plan after normalize_plan/generate_plan before execute_step")
	}

	// 若前置步骤已验证通过或连续失败触发收敛，直接跳过后续执行。
	if shouldSkip, reason := shouldSkipExecutionStep(ctx, in.StepID); shouldSkip {
		result := &ExecutionResult{
			StepID:     in.StepID,
			Success:    false,
			Skipped:    true,
			Mode:       modeLabel(isBashMode),
			Command:    renderedCommand(in.Command, in.Args, in.Script),
			Output:     "",
			Error:      reason,
			ExitCode:   -2,
			Duration:   0,
			ExecutedAt: time.Now().Format("2006-01-02 15:04:05"),
		}
		output, _ := json.Marshal(result)
		if t.logger != nil {
			t.logger.Info("step skipped",
				zap.Int("step_id", in.StepID),
				zap.String("command", in.Command),
				zap.String("reason", reason))
		}
		return string(output), nil
	}

	// 1. 白名单验证
	if !t.whitelist.AllowedCommands[in.Command] {
		return "", fmt.Errorf("command not in whitelist: %s", in.Command)
	}

	// 2. 参数安全检查
	if isBashMode {
		if err := t.validateScript(in.Script); err != nil {
			return "", fmt.Errorf("unsafe script: %w", err)
		}
	} else {
		if err := t.validateArgs(in.Args); err != nil {
			return "", fmt.Errorf("unsafe arguments: %w", err)
		}
	}

	// 3. 演练模式
	if in.DryRun {
		rendered := renderedCommand(in.Command, in.Args, in.Script)
		result := &ExecutionResult{
			StepID:     in.StepID,
			Success:    true,
			Mode:       modeLabel(isBashMode),
			Command:    rendered,
			Output:     fmt.Sprintf("[DRY RUN] Would execute: %s", rendered),
			ExitCode:   0,
			Duration:   0,
			ExecutedAt: time.Now().Format("2006-01-02 15:04:05"),
		}

		output, _ := json.Marshal(result)
		return string(output), nil
	}

	// 4. 执行命令
	result := t.executeCommand(ctx, in.StepID, in.Command, in.Args, in.Script, in.Timeout)

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("step executed",
			zap.Int("step_id", in.StepID),
			zap.String("command", in.Command),
			zap.Bool("success", result.Success),
			zap.Int("duration_ms", result.Duration))
	}

	return string(output), nil
}

// validateArgs 验证参数安全性
func (t *ExecuteStepTool) validateArgs(args []string) error {
	// 危险模式检测
	dangerousPatterns := []string{
		";",      // 命令注入
		"&&",     // 命令链
		"||",     // 命令链
		"|",      // 管道（可能绕过限制）
		"`",      // 命令替换
		"$(",     // 命令替换
		">",      // 重定向
		"<",      // 重定向
		"rm -rf", // 危险删除
	}

	for _, arg := range args {
		for _, pattern := range dangerousPatterns {
			if strings.Contains(arg, pattern) {
				return fmt.Errorf("dangerous pattern detected: %s", pattern)
			}
		}
	}

	return nil
}

// validateScript 校验 Bash 脚本安全性（只拦截高危 destructive 指令）。
// 输入：script 字符串。
// 输出：校验错误（安全时为 nil）。
func (t *ExecuteStepTool) validateScript(script string) error {
	script = strings.TrimSpace(script)
	if script == "" {
		return fmt.Errorf("script is empty")
	}
	unsafeRules := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\brm\s+-rf\s+/`),
		regexp.MustCompile(`(?i)\bmkfs\b`),
		regexp.MustCompile(`(?i)\bdd\s+if=`),
		regexp.MustCompile(`(?i)\bshutdown\b|\breboot\b`),
		regexp.MustCompile(`:\(\)\s*\{\s*:\|:&\s*\};:`),
	}
	for _, rule := range unsafeRules {
		if rule.MatchString(script) {
			return fmt.Errorf("dangerous script pattern detected: %s", rule.String())
		}
	}
	return nil
}

// executeCommand 执行命令
func (t *ExecuteStepTool) executeCommand(ctx context.Context, stepID int, command string, args []string, script string, timeoutSec int) *ExecutionResult {
	start := time.Now()

	// 创建带超时的上下文
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	isBashMode := strings.EqualFold(command, "bash")
	rendered := renderedCommand(command, args, script)

	// 创建命令
	var cmd *exec.Cmd
	if isBashMode {
		cmd = exec.CommandContext(execCtx, "bash", "-lc", script)
	} else {
		cmd = exec.CommandContext(execCtx, command, args...)
	}

	// 执行命令
	output, err := cmd.CombinedOutput()
	duration := time.Since(start).Milliseconds()

	result := &ExecutionResult{
		StepID:     stepID,
		Mode:       modeLabel(isBashMode),
		Command:    rendered,
		Output:     string(output),
		Duration:   int(duration),
		ExecutedAt: time.Now().Format("2006-01-02 15:04:05"),
	}

	if err != nil {
		result.Success = false
		result.Error = err.Error()

		// 获取退出码
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	} else {
		result.Success = true
		result.ExitCode = 0
	}

	return result
}

// modeLabel 返回执行模式名称。
// 输入：是否 bash 模式。
// 输出：模式名（bash 或 direct）。
func modeLabel(isBash bool) string {
	if isBash {
		return "bash"
	}
	return "direct"
}

// renderedCommand 渲染可读命令文本。
// 输入：command、args、script。
// 输出：最终展示命令。
func renderedCommand(command string, args []string, script string) string {
	if strings.EqualFold(strings.TrimSpace(command), "bash") {
		return strings.TrimSpace(script)
	}
	return strings.TrimSpace(command + " " + strings.Join(args, " "))
}
