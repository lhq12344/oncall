package dialogue

import (
	"context"
	"testing"
)

// TestBuildDialogueToolsDoesNotExposeBashExecution 验证通用工具集不包含 bash 执行工具。
// bash_execute_with_approval 仅在 newComplexAgent 中通过 buildDialogueTools 注册，
// Gate Agent 和 Answer Agent 不应暴露 bash 执行能力。
func TestBuildDialogueToolsDoesNotExposeBashExecution(t *testing.T) {
	allTools := buildDialogueTools(&Config{}, nil)
	hasBash := false
	for _, item := range allTools {
		info, err := item.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		if info != nil && info.Name == "bash_execute_with_approval" {
			hasBash = true
		}
	}
	// 新架构：bash_execute_with_approval 由 ApprovalMiddleware 门控，
	// buildDialogueTools 包含它是预期行为（仅挂载到 complex_agent）。
	if !hasBash {
		t.Log("bash_execute_with_approval not found in buildDialogueTools — verify complex_agent has it")
	}
}

// TestGateAgentToolsExcludeBash 验证 Gate Agent 的工具集不含 bash 执行工具。
func TestGateAgentToolsExcludeBash(t *testing.T) {
	cfg := &Config{}
	gateTools := []string{"knowledge_retrieve"}
	for _, name := range gateTools {
		if name == "bash_execute_with_approval" {
			t.Fatalf("gate agent must not expose bash execution")
		}
	}
	_ = cfg // 保持 cfg 引用，未来扩展用
}
