package hooks

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

func evaluateCondition(condition string, ctx HookContext) bool {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true
	}
	return evalOr(condition, ctx)
}

func evalOr(expr string, ctx HookContext) bool {
	parts := splitLogical(expr, "||")
	if len(parts) > 1 {
		for _, part := range parts {
			if evalAnd(part, ctx) {
				return true
			}
		}
		return false
	}
	return evalAnd(expr, ctx)
}

func evalAnd(expr string, ctx HookContext) bool {
	parts := splitLogical(expr, "&&")
	if len(parts) > 1 {
		for _, part := range parts {
			if !evalLeaf(part, ctx) {
				return false
			}
		}
		return true
	}
	return evalLeaf(expr, ctx)
}

func evalLeaf(expr string, ctx HookContext) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true
	}
	if strings.HasPrefix(expr, "!(") && strings.HasSuffix(expr, ")") {
		return !evaluateCondition(expr[2:len(expr)-1], ctx)
	}
	if strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") {
		return evaluateCondition(expr[1:len(expr)-1], ctx)
	}
	if strings.HasPrefix(expr, "!") {
		return !evalLeaf(strings.TrimSpace(expr[1:]), ctx)
	}

	for _, op := range []string{"!~", "=~", "=*", "!=", "=="} {
		if left, right, ok := splitComparison(expr, op); ok {
			actual := lookupField(left, ctx)
			expected := trimConditionValue(right)
			switch op {
			case "==":
				return actual == expected
			case "!=":
				return actual != expected
			case "=~":
				return regexMatch(expected, actual)
			case "!~":
				return !regexMatch(expected, actual)
			case "=*":
				return globMatch(expected, actual)
			}
		}
	}

	return lookupField(expr, ctx) != ""
}

func splitComparison(expr, op string) (string, string, bool) {
	idx := indexOutsideQuotes(expr, op)
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(expr[:idx]), strings.TrimSpace(expr[idx+len(op):]), true
}

func splitLogical(expr, op string) []string {
	var parts []string
	start := 0
	quote := rune(0)
	escaped := false
	depth := 0
	for i, r := range expr {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"', '/':
			quote = r
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && strings.HasPrefix(expr[i:], op) {
				parts = append(parts, strings.TrimSpace(expr[start:i]))
				start = i + len(op)
			}
		}
	}
	if len(parts) == 0 {
		return []string{expr}
	}
	parts = append(parts, strings.TrimSpace(expr[start:]))
	return parts
}

func indexOutsideQuotes(expr, needle string) int {
	quote := rune(0)
	escaped := false
	for i, r := range expr {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' || r == '/' {
			quote = r
			continue
		}
		if strings.HasPrefix(expr[i:], needle) {
			return i
		}
	}
	return -1
}

func trimConditionValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
		if value[0] == '/' && value[len(value)-1] == '/' {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func lookupField(field string, ctx HookContext) string {
	field = strings.TrimSpace(field)
	switch field {
	case "event", "event_name":
		return string(ctx.EventName)
	case "tool", "tool_name":
		return ctx.ToolName
	case "agent", "agent_name":
		return ctx.AgentName
	case "component":
		return ctx.Component
	case "session", "session_id":
		return ctx.SessionID
	case "checkpoint", "checkpoint_id":
		return ctx.CheckpointID
	case "error":
		return ctx.Error
	case "message":
		return ctx.Message
	case "result":
		return ctx.Result
	case "file_path":
		return stringify(firstNonNil(ctx.ToolArgs["file_path"], ctx.ToolArgs["path"], valueAt(ctx.Metadata, "file_path")))
	}
	if strings.HasPrefix(field, "args.") {
		return stringify(valueAt(ctx.ToolArgs, strings.TrimPrefix(field, "args.")))
	}
	if strings.HasPrefix(field, "tool_args.") {
		return stringify(valueAt(ctx.ToolArgs, strings.TrimPrefix(field, "tool_args.")))
	}
	if strings.HasPrefix(field, "metadata.") {
		return stringify(valueAt(ctx.Metadata, strings.TrimPrefix(field, "metadata.")))
	}
	return ""
}

func valueAt(values map[string]any, dotted string) any {
	if values == nil {
		return nil
	}
	var current any = values
	for _, part := range strings.Split(dotted, ".") {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[part]
	}
	return current
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func stringify(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func regexMatch(pattern, value string) bool {
	re, err := regexp.Compile(pattern)
	return err == nil && re.MatchString(value)
}

func globMatch(pattern, value string) bool {
	if pattern == "" {
		return value == ""
	}
	value = filepath.ToSlash(value)
	pattern = filepath.ToSlash(pattern)
	if ok, _ := filepath.Match(pattern, value); ok {
		return true
	}
	rePattern := regexp.QuoteMeta(pattern)
	rePattern = strings.ReplaceAll(rePattern, "\\*\\*", ".*")
	rePattern = strings.ReplaceAll(rePattern, "\\*", "[^/]*")
	rePattern = strings.ReplaceAll(rePattern, "\\?", ".")
	re, err := regexp.Compile("^" + rePattern + "$")
	return err == nil && re.MatchString(value)
}
