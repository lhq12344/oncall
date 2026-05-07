package dialogue

import (
	"context"
	"testing"
)

func TestBuildDialogueToolsDoesNotExposeBashExecution(t *testing.T) {
	tools := buildDialogueTools(&Config{}, nil)
	for _, item := range tools {
		info, err := item.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		if info != nil && info.Name == "bash_execute_with_approval" {
			t.Fatalf("dialogue tools must not expose bash execution")
		}
	}
}
