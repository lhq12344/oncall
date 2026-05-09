package dialogue

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

func TestBuildDialogueAgentToolsIncludesAnalysisTools(t *testing.T) {
	names := toolNames(t, buildDialogueAgentTools(&Config{}, nil))

	for _, want := range []string{
		"intent_analysis",
		"player_emotion_analysis",
		"request_detail_selection",
		"knowledge_retrieve",
		"bash_execute_with_approval",
	} {
		if !containsToolName(names, want) {
			t.Fatalf("dialogue tool set missing %q: %v", want, names)
		}
	}
}

func TestBuildComplexAgentToolsExcludesAnalysisTools(t *testing.T) {
	names := toolNames(t, buildComplexAgentTools(&Config{}, nil))

	for _, forbidden := range []string{
		"intent_analysis",
		"player_emotion_analysis",
	} {
		if containsToolName(names, forbidden) {
			t.Fatalf("complex tool set must not expose %q: %v", forbidden, names)
		}
	}

	for _, want := range []string{
		"request_detail_selection",
		"knowledge_retrieve",
		"bash_execute_with_approval",
	} {
		if !containsToolName(names, want) {
			t.Fatalf("complex tool set missing %q: %v", want, names)
		}
	}
}

func toolNames(t *testing.T, tools []tool.BaseTool) []string {
	t.Helper()
	names := make([]string, 0, len(tools))
	for _, item := range tools {
		info, err := item.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		if info != nil {
			names = append(names, info.Name)
		}
	}
	return names
}

func containsToolName(names []string, target string) bool {
	for _, name := range names {
		if name == target {
			return true
		}
	}
	return false
}
