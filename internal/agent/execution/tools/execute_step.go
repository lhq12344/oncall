package tools

import (
	"context"
	"encoding/gob"
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

func init() {
	gob.Register(&ExecutionApprovalInterruptInfo{})
}

// ExecuteStepTool 执行步骤工具。
type ExecuteStepTool struct {
	logger    *zap.Logger
	whitelist *CommandWhitelist
}

// CommandWhitelist 命令白名单。
type CommandWhitelist struct {
	AllowedCommands map[string]bool
}

// ExecutionApprovalInterruptInfo 定义 execution_agent 在执行变更类命令前的审批信息。
type ExecutionApprovalInterruptInfo struct {
	StepID     int      `json:"step_id,omitempty"`
	Command    string   `json:"command"`
	Args       []string `json:"args,omitempty"`
	Timeout    int      `json:"timeout"`
	Reason     string   `json:"reason,omitempty"`
	RawCommand string   `json:"raw_command,omitempty"`
}

func (i *ExecutionApprovalInterruptInfo) String() string {
	if i == nil {
		return "检测到会修改资源的执行步骤，等待用户确认。"
	}

	commandLine := strings.TrimSpace(i.RawCommand)
	if commandLine == "" {
		commandLine = strings.TrimSpace(i.Command + " " + strings.Join(i.Args, " "))
	}
	if commandLine == "" {
		commandLine = "(empty)"
	}

	if strings.TrimSpace(i.Reason) != "" {
		return fmt.Sprintf("步骤 %d 待执行变更命令：%s；超时：%ds；原因：%s。请确认是否继续。",
			i.StepID, commandLine, i.Timeout, strings.TrimSpace(i.Reason))
	}
	return fmt.Sprintf("步骤 %d 待执行变更命令：%s；超时：%ds。请确认是否继续。", i.StepID, commandLine, i.Timeout)
}

// ExecutionResult 执行结果。
type ExecutionResult struct {
	StepID     int    `json:"step_id"`
	Success    bool   `json:"success"`
	Skipped    bool   `json:"skipped,omitempty"`
	Approved   bool   `json:"approved,omitempty"`
	Resolved   bool   `json:"resolved,omitempty"`
	Executed   bool   `json:"executed,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Command    string `json:"command,omitempty"`
	Timeout    int    `json:"timeout,omitempty"`
	Output     string `json:"output"`
	Error      string `json:"error"`
	ExitCode   int    `json:"exit_code"`
	Duration   int    `json:"duration"`
	ExecutedAt string `json:"executed_at"`
	Comment    string `json:"comment,omitempty"`
}

type executeStepArgs struct {
	StepID  int      `json:"step_id"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Script  string   `json:"script"`
	Timeout int      `json:"timeout"`
	DryRun  bool     `json:"dry_run"`
}

const maxSameStepCommandAttempts = 2
const maxSameStepExecutionAttempts = 4
const maxExecutionOutputRunes = 800
const defaultExecuteStepTimeoutSeconds = 15

// NewExecuteStepTool 创建执行步骤工具。
// 输入：logger。
// 输出：可注册到 execution_agent 的执行工具。
func NewExecuteStepTool(logger *zap.Logger) tool.BaseTool {
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
		Desc: "执行单个步骤。命令必须通过白名单验证，支持超时控制；若步骤会修改资源，将自动中断并等待用户审批。",
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
				ElemInfo: &schema.ParameterInfo{Type: schema.String},
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
				Desc:     "超时时间（秒），默认 15",
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
	fail := func(err error) (string, error) {
		if err == nil {
			return "", nil
		}
		decision := recordExecutionToolFailure(ctx, "execute_step", err.Error())
		return "", fmt.Errorf("%s", decision.StopReason)
	}

	in, err := parseExecuteStepInput(ctx, argumentsInJSON)
	if err != nil {
		return fail(err)
	}

	in.Command = strings.TrimSpace(in.Command)
	if in.Command == "" {
		return fail(fmt.Errorf("command is required"))
	}

	if strings.TrimSpace(in.Script) == "" && len(in.Args) == 0 && strings.Contains(in.Command, " ") {
		in.Script = in.Command
		in.Command = "bash"
	}

	in.Script = strings.TrimSpace(in.Script)
	isBashMode := strings.EqualFold(in.Command, "bash")
	if isBashMode && in.Script == "" && len(in.Args) > 0 {
		in.Script = strings.TrimSpace(strings.Join(in.Args, " "))
		in.Args = nil
	}
	if isBashMode && in.Script == "" {
		return fail(fmt.Errorf("script is required when command is bash"))
	}
	if !isBashMode && in.Script != "" {
		return fail(fmt.Errorf("script only supported when command is bash"))
	}

	if in.Timeout <= 0 {
		in.Timeout = defaultExecuteStepTimeoutSeconds
	}

	rendered := renderedCommand(in.Command, in.Args, in.Script)

	if ok, _ := hasPreparedExecutionPlan(ctx); !ok {
		return fail(fmt.Errorf("execution plan not prepared: call generate_plan first with intent/context before execute_step"))
	}
	if ok, _ := hasValidatedExecutionPlan(ctx); !ok {
		return fail(fmt.Errorf("execution plan not validated: call validate_plan after normalize_plan/generate_plan before execute_step"))
	}

	if shouldSkip, reason := shouldSkipExecutionStep(ctx, in.StepID); shouldSkip {
		result := &ExecutionResult{
			StepID:     in.StepID,
			Success:    false,
			Skipped:    true,
			Mode:       modeLabel(isBashMode),
			Command:    rendered,
			Timeout:    in.Timeout,
			Output:     "",
			Error:      reason,
			ExitCode:   -2,
			Duration:   0,
			ExecutedAt: time.Now().Format("2006-01-02 15:04:05"),
		}
		if err := rememberExecutionResult(ctx, result); err != nil {
			return "", err
		}
		output, _ := marshalExecutionResult(result)
		if t.logger != nil {
			t.logger.Info("step skipped",
				zap.Int("step_id", in.StepID),
				zap.String("command", in.Command),
				zap.String("reason", reason))
		}
		return output, nil
	}

	if in.StepID > 0 {
		if attemptCount, validated, hit := getStepExecutionStats(ctx, in.StepID); hit && !validated && attemptCount >= maxSameStepExecutionAttempts {
			reason := fmt.Sprintf("步骤 %d 在未完成校验前已执行 %d 次，判定进入重复试错，停止自动执行并建议人工处理或重规划", in.StepID, attemptCount)
			markExecutionStopped(ctx, reason, "manual_required")
			result := &ExecutionResult{
				StepID:     in.StepID,
				Success:    false,
				Skipped:    true,
				Mode:       modeLabel(isBashMode),
				Command:    rendered,
				Timeout:    in.Timeout,
				Output:     "",
				Error:      reason,
				ExitCode:   -4,
				Duration:   0,
				ExecutedAt: time.Now().Format("2006-01-02 15:04:05"),
			}
			if err := rememberExecutionResult(ctx, result); err != nil {
				return "", err
			}
			if t.logger != nil {
				t.logger.Info("step execution blocked due to excessive retries",
					zap.Int("step_id", in.StepID),
					zap.String("command", rendered),
					zap.Int("step_attempt_count", attemptCount),
					zap.String("reason", reason))
			}
			return marshalExecutionResult(result)
		}

		attemptCount, succeeded, lastResult, hit := getStepCommandStats(ctx, in.StepID, rendered)
		if hit && succeeded {
			result := &ExecutionResult{
				StepID:     in.StepID,
				Success:    true,
				Skipped:    true,
				Mode:       modeLabel(isBashMode),
				Command:    rendered,
				Timeout:    in.Timeout,
				Output:     "",
				Error:      "同一步骤同一命令已成功执行，忽略重复调用，请继续 validate_result 或执行下一步",
				ExitCode:   0,
				Duration:   0,
				ExecutedAt: time.Now().Format("2006-01-02 15:04:05"),
			}
			if lastResult != nil {
				result.Output = lastResult.Output
				if lastResult.ExitCode != 0 {
					result.ExitCode = lastResult.ExitCode
				}
			}
			if err := rememberExecutionResult(ctx, result); err != nil {
				return "", err
			}
			if t.logger != nil {
				t.logger.Info("step duplicate execution skipped",
					zap.Int("step_id", in.StepID),
					zap.String("command", rendered),
					zap.Int("attempt_count", attemptCount))
			}
			return marshalExecutionResult(result)
		}

		if hit && !succeeded && attemptCount >= maxSameStepCommandAttempts {
			reason := fmt.Sprintf("同一步骤同一命令已连续失败 %d 次，停止重复执行并建议人工处理或重规划", attemptCount)
			markExecutionStopped(ctx, reason, "manual_required")
			result := &ExecutionResult{
				StepID:     in.StepID,
				Success:    false,
				Skipped:    true,
				Mode:       modeLabel(isBashMode),
				Command:    rendered,
				Timeout:    in.Timeout,
				Output:     "",
				Error:      reason,
				ExitCode:   -3,
				Duration:   0,
				ExecutedAt: time.Now().Format("2006-01-02 15:04:05"),
			}
			if lastResult != nil {
				result.Output = lastResult.Output
			}
			if err := rememberExecutionResult(ctx, result); err != nil {
				return "", err
			}
			if t.logger != nil {
				t.logger.Info("step execution blocked due to repeated failures",
					zap.Int("step_id", in.StepID),
					zap.String("command", rendered),
					zap.Int("attempt_count", attemptCount),
					zap.String("reason", reason))
			}
			return marshalExecutionResult(result)
		}
	}

	if !t.whitelist.AllowedCommands[in.Command] {
		return fail(fmt.Errorf("command not in whitelist: %s", in.Command))
	}

	if isBashMode {
		if err := t.validateScript(in.Script); err != nil {
			return fail(fmt.Errorf("unsafe script: %w", err))
		}
	} else {
		if err := t.validateArgs(in.Args); err != nil {
			return fail(fmt.Errorf("unsafe arguments: %w", err))
		}
	}

	approvalInfo := t.buildApprovalInterruptInfo(in.StepID, in.Command, in.Args, in.Script, in.Timeout)
	requiresApproval := approvalInfo != nil && !in.DryRun
	approvalComment := ""
	if requiresApproval {
		wasInterrupted, _, _ := tool.GetInterruptState[any](ctx)
		if !wasInterrupted {
			return "", tool.Interrupt(ctx, approvalInfo)
		}

		isResumeTarget, hasData, resumeData := tool.GetResumeContext[map[string]any](ctx)
		if !isResumeTarget || !hasData {
			return "", tool.Interrupt(ctx, approvalInfo)
		}

		approved, resolved, comment := parseExecutionApprovalDecision(resumeData)
		approvalComment = comment
		if resolved {
			result := &ExecutionResult{
				StepID:     in.StepID,
				Success:    true,
				Approved:   true,
				Resolved:   true,
				Executed:   false,
				Mode:       modeLabel(isBashMode),
				Command:    rendered,
				Timeout:    in.Timeout,
				Output:     "",
				Error:      "",
				ExitCode:   0,
				Duration:   0,
				ExecutedAt: time.Now().Format("2006-01-02 15:04:05"),
				Comment:    approvalComment,
			}
			if err := rememberExecutionResult(ctx, result); err != nil {
				return fail(err)
			}
			clearRepeatedExecutionToolFailureState(ctx)
			return marshalExecutionResult(result)
		}

		if !approved {
			result := &ExecutionResult{
				StepID:     in.StepID,
				Success:    false,
				Approved:   false,
				Resolved:   false,
				Executed:   false,
				Mode:       modeLabel(isBashMode),
				Command:    rendered,
				Timeout:    in.Timeout,
				Output:     "",
				Error:      "command execution rejected by user",
				ExitCode:   -1,
				Duration:   0,
				ExecutedAt: time.Now().Format("2006-01-02 15:04:05"),
				Comment:    approvalComment,
			}
			if err := rememberExecutionResult(ctx, result); err != nil {
				return fail(err)
			}
			clearRepeatedExecutionToolFailureState(ctx)
			return marshalExecutionResult(result)
		}
	}

	if in.DryRun {
		result := &ExecutionResult{
			StepID:     in.StepID,
			Success:    true,
			Mode:       modeLabel(isBashMode),
			Command:    rendered,
			Timeout:    in.Timeout,
			Output:     fmt.Sprintf("[DRY RUN] Would execute: %s", rendered),
			ExitCode:   0,
			Duration:   0,
			ExecutedAt: time.Now().Format("2006-01-02 15:04:05"),
		}
		if err := rememberExecutionResult(ctx, result); err != nil {
			return fail(err)
		}
		clearRepeatedExecutionToolFailureState(ctx)
		return marshalExecutionResult(result)
	}

	result := t.executeCommand(ctx, in.StepID, in.Command, in.Args, in.Script, in.Timeout)
	if requiresApproval {
		result.Approved = true
		result.Resolved = false
		result.Executed = true
		result.Comment = approvalComment
	}
	if !result.Success {
		decision := recordExecutionToolFailure(ctx, "execute_step", firstNonEmptyExecutionText(result.Error, result.Output, rendered))
		if decision.Reached {
			result.Error = firstNonEmptyExecutionText(result.Error, decision.StopReason)
			if !strings.Contains(result.Error, decision.StopReason) {
				result.Error = strings.TrimSpace(result.Error + "；" + decision.StopReason)
			}
		}
	} else {
		clearRepeatedExecutionToolFailureState(ctx)
	}
	if err := rememberExecutionResult(ctx, result); err != nil {
		return fail(err)
	}

	output, err := marshalExecutionResult(result)
	if err != nil {
		return fail(err)
	}

	if t.logger != nil {
		t.logger.Info("step executed",
			zap.Int("step_id", in.StepID),
			zap.String("command", in.Command),
			zap.Bool("success", result.Success),
			zap.Int("duration_ms", result.Duration),
			zap.Bool("requires_approval", requiresApproval))
	}

	return output, nil
}

// parseExecuteStepInput 解析 execute_step 参数，兼容空参、仅 step_id、以及基于缓存计划自动补全。
// 输入：ctx、argumentsInJSON。
// 输出：可执行的完整参数。
func parseExecuteStepInput(ctx context.Context, argumentsInJSON string) (executeStepArgs, error) {
	payload := strings.TrimSpace(argumentsInJSON)
	if payload == "" || payload == "{}" || strings.EqualFold(payload, "null") {
		if nextStep, ok := getNextPendingExecutionStep(ctx); ok && nextStep != nil {
			return executeStepArgsFromPlanStep(*nextStep), nil
		}
		return executeStepArgs{}, fmt.Errorf("invalid arguments: missing execute_step payload, and no pending plan step available")
	}

	var in executeStepArgs
	if err := unmarshalArgsLenient(payload, &in); err != nil {
		if isTruncatedJSONError(err) {
			if nextStep, ok := getNextPendingExecutionStep(ctx); ok && nextStep != nil {
				return executeStepArgsFromPlanStep(*nextStep), nil
			}
		}
		return executeStepArgs{}, fmt.Errorf("invalid arguments: %w", err)
	}

	if in.StepID > 0 {
		if step, ok := getExecutionPlanStepByID(ctx, in.StepID); ok && step != nil {
			fillExecuteStepArgsFromPlan(&in, *step)
		}
	}

	if strings.TrimSpace(in.Command) == "" {
		if nextStep, ok := getNextPendingExecutionStep(ctx); ok && nextStep != nil {
			return executeStepArgsFromPlanStep(*nextStep), nil
		}
		return executeStepArgs{}, fmt.Errorf("invalid arguments: command is required")
	}

	return in, nil
}

// executeStepArgsFromPlanStep 将计划步骤转换为 execute_step 参数。
// 输入：ExecutionStep。
// 输出：executeStepArgs。
func executeStepArgsFromPlanStep(step ExecutionStep) executeStepArgs {
	args := executeStepArgs{
		StepID:  step.StepID,
		Command: strings.TrimSpace(step.Command),
		Args:    append([]string(nil), step.Args...),
		Timeout: step.Timeout,
	}

	if strings.EqualFold(args.Command, "bash") {
		if len(args.Args) > 0 {
			args.Script = strings.TrimSpace(strings.Join(args.Args, " "))
			args.Args = nil
		}
	}
	return args
}

// fillExecuteStepArgsFromPlan 使用缓存计划补全缺失字段。
// 输入：in、step。
// 输出：直接修改 in。
func fillExecuteStepArgsFromPlan(in *executeStepArgs, step ExecutionStep) {
	if in == nil {
		return
	}
	if in.StepID <= 0 {
		in.StepID = step.StepID
	}
	if strings.TrimSpace(in.Command) == "" {
		in.Command = strings.TrimSpace(step.Command)
	}
	if len(in.Args) == 0 && len(step.Args) > 0 {
		in.Args = append([]string(nil), step.Args...)
	}
	if in.Timeout <= 0 {
		in.Timeout = step.Timeout
	}
	if strings.EqualFold(strings.TrimSpace(in.Command), "bash") && strings.TrimSpace(in.Script) == "" && len(step.Args) > 0 {
		in.Script = strings.TrimSpace(strings.Join(step.Args, " "))
		in.Args = nil
	}
}

// validateArgs 验证参数安全性。
// 输入：args。
// 输出：校验错误。
func (t *ExecuteStepTool) validateArgs(args []string) error {
	dangerousPatterns := []string{";", "&&", "||", "|", "`", "$(", ">", "<", "rm -rf"}
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
// 输入：script。
// 输出：校验错误。
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

// buildApprovalInterruptInfo 判断当前步骤是否需要人工审批，并构造审批中断信息。
// 输入：stepID、command、args、script、timeoutSec。
// 输出：需要审批时返回中断信息，否则返回 nil。
func (t *ExecuteStepTool) buildApprovalInterruptInfo(stepID int, command string, args []string, script string, timeoutSec int) *ExecutionApprovalInterruptInfo {
	reason := approvalReasonForCommand(command, args, script)
	if reason == "" {
		return nil
	}

	info := &ExecutionApprovalInterruptInfo{
		StepID:     stepID,
		Command:    strings.TrimSpace(command),
		Args:       append([]string(nil), args...),
		Timeout:    timeoutSec,
		Reason:     reason,
		RawCommand: renderedCommand(command, args, script),
	}
	if strings.EqualFold(strings.TrimSpace(command), "bash") {
		info.Command = "bash"
		info.Args = nil
	}
	return info
}

// approvalReasonForCommand 判断命令是否会修改资源。
// 输入：command、args、script。
// 输出：若需审批返回原因文本，否则返回空字符串。
func approvalReasonForCommand(command string, args []string, script string) string {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "kubectl":
		if isReadOnlyKubectl(args) {
			return ""
		}
		return "该 kubectl 命令会修改 Kubernetes 资源或运行状态，需要人工确认后才能执行。"
	case "docker":
		if isReadOnlyDocker(args) {
			return ""
		}
		return "该 docker 命令会修改容器或镜像状态，需要人工确认后才能执行。"
	case "systemctl":
		if isReadOnlySystemctl(args) {
			return ""
		}
		return "该 systemctl 命令会修改服务运行状态，需要人工确认后才能执行。"
	case "bash":
		if isReadOnlyShellScript(script) {
			return ""
		}
		return "该 Bash 步骤可能修改集群或系统资源，需要人工确认后才能执行。"
	default:
		return ""
	}
}

// isReadOnlyKubectl 判断 kubectl 参数是否为只读操作。
// 输入：kubectl 参数数组。
// 输出：是否只读。
func isReadOnlyKubectl(args []string) bool {
	if len(args) == 0 {
		return true
	}
	readOnlyVerbs := map[string]struct{}{
		"get":           {},
		"describe":      {},
		"logs":          {},
		"top":           {},
		"version":       {},
		"api-resources": {},
		"api-versions":  {},
		"cluster-info":  {},
		"explain":       {},
		"wait":          {},
	}
	for _, arg := range args {
		token := strings.ToLower(strings.TrimSpace(arg))
		if token == "" || strings.HasPrefix(token, "-") {
			continue
		}
		switch token {
		case "auth":
			return containsAnyToken(args, "can-i")
		case "config":
			return containsAnyToken(args, "view", "current-context", "get-contexts")
		case "rollout":
			return containsAnyToken(args, "status", "history")
		}
		_, ok := readOnlyVerbs[token]
		return ok
	}
	return true
}

// isReadOnlyDocker 判断 docker 参数是否为只读操作。
// 输入：docker 参数数组。
// 输出：是否只读。
func isReadOnlyDocker(args []string) bool {
	if len(args) == 0 {
		return true
	}
	readOnly := map[string]struct{}{
		"ps":      {},
		"images":  {},
		"logs":    {},
		"inspect": {},
		"stats":   {},
		"version": {},
		"info":    {},
		"events":  {},
		"top":     {},
		"diff":    {},
	}
	for _, arg := range args {
		token := strings.ToLower(strings.TrimSpace(arg))
		if token == "" || strings.HasPrefix(token, "-") {
			continue
		}
		_, ok := readOnly[token]
		return ok
	}
	return true
}

// isReadOnlySystemctl 判断 systemctl 参数是否为只读操作。
// 输入：systemctl 参数数组。
// 输出：是否只读。
func isReadOnlySystemctl(args []string) bool {
	if len(args) == 0 {
		return true
	}
	readOnly := map[string]struct{}{
		"status":          {},
		"is-active":       {},
		"is-enabled":      {},
		"list-units":      {},
		"list-unit-files": {},
		"show":            {},
		"cat":             {},
	}
	for _, arg := range args {
		token := strings.ToLower(strings.TrimSpace(arg))
		if token == "" || strings.HasPrefix(token, "-") {
			continue
		}
		_, ok := readOnly[token]
		return ok
	}
	return true
}

// isReadOnlyShellScript 判断脚本是否由只读命令构成。
// 输入：shell script。
// 输出：是否只读。
func isReadOnlyShellScript(script string) bool {
	script = strings.TrimSpace(script)
	if script == "" {
		return true
	}

	segments := regexp.MustCompile(`\|\||&&|;|\||\n`).Split(script, -1)
	for _, segment := range segments {
		fields := strings.Fields(strings.TrimSpace(segment))
		if len(fields) == 0 {
			continue
		}

		command := strings.ToLower(strings.TrimSpace(fields[0]))
		args := fields[1:]
		switch command {
		case "kubectl":
			if !isReadOnlyKubectl(args) {
				return false
			}
		case "docker":
			if !isReadOnlyDocker(args) {
				return false
			}
		case "systemctl":
			if !isReadOnlySystemctl(args) {
				return false
			}
		case "cat", "ls", "tail", "head", "grep", "awk", "sed", "ps", "top", "free", "df", "du", "uptime", "date", "echo", "curl", "wget", "ping", "netstat", "ss", "journalctl":
			continue
		default:
			return false
		}
	}
	return true
}

// containsAnyToken 判断参数中是否包含目标 token。
// 输入：args、targets。
// 输出：是否存在命中。
func containsAnyToken(args []string, targets ...string) bool {
	targetSet := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		target = strings.ToLower(strings.TrimSpace(target))
		if target == "" {
			continue
		}
		targetSet[target] = struct{}{}
	}
	for _, arg := range args {
		token := strings.ToLower(strings.TrimSpace(arg))
		if token == "" {
			continue
		}
		if _, ok := targetSet[token]; ok {
			return true
		}
	}
	return false
}

