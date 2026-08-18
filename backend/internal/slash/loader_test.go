package slash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectCommands(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, ".oncall", "commands", "git", "log.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	content := "---\ndescription: Show git log\nargument-hint: <range>\naliases: [gl, history]\n---\n\nPlease inspect git log for $ARGUMENTS"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cmds, warnings := LoadProjectCommands(root)
	if len(warnings) != 0 {
		t.Fatalf("warnings=%v, want none", warnings)
	}
	if len(cmds) != 1 {
		t.Fatalf("len(cmds)=%d, want 1", len(cmds))
	}
	cmd := cmds[0]
	if cmd.Name != "git:log" {
		t.Fatalf("name=%q, want git:log", cmd.Name)
	}
	if cmd.Description != "Show git log" || cmd.ArgumentHint != "<range>" {
		t.Fatalf("metadata not parsed: %#v", cmd)
	}
	if len(cmd.Aliases) != 2 || cmd.Aliases[0] != "gl" || cmd.Aliases[1] != "history" {
		t.Fatalf("aliases=%#v, want gl/history", cmd.Aliases)
	}
	result, err := cmd.Handler(&Context{Args: "HEAD~3..HEAD"})
	if err != nil {
		t.Fatalf("handler failed: %v", err)
	}
	if !strings.Contains(result.Prompt, "HEAD~3..HEAD") || strings.Contains(result.Prompt, "$ARGUMENTS") {
		t.Fatalf("prompt not expanded: %s", result.Prompt)
	}
}

func TestLoadProjectCommandsAppendsArguments(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, ".oncall", "commands", "deploy.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte("Check deploy readiness."), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	cmds, warnings := LoadProjectCommands(root)
	if len(warnings) != 0 || len(cmds) != 1 {
		t.Fatalf("cmds=%d warnings=%v", len(cmds), warnings)
	}
	result, err := cmds[0].Handler(&Context{Args: "prod"})
	if err != nil {
		t.Fatalf("handler failed: %v", err)
	}
	if !strings.Contains(result.Prompt, "## User Request\nprod") {
		t.Fatalf("prompt did not append args: %s", result.Prompt)
	}
}
