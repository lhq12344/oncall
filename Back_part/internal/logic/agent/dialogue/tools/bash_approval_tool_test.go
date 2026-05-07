package tools

import (
	"context"
	"strings"
	"testing"
)

func TestBashApprovalToolRejectsScriptExecution(t *testing.T) {
	tool := NewBashApprovalTool(nil).(*BashApprovalTool)

	_, err := tool.InvokableRun(context.Background(), `{"command":"bash","script":"echo ok"}`)
	if err == nil {
		t.Fatalf("expected script execution to be rejected")
	}
	if !strings.Contains(err.Error(), "script execution is disabled") &&
		!strings.Contains(err.Error(), "command not in whitelist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBashApprovalToolRejectsOperationalCommands(t *testing.T) {
	tool := NewBashApprovalTool(nil).(*BashApprovalTool)

	for _, command := range []string{"bash", "kubectl", "docker", "systemctl", "curl", "wget"} {
		t.Run(command, func(t *testing.T) {
			_, err := tool.InvokableRun(context.Background(), `{"command":"`+command+`","args":["--version"]}`)
			if err == nil {
				t.Fatalf("expected %s to be rejected", command)
			}
			if !strings.Contains(err.Error(), "command not in whitelist") &&
				!strings.Contains(err.Error(), "script execution is disabled") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestBashApprovalToolRejectsShellFragmentsInArgs(t *testing.T) {
	tool := NewBashApprovalTool(nil).(*BashApprovalTool)

	_, err := tool.InvokableRun(context.Background(), `{"command":"ls","args":["/tmp","&&","rm","-rf","/"]}`)
	if err == nil {
		t.Fatalf("expected shell fragments to be rejected")
	}
	if !strings.Contains(err.Error(), "unsafe argument") {
		t.Fatalf("unexpected error: %v", err)
	}
}
