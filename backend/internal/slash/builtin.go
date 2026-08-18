package slash

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func CreateDefaultRegistry(workDir string) *Registry {
	reg := NewRegistry()
	for _, cmd := range builtinCommands() {
		reg.RegisterWithWarning(cmd)
	}
	commands, warnings := LoadProjectCommands(workDir)
	for _, warning := range warnings {
		reg.AddWarning(warning)
	}
	for _, cmd := range commands {
		if reg.HasConflict(cmd.Name) {
			reg.AddWarning(fmt.Sprintf("command %q from %s conflicts with builtin and was skipped", cmd.Name, cmd.SourcePath))
			continue
		}
		reg.RegisterWithWarning(cmd)
	}
	return reg
}

func builtinCommands() []Command {
	return []Command{
		{Name: "help", Aliases: []string{"h", "?"}, Type: TypeLocal, Source: SourceBuiltin, Builtin: true, Description: "列出所有斜杠命令，或查看单个命令详情。", ArgumentHint: "[command]", Handler: helpHandler},
		{Name: "commands", Type: TypeLocal, Source: SourceBuiltin, Builtin: true, Description: "显示斜杠命令加载来源与警告。", Handler: commandsHandler},
		{Name: "status", Aliases: []string{"s"}, Type: TypeLocal, Source: SourceBuiltin, Builtin: true, Description: "显示 OnCall 运行状态、Agent/Runners 与观测能力可用性。", Handler: statusHandler},
		{Name: "hooks", Aliases: []string{"hook"}, Type: TypeLocal, Source: SourceBuiltin, Builtin: true, Description: "Show hook engine status, rule count, and pending notifications.", ArgumentHint: "[status]", Handler: hooksHandler},
		{Name: "session", Type: TypeLocal, Source: SourceBuiltin, Builtin: true, Description: "显示当前会话与最近消息概览。", Handler: sessionHandler},
		{Name: "memory", Type: TypeLocal, Source: SourceBuiltin, Builtin: true, Description: "显示最近会话记忆摘要。", ArgumentHint: "[list]", Handler: memoryHandler},
		{Name: "review", Type: TypePrompt, Source: SourceBuiltin, Builtin: true, Description: "审查当前代码变更，关注逻辑、安全、性能和测试。", ArgumentHint: "[focus]", Handler: promptHandler("review", buildReviewPrompt)},
		{Name: "diagnose", Aliases: []string{"diag"}, Type: TypePrompt, Source: SourceBuiltin, Builtin: true, Description: "将症状转换为 OnCall 故障诊断任务。", ArgumentHint: "<symptom>", Handler: promptHandler("diagnose", buildDiagnosePrompt)},
		{Name: "ops", Aliases: []string{"incident", "aiops"}, Type: TypeOpsWorkflow, Source: SourceBuiltin, Builtin: true, Description: "触发完整 AI 运维处置工作流。", ArgumentHint: "<incident>", Handler: promptHandler("ops", buildOpsPrompt)},
		{Name: "k8s", Aliases: []string{"pods"}, Type: TypePrompt, Source: SourceBuiltin, Builtin: true, Description: "只读检查 Kubernetes 状态。", ArgumentHint: "[resource] [-n namespace]", Handler: promptHandler("k8s", buildK8sPrompt)},
		{Name: "metrics", Aliases: []string{"prom"}, Type: TypePrompt, Source: SourceBuiltin, Builtin: true, Description: "查询 Prometheus 指标并解释异常。", ArgumentHint: "<query>", Handler: promptHandler("metrics", buildMetricsPrompt)},
		{Name: "logs", Aliases: []string{"last-error", "errors"}, Type: TypePrompt, Source: SourceBuiltin, Builtin: true, Description: "查询最近错误日志并提取关键堆栈。", ArgumentHint: "[query|error] [time_range]", Handler: promptHandler("logs", buildLogsPrompt)},
		{Name: "cases", Type: TypePrompt, Source: SourceBuiltin, Builtin: true, Description: "检索历史故障处理案例。", ArgumentHint: "<query>", Handler: promptHandler("cases", buildCasesPrompt)},
		{Name: "clear", Type: TypeClientAction, Source: SourceBuiltin, Builtin: true, Description: "清空当前前端会话消息。", Handler: func(ctx *Context) (Result, error) {
			return Result{Type: TypeClientAction, Action: "clear_session", Payload: map[string]any{"scope": "current"}, Persist: false}, nil
		}},
	}
}

