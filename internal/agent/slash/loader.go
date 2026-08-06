package slash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func LoadProjectCommands(workDir string) ([]Command, []string) {
	var commands []Command
	var warnings []string
	for _, source := range []struct {
		dir    string
		source CommandSource
	}{
		{dir: filepath.Join(workDir, ".oncall", "commands"), source: SourceProject},
		{dir: filepath.Join(workDir, ".mewcode", "commands"), source: SourceMewCompat},
	} {
		loaded, warns := loadCommandsFromDir(source.dir, source.source)
		commands = append(commands, loaded...)
		warnings = append(warnings, warns...)
	}
	return commands, warnings
}

func loadCommandsFromDir(root string, source CommandSource) ([]Command, []string) {
	if strings.TrimSpace(root) == "" {
		return nil, nil
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []string{fmt.Sprintf("cannot stat command dir %s: %v", root, err)}
	}

	var commands []Command
	var warnings []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("cannot read command path %s: %v", path, err))
			return nil
		}
		if d == nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		cmd, err := parseCommandFile(root, path, source)
		if err != nil {
			warnings = append(warnings, err.Error())
			return nil
		}
		commands = append(commands, cmd)
		return nil
	})
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("cannot walk command dir %s: %v", root, err))
	}
	return commands, warnings
}

func parseCommandFile(root, path string, source CommandSource) (Command, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Command{}, fmt.Errorf("cannot read command file %s: %w", path, err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return Command{}, fmt.Errorf("cannot resolve command file %s: %w", path, err)
	}
	name := strings.TrimSuffix(rel, filepath.Ext(rel))
	name = filepath.ToSlash(name)
	name = strings.ReplaceAll(name, "/", ":")
	name = normalizeName(name)

	meta, body := parseFrontMatter(string(raw))
	desc := strings.TrimSpace(meta["description"])
	hint := strings.TrimSpace(meta["argument-hint"])
	aliases := parseAliases(meta["aliases"])
	template := strings.TrimSpace(body)
	if template == "" {
		return Command{}, fmt.Errorf("command file %s has empty body", path)
	}
	if desc == "" {
		desc = "Project command " + name
	}

	return Command{
		Name:         name,
		Description:  desc,
		ArgumentHint: hint,
		Aliases:      aliases,
		Type:         TypePrompt,
		Source:       source,
		SourcePath:   path,
		Handler: func(ctx *Context) (Result, error) {
			args := ""
			if ctx != nil {
				args = strings.TrimSpace(ctx.Args)
			}
			prompt := expandArguments(template, args)
			return Result{Type: TypePrompt, Prompt: prompt, Persist: true}, nil
		},
	}, nil
}

func parseFrontMatter(raw string) (map[string]string, string) {
	meta := map[string]string{}
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return meta, raw
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, ":"); idx >= 0 {
			key := strings.ToLower(strings.TrimSpace(line[:idx]))
			value := strings.TrimSpace(line[idx+1:])
			meta[key] = strings.Trim(value, "\"'")
		}
	}
	if end < 0 {
		return meta, raw
	}
	return meta, strings.Join(lines[end+1:], "\n")
}

func parseAliases(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	raw = strings.Trim(raw, "[]")
	parts := strings.Split(raw, ",")
	if len(parts) == 1 {
		parts = strings.Fields(raw)
	}
	var aliases []string
	for _, part := range parts {
		part = strings.Trim(strings.TrimSpace(part), "\"'")
		if part != "" {
			aliases = append(aliases, normalizeAlias(part))
		}
	}
	return uniqueStrings(aliases)
}

func expandArguments(template, args string) string {
	if strings.Contains(template, "$ARGUMENTS") {
		return strings.ReplaceAll(template, "$ARGUMENTS", args)
	}
	if strings.TrimSpace(args) == "" {
		return template
	}
	return strings.TrimSpace(template) + "\n\n## User Request\n" + args
}
