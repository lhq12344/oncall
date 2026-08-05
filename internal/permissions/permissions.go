package permissions

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

type DecisionEffect string

const (
	Allow DecisionEffect = "allow"
	Deny  DecisionEffect = "deny"
	Ask   DecisionEffect = "ask"
)

type Decision struct {
	Effect DecisionEffect
	Reason string
}

type PermissionMode string

const (
	ModeDefault     PermissionMode = "default"
	ModeAcceptEdits PermissionMode = "acceptEdits"
	ModePlan        PermissionMode = "plan"
	ModeBypass      PermissionMode = "bypassPermissions"
)

type ToolCategory string

const (
	CategoryRead    ToolCategory = "read"
	CategoryWrite   ToolCategory = "write"
	CategoryCommand ToolCategory = "command"
)

var modeMatrix = map[PermissionMode]map[ToolCategory]DecisionEffect{
	ModeDefault:     {CategoryRead: Allow, CategoryWrite: Ask, CategoryCommand: Ask},
	ModeAcceptEdits: {CategoryRead: Allow, CategoryWrite: Allow, CategoryCommand: Ask},
	ModeBypass:      {CategoryRead: Allow, CategoryWrite: Allow, CategoryCommand: Allow},
}

func ModeDecide(mode PermissionMode, category ToolCategory) DecisionEffect {
	if mode == ModePlan {
		return Ask
	}
	m, ok := modeMatrix[mode]
	if !ok {
		return Ask
	}
	if effect, ok := m[category]; ok {
		return effect
	}
	return Ask
}

type dangerousPattern struct {
	re     *regexp.Regexp
	reason string
}

var defaultDangerousPatterns = []dangerousPattern{
	{regexp.MustCompile("(?i)\\brm\\s+-[a-z]*r[a-z]*f[a-z]*\\s+/(?:\\s|$)"), "recursive force delete root"},
	{regexp.MustCompile("(?i)\\bmkfs\\."), "format disk"},
	{regexp.MustCompile("(?i)\\bdd\\s+if=.*\\s+of=/dev/"), "direct write to disk device"},
	{regexp.MustCompile("(?i)\\bchmod\\s+-R\\s+777\\s+/"), "recursive chmod root"},
	{regexp.MustCompile(":\\(\\)\\s*\\{\\s*:\\|:&\\s*\\};:"), "fork bomb"},
	{regexp.MustCompile("(?i)\\b(curl|wget)\\b.*\\|\\s*(sh|bash)\\b"), "remote script piped to shell"},
	{regexp.MustCompile("(?i)>\\s*/dev/sd[a-z]?"), "overwrite disk device"},
	{regexp.MustCompile("(?i)\\bgit\\s+push\\b.*--force"), "force push"},
	{regexp.MustCompile("(?i)\\bgit\\s+reset\\s+--hard\\b"), "hard reset"},
	{regexp.MustCompile("(?i)\\bgit\\s+clean\\s+-[a-z]*f"), "force clean untracked files"},
	{regexp.MustCompile("(?i)\\bgit\\s+checkout\\s+\\."), "discard all local changes"},
	{regexp.MustCompile("(?i)\\bgit\\s+branch\\s+-D\\b"), "force delete branch"},
	{regexp.MustCompile("(?i)\\b(shutdown|reboot|halt|poweroff)\\b"), "system power operation"},
}

func DetectDangerous(command string) (bool, string) {
	for _, p := range defaultDangerousPatterns {
		if p.re.MatchString(command) {
			return true, p.reason
		}
	}
	return false, ""
}

type PathSandbox struct {
	allowedRoots []string
	denyWrite    []string
	projectRoot  string
}

func NewPathSandbox(projectRoot string, extraAllowed ...string) *PathSandbox {
	root, _ := filepath.Abs(projectRoot)
	allowed := []string{root, os.TempDir()}
	for _, p := range extraAllowed {
		if strings.TrimSpace(p) == "" {
			continue
		}
		abs, _ := filepath.Abs(p)
		allowed = append(allowed, abs)
	}

	denyWrite := []string{
		filepath.Join(root, "manifest", "config", "config.yaml"),
		filepath.Join(root, ".oncall", "permissions.local.yaml"),
		filepath.Join(root, ".codex"),
		filepath.Join(root, ".agents"),
	}

	return &PathSandbox{allowedRoots: cleanPaths(allowed), denyWrite: cleanPaths(denyWrite), projectRoot: root}
}

func (s *PathSandbox) Check(path string) (bool, string) {
	if strings.TrimSpace(path) == "" {
		return true, ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Sprintf("invalid path: %v", err)
	}
	abs = filepath.Clean(abs)

	if s.IsProtected(abs) {
		return false, "path is protected by OnCall permission policy"
	}

	for _, root := range s.allowedRoots {
		if containsPath(root, abs) {
			return true, ""
		}
	}
	return false, "path is outside allowed roots"
}