func helpHandler(ctx *Context) (Result, error) {
	var reg *Registry
	args := ""
	if ctx != nil {
		reg = ctx.Registry
		args = strings.TrimSpace(ctx.Args)
	}
	if reg == nil {
		return Result{Type: TypeLocal, Content: "Slash command registry is not available."}, nil
	}
	if args != "" {
		name := normalizeName(strings.Fields(args)[0])
		if cmd, ok := reg.Find(name); ok {
			aliases := ""
			if len(cmd.Aliases) > 0 {
				aliases = "\nAliases: /" + strings.Join(cmd.Aliases, ", /")
			}
			hint := ""
			if cmd.ArgumentHint != "" {
				hint = " " + cmd.ArgumentHint
			}
			return Result{Type: TypeLocal, Content: fmt.Sprintf("/%s%s\nType: %s\nSource: %s%s\n\n%s", cmd.Name, hint, cmd.Type, cmd.Source, aliases, cmd.Description)}, nil
		}
		return Result{Type: TypeLocal, Content: fmt.Sprintf("Unknown slash command /%s. Try /help.", name)}, nil
	}

	items := reg.List()
	var b strings.Builder
	b.WriteString("OnCall Slash Commands\n\n")
	for _, item := range items {
		hint := ""
		if item.ArgumentHint != "" {
			hint = " " + item.ArgumentHint
		}
		aliases := ""
		if len(item.Aliases) > 0 {
			aliases = fmt.Sprintf(" (aliases: /%s)", strings.Join(item.Aliases, ", /"))
		}
		b.WriteString(fmt.Sprintf("- /%s%s — %s%s\n", item.Name, hint, item.Description, aliases))
	}
	b.WriteString("\nUse /help <command> for details.")
	return Result{Type: TypeLocal, Content: b.String()}, nil
}

func commandsHandler(ctx *Context) (Result, error) {
	if ctx == nil || ctx.Registry == nil {
		return Result{Type: TypeLocal, Content: "Slash command registry is not available."}, nil
	}
	items := ctx.Registry.List()
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Source]++
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("Slash command sources\n\n")
	for _, key := range keys {
		b.WriteString(fmt.Sprintf("- %s: %d\n", key, counts[key]))
	}
	if warnings := ctx.Registry.Warnings(); len(warnings) > 0 {
		b.WriteString("\nWarnings\n")
		for _, warning := range warnings {
			b.WriteString("- " + warning + "\n")
		}
	}
	return Result{Type: TypeLocal, Content: strings.TrimSpace(b.String())}, nil
}

