package slash

import "strings"

type ParsedCommand struct {
	Name string
	Args string
	Raw  string
}

func Parse(text string) (ParsedCommand, bool) {
	raw := strings.TrimSpace(text)
	if !strings.HasPrefix(raw, "/") {
		return ParsedCommand{}, false
	}
	body := strings.TrimSpace(strings.TrimPrefix(raw, "/"))
	if body == "" {
		return ParsedCommand{Raw: raw}, true
	}
	fields := strings.Fields(body)
	name := normalizeName(fields[0])
	args := ""
	if len(fields) > 1 {
		idx := strings.Index(body, fields[1])
		if idx >= 0 {
			args = strings.TrimSpace(body[idx:])
		}
	}
	return ParsedCommand{Name: name, Args: args, Raw: raw}, true
}