func (s *PathSandbox) IsProtected(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)

	if s.projectRoot != "" && containsPath(s.projectRoot, abs) {
		base := filepath.Base(abs)
		if base == ".env" || strings.HasPrefix(base, ".env.") {
			return true
		}
	}

	for _, protected := range s.denyWrite {
		if containsPath(protected, abs) {
			return true
		}
	}
	return false
}

func cleanPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		out = append(out, filepath.Clean(p))
	}
	return out
}

func containsPath(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if strings.EqualFold(root, path) {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

type RuleEffect string

const (
	RuleAllow RuleEffect = "allow"
	RuleDeny  RuleEffect = "deny"
	RuleAsk   RuleEffect = "ask"
)

type Rule struct {
	ToolName string
	Pattern  string
	Effect   RuleEffect
}

func (r Rule) Matches(toolName, content string) bool {
	return r.ToolName == toolName && globMatch(r.Pattern, content)
}

func globMatch(pattern, content string) bool {
	if !strings.ContainsAny(pattern, "*?") {
		return pattern == content
	}
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteByte('.')
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(content)
}

type RuleEngine struct {
	UserPath    string
	ProjectPath string
	LocalPath   string
}

func (e *RuleEngine) Evaluate(toolName, content string) *RuleEffect {
	for _, path := range []string{e.UserPath, e.ProjectPath, e.LocalPath} {
		rules := loadRulesFile(path)
		for i := len(rules) - 1; i >= 0; i-- {
			if rules[i].Matches(toolName, content) {
				eff := rules[i].Effect
				return &eff
			}
		}
	}
	return nil
}

func (e *RuleEngine) AppendLocalRule(r Rule) error {
	if strings.TrimSpace(e.LocalPath) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(e.LocalPath), 0o755); err != nil {
		return err
	}
	line := fmt.Sprintf("- rule: %s(%s)\n  effect: %s\n", r.ToolName, r.Pattern, r.Effect)
	f, err := os.OpenFile(e.LocalPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

func loadRulesFile(path string) []Rule {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseRulesYAMLSubset(string(data))
}

func parseRulesYAMLSubset(content string) []Rule {
	var rules []Rule
	var pendingRule string
	var pendingEffect RuleEffect

	flush := func() {
		if pendingRule == "" || pendingEffect == "" {
			return
		}
		if r, ok := ParseRule(pendingRule, pendingEffect); ok {
			rules = append(rules, r)
		}
		pendingRule = ""
		pendingEffect = ""
	}

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			flush()
			line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
		}
		if strings.HasPrefix(line, "rule:") {
			pendingRule = cleanYAMLScalar(strings.TrimSpace(strings.TrimPrefix(line, "rule:")))
			continue
		}
		if strings.HasPrefix(line, "effect:") {
			pendingEffect = RuleEffect(cleanYAMLScalar(strings.TrimSpace(strings.TrimPrefix(line, "effect:"))))
			flush()
			continue
		}
		if strings.Contains(line, "rule:") && strings.Contains(line, "effect:") {
			fields := strings.Split(line, ",")
			var rule string
			var effect RuleEffect
			for _, field := range fields {
				field = strings.Trim(field, " {}")
				if strings.HasPrefix(field, "rule:") {
					rule = cleanYAMLScalar(strings.TrimSpace(strings.TrimPrefix(field, "rule:")))
				}
				if strings.HasPrefix(field, "effect:") {
					effect = RuleEffect(cleanYAMLScalar(strings.TrimSpace(strings.TrimPrefix(field, "effect:"))))
				}
			}
			if r, ok := ParseRule(rule, effect); ok {
				rules = append(rules, r)
			}
		}
	}
	flush()
	return rules
}

func cleanYAMLScalar(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "'\"")
	return value
}

func ParseRule(expr string, effect RuleEffect) (Rule, bool) {
	expr = strings.TrimSpace(expr)
	if effect != RuleAllow && effect != RuleDeny && effect != RuleAsk {
		return Rule{}, false
	}
	open := strings.Index(expr, "(")
	close := strings.LastIndex(expr, ")")
	if open <= 0 || close <= open {
		return Rule{}, false
	}
	toolName := strings.TrimSpace(expr[:open])
	pattern := strings.TrimSpace(expr[open+1 : close])
	if toolName == "" || pattern == "" {
		return Rule{}, false
	}
	return Rule{ToolName: toolName, Pattern: pattern, Effect: effect}, true
}