func statusHandler(ctx *Context) (Result, error) {
	status := ctx.currentStatus()
	workDir := filepath.Base(status.WorkDir)
	if workDir == "." || workDir == string(filepath.Separator) {
		workDir = status.WorkDir
	}
	var b strings.Builder
	b.WriteString("OnCall Status\n\n")
	b.WriteString(fmt.Sprintf("- Session: %s\n", emptyDefault(status.SessionID, "(empty)")))
	b.WriteString(fmt.Sprintf("- WorkDir: %s\n", emptyDefault(workDir, "(unknown)")))
	b.WriteString(fmt.Sprintf("- Dialogue runner: %s\n", yesNo(status.ChatRunnerReady)))
	b.WriteString(fmt.Sprintf("- Ops runner: %s\n", yesNo(status.OpsRunnerReady)))
	b.WriteString(fmt.Sprintf("- Dialogue agent: %s\n", yesNo(status.DialogueAgentReady)))
	b.WriteString(fmt.Sprintf("- Ops agent: %s\n", yesNo(status.OpsAgentReady)))
	b.WriteString(fmt.Sprintf("- Knowledge agent: %s\n", yesNo(status.KnowledgeAgentReady)))
	b.WriteString(fmt.Sprintf("- K8s configured: %s\n", yesNo(status.K8sAvailable)))
	b.WriteString(fmt.Sprintf("- Prometheus configured: %s\n", yesNo(status.PrometheusAvailable)))
	b.WriteString(fmt.Sprintf("- ES configured: %s\n", yesNo(status.ESAvailable)))
	b.WriteString(fmt.Sprintf("- Hooks enabled: %s\n", yesNo(status.HooksEnabled)))
	b.WriteString(fmt.Sprintf("- Hook rules: %d\n", status.HookRules))
	b.WriteString(fmt.Sprintf("- Hook notifications: %d\n", status.HookNotifications))
	b.WriteString(fmt.Sprintf("- Commands: %d total / %d project\n", status.LoadedCommands, status.UserCommands))
	return Result{Type: TypeLocal, Content: b.String()}, nil
}

func hooksHandler(ctx *Context) (Result, error) {
	status := ctx.currentStatus()
	var b strings.Builder
	b.WriteString("OnCall Hooks\n\n")
	b.WriteString(fmt.Sprintf("- Enabled: %s\n", yesNo(status.HooksEnabled)))
	b.WriteString(fmt.Sprintf("- Rules: %d\n", status.HookRules))
	b.WriteString(fmt.Sprintf("- Pending notifications: %d\n", status.HookNotifications))
	b.WriteString("- Safety: command hooks are disabled by default; hooks cannot bypass permission checks.")
	return Result{Type: TypeLocal, Content: b.String()}, nil
}

func sessionHandler(ctx *Context) (Result, error) {
	msgs := ctx.recentMessages(5)
	var b strings.Builder
	b.WriteString("Session\n\n")
	b.WriteString(fmt.Sprintf("- ID: %s\n", emptyDefault(cSessionID(ctx), "(empty)")))
	b.WriteString(fmt.Sprintf("- Recent messages: %d\n", len(msgs)))
	if len(msgs) > 0 {
		b.WriteString("\nRecent\n")
		for _, msg := range msgs {
			content := clip(strings.ReplaceAll(strings.TrimSpace(msg.Content), "\n", " "), 100)
			b.WriteString(fmt.Sprintf("- %s: %s\n", emptyDefault(msg.Role, "unknown"), content))
		}
	}
	return Result{Type: TypeLocal, Content: b.String()}, nil
}

func memoryHandler(ctx *Context) (Result, error) {
	msgs := ctx.recentMessages(8)
	if len(msgs) == 0 {
		return Result{Type: TypeLocal, Content: "No recent session memory is available."}, nil
	}
	var b strings.Builder
	b.WriteString("Recent session memory\n\n")
	for _, msg := range msgs {
		content := clip(strings.ReplaceAll(strings.TrimSpace(msg.Content), "\n", " "), 180)
		b.WriteString(fmt.Sprintf("- %s: %s\n", emptyDefault(msg.Role, "unknown"), content))
	}
	return Result{Type: TypeLocal, Content: b.String()}, nil
}

func promptHandler(kind string, builder func(args string, ctx *Context) string) Handler {
	return func(ctx *Context) (Result, error) {
		args := ""
		if ctx != nil {
			args = strings.TrimSpace(ctx.Args)
		}
		prompt := strings.TrimSpace(builder(args, ctx))
		if prompt == "" {
			return Result{}, fmt.Errorf("%s prompt is empty", kind)
		}
		resultType := TypePrompt
		if kind == "ops" {
			resultType = TypeOpsWorkflow
		}
		return Result{Type: resultType, Prompt: prompt, Persist: true, Metadata: map[string]any{"slash_kind": kind}}, nil
	}
}

