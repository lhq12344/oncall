package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
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
		Desc: "在沙盒环境中执行单个步骤。命令必须通过白名单验证，支持超时控制。",
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

	// 默认超时 30 秒
	if in.Timeout <= 0 {
		in.Timeout = 30
	}

	// 1. 白名单验证
	if !t.whitelist.AllowedCommands[in.Command] {
		return "", fmt.Errorf("command not in whitelist: %s", in.Command)
	}

	// 2. 参数安全检查
	if err := t.validateArgs(in.Args); err != nil {
		return "", fmt.Errorf("unsafe arguments: %w", err)
	}

	// 3. 演练模式
	if in.DryRun {
		result := &ExecutionResult{
			StepID:     in.StepID,
			Success:    true,
			Output:     fmt.Sprintf("[DRY RUN] Would execute: %s %s", in.Command, strings.Join(in.Args, " ")),
			ExitCode:   0,
			Duration:   0,
			ExecutedAt: time.Now().Format("2006-01-02 15:04:05"),
		}

		output, _ := json.Marshal(result)
		return string(output), nil
	}

	// 4. 执行命令
	result := t.executeCommand(ctx, in.StepID, in.Command, in.Args, in.Timeout)

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

// executeCommand 执行命令
func (t *ExecuteStepTool) executeCommand(ctx context.Context, stepID int, command string, args []string, timeoutSec int) *ExecutionResult {
	start := time.Now()

	// 创建带超时的上下文
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// 创建命令
	cmd := exec.CommandContext(execCtx, command, args...)

	// 执行命令
	output, err := cmd.CombinedOutput()
	duration := time.Since(start).Milliseconds()

	result := &ExecutionResult{
		StepID:     stepID,
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
