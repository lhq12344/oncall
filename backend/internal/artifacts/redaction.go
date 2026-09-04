package artifacts

import "regexp"

var secretPattern = regexp.MustCompile(`(?i)(password|secret|token|api[_-]?key)\s*[:=]\s*[^\s]+`)

func Redact(data []byte) []byte {
	return secretPattern.ReplaceAll(data, []byte(`${1}=[redacted]`))
}
