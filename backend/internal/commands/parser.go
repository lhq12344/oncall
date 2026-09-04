package commands

import "strings"

type Parsed struct {
	Name string
	Args string
	Raw  string
}

func Parse(input string) (Parsed, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return Parsed{}, false
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return Parsed{}, false
	}
	name := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	args := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
	return Parsed{Name: name, Args: args, Raw: trimmed}, true
}