// parseExecutionApprovalDecision 解析恢复执行时的审批决定。
// 输入：resumeData。
// 输出：approved、resolved、comment。
func parseExecutionApprovalDecision(data map[string]any) (approved bool, resolved bool, comment string) {
	if data == nil {
		return false, false, ""
	}
	if b, ok := boolFromExecutionAny(data["approved"]); ok {
		approved = b
	}
	if b, ok := boolFromExecutionAny(data["resolved"]); ok {
		resolved = b
	}
	if msg, ok := data["comment"].(string); ok {
		comment = strings.TrimSpace(msg)
	}
	return approved, resolved, comment
}

// boolFromExecutionAny 将任意值解析为布尔语义。
// 输入：任意值。
// 输出：解析结果、是否成功解析。
func boolFromExecutionAny(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "y", "ok", "approved", "confirm", "confirmed", "resolved", "done":
			return true, true
		case "false", "0", "no", "n", "reject", "rejected", "unresolved", "pending":
			return false, true
		}
	}
	return false, false
}

// marshalExecutionResult 序列化执行结果。
// 输入：执行结果。
// 输出：JSON 字符串。
func marshalExecutionResult(result *ExecutionResult) (string, error) {
	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}
	return string(output), nil
}

// executeCommand 执行命令。
// 输入：ctx、stepID、command、args、script、timeoutSec。
// 输出：结构化执行结果。
func (t *ExecuteStepTool) executeCommand(ctx context.Context, stepID int, command string, args []string, script string, timeoutSec int) *ExecutionResult {
	start := time.Now()
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	isBashMode := strings.EqualFold(command, "bash")
	rendered := renderedCommand(command, args, script)

	var cmd *exec.Cmd
	if isBashMode {
		cmd = exec.CommandContext(execCtx, "bash", "-lc", script)
	} else {
		cmd = exec.CommandContext(execCtx, command, args...)
	}

	output, err := cmd.CombinedOutput()
	duration := time.Since(start).Milliseconds()

	result := &ExecutionResult{
		StepID:     stepID,
		Mode:       modeLabel(isBashMode),
		Command:    rendered,
		Timeout:    timeoutSec,
		Output:     sanitizeExecutionOutput(output, maxExecutionOutputRunes),
		Duration:   int(duration),
		ExecutedAt: time.Now().Format("2006-01-02 15:04:05"),
	}

	if err != nil {
		result.Success = false
		result.Error = err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
		return result
	}

	result.Success = true
	result.ExitCode = 0
	return result
}

// modeLabel 返回执行模式名称。
// 输入：是否 bash 模式。
// 输出：模式名。
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

// sanitizeExecutionOutput 清洗并裁剪命令输出，避免大段日志或控制字符直接回灌模型上下文。
// 输入：原始输出字节、最大保留 rune 数。
// 输出：可安全写入 ExecutionResult 的文本。
func sanitizeExecutionOutput(raw []byte, maxRunes int) string {
	text := strings.ToValidUTF8(string(raw), "�")
	text = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return r
		case r < 32:
			return -1
		default:
			return r
		}
	}, text)
	text = strings.TrimSpace(text)
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}

	headLen := maxRunes / 2
	tailLen := maxRunes - headLen
	if headLen <= 0 || tailLen <= 0 {
		return string(runes[:maxRunes]) + "\n... (truncated)"
	}

	head := string(runes[:headLen])
	tail := string(runes[len(runes)-tailLen:])
	return head + "\n... (truncated) ...\n" + tail
}