var safeCommandPrefixes = []string{
	"pwd", "ls", "cat", "head", "tail", "grep", "awk", "sed", "ps", "top", "free", "df", "du", "uptime", "date", "echo", "whoami", "id", "hostname", "uname",
	"go version", "go env", "go list", "go test",
	"git status", "git diff", "git log", "git show", "git branch",
	"kubectl get", "kubectl describe", "kubectl logs", "kubectl top", "kubectl api-resources", "kubectl api-versions", "kubectl cluster-info", "kubectl version", "kubectl explain", "kubectl events", "kubectl auth can-i", "kubectl config view", "kubectl config current-context", "kubectl config get-contexts", "kubectl rollout status", "kubectl rollout history",
	"docker ps", "docker images", "docker logs", "docker inspect", "docker stats", "docker version", "docker info", "docker events", "docker top", "docker diff", "docker container ls", "docker container logs", "docker container inspect", "docker image ls", "docker image inspect", "docker network ls", "docker network inspect", "docker volume ls", "docker volume inspect", "docker system df",
	"systemctl status", "systemctl show", "systemctl is-active", "systemctl is-enabled", "systemctl is-failed", "systemctl list-units", "systemctl list-unit-files", "systemctl list-dependencies", "systemctl cat",
	"journalctl",
}

func IsSafeCommand(command string) bool {
	cmd := strings.TrimSpace(command)
	if cmd == "" || hasShellMetachar(cmd) {
		return false
	}
	normalized := strings.Join(strings.Fields(cmd), " ")
	for _, prefix := range safeCommandPrefixes {
		if normalized == prefix || strings.HasPrefix(normalized, prefix+" ") {
			return true
		}
	}
	return false
}

func hasShellMetachar(cmd string) bool {
	for _, fragment := range []string{">", "<", "|", ";", "&&", "||", "$(", "`", "\n", "\r"} {
		if strings.Contains(cmd, fragment) {
			return true
		}
	}
	return false
}

var contentFields = map[string]string{
	"Bash":      "command",
	"ReadFile":  "file_path",
	"WriteFile": "file_path",
	"EditFile":  "file_path",
	"Glob":      "pattern",
	"Grep":      "pattern",
}

func ExtractContent(toolName string, args map[string]any) string {
	switch toolName {
	case "bash_execute_with_approval":
		return strings.TrimSpace(joinCommandArgs(stringValue(args["command"]), stringSliceValue(args["args"])))
	case "execute_step":
		command := stringValue(args["command"])
		if strings.EqualFold(strings.TrimSpace(command), "bash") {
			if script := strings.TrimSpace(stringValue(args["script"])); script != "" {
				return script
			}
			return strings.TrimSpace(strings.Join(stringSliceValue(args["args"]), " "))
		}
		return strings.TrimSpace(joinCommandArgs(command, stringSliceValue(args["args"])))
	default:
		field, ok := contentFields[toolName]
		if !ok {
			return ""
		}
		return strings.TrimSpace(stringValue(args[field]))
	}
}

func DescribeToolAction(toolName string, args map[string]any) string {
	if content := ExtractContent(toolName, args); content != "" {
		return content
	}
	parts := make([]string, 0, len(args))
	for k, v := range args {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, ", ")
}

func joinCommandArgs(command string, args []string) string {
	command = strings.TrimSpace(command)
	joinedArgs := strings.TrimSpace(strings.Join(args, " "))
	return strings.TrimSpace(command + " " + joinedArgs)
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func stringSliceValue(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

type Options struct {
	ProjectRoot       string
	Mode              PermissionMode
	PlanFilePath      string
	ExtraAllowedRoots []string
	SandboxEnabled    bool
	UserRulesPath     string
	ProjectRulesPath  string
	LocalRulesPath    string
}

type Checker struct {
	Mode            PermissionMode
	PlanFilePath    string
	SandboxEnabled  bool
	Sandbox         *PathSandbox
	RuleEngine      *RuleEngine
	sessionAllowed  []Rule
	sessionAllowedM sync.RWMutex
}

func NewChecker(opts Options) *Checker {
	root := opts.ProjectRoot
	if strings.TrimSpace(root) == "" {
		if cwd, err := os.Getwd(); err == nil {
			root = cwd
		}
	}
	root, _ = filepath.Abs(root)
	mode := opts.Mode
	if mode == "" {
		mode = ModeDefault
	}

	userPath := opts.UserRulesPath
	if userPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			userPath = filepath.Join(home, ".oncall", "permissions.yaml")
		}
	}
	projectPath := opts.ProjectRulesPath
	if projectPath == "" {
		projectPath = filepath.Join(root, ".oncall", "permissions.yaml")
	}
	localPath := opts.LocalRulesPath
	if localPath == "" {
		localPath = filepath.Join(root, ".oncall", "permissions.local.yaml")
	}

	return &Checker{
		Mode:           mode,
		PlanFilePath:   opts.PlanFilePath,
		SandboxEnabled: opts.SandboxEnabled,
		Sandbox:        NewPathSandbox(root, opts.ExtraAllowedRoots...),
		RuleEngine: &RuleEngine{
			UserPath:    userPath,
			ProjectPath: projectPath,
			LocalPath:   localPath,
		},
	}
}

