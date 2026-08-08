package slash

import (
	"strings"
	"testing"
)

func TestHooksBuiltinStatus(t *testing.T) {
	t.Parallel()
	reg := CreateDefaultRegistry(t.TempDir())
	cmd, ok := reg.Find("hooks")
	if !ok {
		t.Fatal("expected /hooks builtin command")
	}
	result, err := cmd.Handler(&Context{Status: func() StatusSnapshot {
		return StatusSnapshot{
			HooksEnabled:      true,
			HookRules:         2,
			HookNotifications: 1,
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Enabled: yes", "Rules: 2", "Pending notifications: 1", "cannot bypass permission checks"} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("expected %q in hooks output, got %q", want, result.Content)
		}
	}
}
