package toolregistry

import (
	"context"
	"sort"
	"testing"

	"go_agent/internal/ai/models"
)

func TestRegistryFiltersToolsByAgent(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry(Dependencies{ChatModel: &models.ChatModel{}})

	tests := []struct {
		name      string
		agent     AgentKind
		wantNames []string
		forbidden []string
	}{
		{
			name:  "dialogue agent gets user-facing retrieval and diagnostic tools only",
			agent: AgentDialogue,
			wantNames: []string{
				"intent_analysis",
				"request_detail_selection",
				"knowledge_retrieve",
				"ops_case_retrieve",
				"bash_execute_with_approval",
				"web_search",
				"k8s_monitor",
				"metrics_collector",
			},
			forbidden: []string{"normalize_plan", "generate_plan", "validate_plan", "execute_step", "rollback", "prune_knowledge"},
		},
		{
			name:  "plan agent cannot execute approved plans",
			agent: AgentPlan,
			wantNames: []string{
				"normalize_plan",
				"generate_plan",
				"validate_plan",
			},
			forbidden: []string{"execute_step", "validate_result", "rollback", "k8s_monitor"},
		},
		{
			name:  "execution agent consumes approved plans only",
			agent: AgentExecution,
			wantNames: []string{
				"execute_step",
				"validate_result",
				"rollback",
			},
			forbidden: []string{"normalize_plan", "generate_plan", "validate_plan", "k8s_monitor"},
		},
		{
			name:  "ops incident agent gets diagnostic tools without execution tools",
			agent: AgentOpsIncident,
			wantNames: []string{
				"k8s_monitor",
				"metrics_collector",
				"es_log_query",
				"time_query",
				"build_dependency_graph",
				"correlate_signals",
				"infer_root_cause",
				"analyze_impact",
			},
			forbidden: []string{"normalize_plan", "generate_plan", "execute_step", "rollback", "web_search"},
		},
		{
			name:  "strategy agent gets strategy tools including knowledge update",
			agent: AgentStrategy,
			wantNames: []string{
				"evaluate_strategy",
				"optimize_strategy",
				"update_knowledge",
				"prune_knowledge",
			},
			forbidden: []string{"normalize_plan", "generate_plan", "execute_step", "k8s_monitor", "web_search"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools, err := registry.ToolsForAgent(ctx, tt.agent)
			if err != nil {
				t.Fatalf("ToolsForAgent returned error: %v", err)
			}
			got := toolNames(ctx, t, tools)

			for _, want := range tt.wantNames {
				if !contains(got, want) {
					t.Fatalf("expected %s in tool set %v", want, got)
				}
			}
			for _, forbidden := range tt.forbidden {
				if contains(got, forbidden) {
					t.Fatalf("did not expect %s in tool set %v", forbidden, got)
				}
			}
			if hasDuplicate(got) {
				t.Fatalf("tool set contains duplicate names: %v", got)
			}
		})
	}
}

func TestRegistryRejectsUnknownAgent(t *testing.T) {
	_, err := NewRegistry(Dependencies{}).ToolsForAgent(context.Background(), AgentKind("unknown_agent"))
	if err == nil {
		t.Fatal("expected unknown agent to return an error")
	}
}

func TestRegistryBuildsExecutableGatewayToolsForAgent(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry(Dependencies{ChatModel: &models.ChatModel{}})

	tools, err := registry.ExecutableToolsForAgent(ctx, AgentPlan, ToolExposureDeferredGateway)
	if err != nil {
		t.Fatalf("ExecutableToolsForAgent returned error: %v", err)
	}
	got := toolNames(ctx, t, tools)
	if !contains(got, "ToolSearch") {
		t.Fatalf("expected deferred gateway tools to include tool_search, got %v", got)
	}
	if !contains(got, "InvokeDeferredTool") {
		t.Fatalf("expected deferred gateway tools to include invoke_deferred_tool, got %v", got)
	}
	if contains(got, "normalize_plan") {
		t.Fatalf("deferred gateway should not expose concrete deferred tool directly, got %v", got)
	}
}

func TestRegistryBuildsAlwaysToolsForAgent(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry(Dependencies{ChatModel: &models.ChatModel{}})

	tools, err := registry.ExecutableToolsForAgent(ctx, AgentRCA, ToolExposureAlways)
	if err != nil {
		t.Fatalf("ExecutableToolsForAgent returned error: %v", err)
	}
	got := toolNames(ctx, t, tools)
	if !contains(got, "ReadFile") {
		t.Fatalf("expected always tools to include read_file, got %v", got)
	}
	if !contains(got, "ToolSearch") {
		t.Fatalf("expected always tools to include tool_search, got %v", got)
	}
}

func toolNames(ctx context.Context, t *testing.T, tools []BaseTool) []string {
	t.Helper()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		info, err := tool.Info(ctx)
		if err != nil {
			t.Fatalf("Info returned error: %v", err)
		}
		names = append(names, info.Name)
	}
	sort.Strings(names)
	return names
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasDuplicate(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}
