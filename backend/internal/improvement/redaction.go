package improvement

import "regexp"

var sensitivePattern = regexp.MustCompile(`(?i)(password|secret|token|api[_-]?key)\s*[:=]\s*[^\s]+`)

func Redact(value string) string { return sensitivePattern.ReplaceAllString(value, `${1}=[redacted]`) }