func buildReviewPrompt(args string, ctx *Context) string {
	return "请分析当前 git diff，执行一次严格代码审查。重点检查：\n1. 逻辑错误和边界条件\n2. 安全问题和权限绕过\n3. 性能或资源泄露\n4. 测试覆盖与回归风险\n5. 是否符合 OnCall 的审批、SSE 与 Agent 语义。" + optionalFocus(args)
}

func buildDiagnosePrompt(args string, ctx *Context) string {
	return fmt.Sprintf("请作为 OnCall dialogue_agent 诊断以下故障症状：%s\n\n要求：\n- 先使用 intent_analysis 判断意图和缺失信息。\n- 缺少 namespace/service/time_range 时使用 request_detail_selection 追问。\n- 信息足够后优先使用 k8s_monitor、metrics_collector、ops_case_retrieve 做只读观测和历史案例检索。\n- 输出观测结果、可能根因、下一步建议；不要直接执行变更命令。", emptyDefault(args, "(用户未提供症状，请先询问关键细节)"))
}

func buildOpsPrompt(args string, ctx *Context) string {
	return fmt.Sprintf("请启动 OnCall 完整 AI 运维处置工作流处理以下事件：%s\n\n执行要求：\n- 先观测 Kubernetes Pod 状态、关键指标和错误日志。\n- 然后进行 RCA、修复方案生成、命令计划校验、必要时触发人工审批中断。\n- 命名空间检查必须优先 infra；如需要再补充 default/staging/production/kube-system。\n- 输出最终技术报告，说明是否解决、证据和后续建议。", emptyDefault(args, "系统健康检查"))
}

func buildK8sPrompt(args string, ctx *Context) string {
	return fmt.Sprintf("请只读检查 Kubernetes 状态：%s\n\n约束：\n- 必须使用 k8s_monitor 或等价只读观测能力。\n- 禁止生成或执行 kubectl delete/apply/patch/scale/rollout restart 等变更命令。\n- 如果发现需要变更，请建议用户改用 /ops <incident> 或走审批工具。\n- 输出 namespace、pod/deployment 状态、异常事件和下一步排查建议。", emptyDefault(args, "pods --all-namespaces"))
}

func buildMetricsPrompt(args string, ctx *Context) string {
	return fmt.Sprintf("请查询并解释 Prometheus 指标：%s\n\n要求：\n- 使用 metrics_collector。\n- 说明时间范围、指标趋势、阈值/异常点和可能影响。\n- 如指标不足，请明确建议补充 service、namespace、时间范围。", emptyDefault(args, "(用户未提供指标查询，请先询问 service/namespace/time_range)"))
}

func buildLogsPrompt(args string, ctx *Context) string {
	last := ""
	if ctx != nil {
		last = ctx.lastError()
	}
	return fmt.Sprintf("请查询最近错误日志：%s\n\n要求：\n- 优先使用 es_log_query 或已有日志查询能力，时间范围默认 1h。\n- 如果无法访问 ES，基于当前会话最近错误上下文降级分析。\n- 提取最近错误时间、服务/pod、关键堆栈、错误频率和建议下一步。\n\n当前会话最近错误（可能为空）：\n%s", emptyDefault(args, "error 1h"), emptyDefault(last, "(none)"))
}

func buildCasesPrompt(args string, ctx *Context) string {
	return fmt.Sprintf("请使用 ops_case_retrieve 检索历史故障处理案例：%s\n\n输出最相关案例、相似点、已验证修复动作和本次可借鉴/不可直接套用的注意事项。", emptyDefault(args, "(用户未提供查询，请先询问故障关键词)"))
}

func optionalFocus(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return ""
	}
	return "\n\nAdditional focus: " + args
}

func emptyDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func clip(value string, max int) string {
	r := []rune(value)
	if len(r) <= max {
		return value
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}