func (c *Checker) Check(toolName string, args map[string]any) Decision {
	if c == nil {
		c = NewChecker(Options{})
	}
	content := ExtractContent(toolName, args)
	category := CategoryForTool(toolName)

	if c.Mode == ModePlan && category == CategoryWrite && isPlanFile(content, c.PlanFilePath) {
		return Decision{Effect: Allow, Reason: "plan file write allowed"}
	}

	if category == CategoryCommand && IsSafeCommand(content) {
		return Decision{Effect: Allow, Reason: "safe read-only command"}
	}

	if dangerous, reason := DetectDangerous(content); dangerous {
		return Decision{Effect: Deny, Reason: reason}
	}

	if category == CategoryRead || category == CategoryWrite {
		if ok, reason := c.Sandbox.Check(content); !ok {
			if c.Mode == ModeBypass {
				return Decision{Effect: Allow, Reason: "bypass permissions"}
			}
			if c.Sandbox.IsProtected(content) {
				return Decision{Effect: Deny, Reason: reason}
			}
			return Decision{Effect: Ask, Reason: reason}
		}
	}

	if c.SandboxEnabled && category == CategoryCommand {
		for _, sub := range splitCompoundCommand(content) {
			if eff := c.RuleEngine.Evaluate(toolName, sub); eff != nil {
				switch *eff {
				case RuleDeny:
					return Decision{Effect: Deny, Reason: "denied by permission rule"}
				case RuleAsk:
					return Decision{Effect: Ask, Reason: "approval required by permission rule"}
				}
			}
		}
		return Decision{Effect: Allow, Reason: "command allowed by OS sandbox mode"}
	}

	if eff := c.RuleEngine.Evaluate(toolName, content); eff != nil {
		return decisionFromRuleEffect(*eff, "matched permission rule")
	}

	if c.checkSessionAllowed(toolName, content) {
		return Decision{Effect: Allow, Reason: "allowed for this session"}
	}

	effect := ModeDecide(c.Mode, category)
	return Decision{Effect: effect, Reason: fmt.Sprintf("%s permission mode", c.Mode)}
}

func CategoryForTool(toolName string) ToolCategory {
	switch toolName {
	case "ReadFile", "Glob", "Grep":
		return CategoryRead
	case "WriteFile", "EditFile":
		return CategoryWrite
	case "Bash", "bash_execute_with_approval", "execute_step":
		return CategoryCommand
	default:
		return CategoryCommand
	}
}

func decisionFromRuleEffect(effect RuleEffect, reason string) Decision {
	switch effect {
	case RuleAllow:
		return Decision{Effect: Allow, Reason: reason}
	case RuleDeny:
		return Decision{Effect: Deny, Reason: reason}
	case RuleAsk:
		return Decision{Effect: Ask, Reason: reason}
	default:
		return Decision{Effect: Ask, Reason: reason}
	}
}

func (c *Checker) checkSessionAllowed(toolName, content string) bool {
	c.sessionAllowedM.RLock()
	defer c.sessionAllowedM.RUnlock()
	for _, rule := range c.sessionAllowed {
		if rule.Matches(toolName, content) {
			return true
		}
	}
	return false
}

func (c *Checker) AllowAlways(toolName string, args map[string]any) error {
	if c == nil {
		return nil
	}
	content := ExtractContent(toolName, args)
	if strings.TrimSpace(content) == "" {
		return nil
	}
	rule := Rule{ToolName: toolName, Pattern: content, Effect: RuleAllow}

	c.sessionAllowedM.Lock()
	c.sessionAllowed = append(c.sessionAllowed, rule)
	c.sessionAllowedM.Unlock()

	if c.RuleEngine == nil {
		return nil
	}
	return c.RuleEngine.AppendLocalRule(rule)
}

func isPlanFile(path, planFilePath string) bool {
	if strings.TrimSpace(planFilePath) == "" || strings.TrimSpace(path) == "" {
		return false
	}
	absPath, err1 := filepath.Abs(path)
	absPlan, err2 := filepath.Abs(planFilePath)
	return err1 == nil && err2 == nil && filepath.Clean(absPath) == filepath.Clean(absPlan)
}

func splitCompoundCommand(cmd string) []string {
	parts := regexp.MustCompile("\\s*(?:&&|\\|\\||[;|])\\s*").Split(cmd, -1)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return []string{cmd}
	}
	return result
}
