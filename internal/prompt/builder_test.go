package prompt

import (
	"strings"
	"testing"
)

func TestBuilderBuildSortsAndSkipsEmptySections(t *testing.T) {
	got := NewBuilder().
		Add(Section{Name: "late", Priority: 20, Content: "late"}).
		Add(Section{Name: "empty", Priority: 10, Content: "   "}).
		Add(Section{Name: "early", Priority: 0, Content: "early"}).
		Build()

	if got != "early\n\nlate" {
		t.Fatalf("unexpected build output: %q", got)
	}
}

func TestBuildAgentPromptIncludesRoleEnvironmentAndExtensions(t *testing.T) {
	env := EnvironmentContext{
		WorkDir:   "/repo/oncall",
		OS:        "testos",
		Arch:      "testarch",
		Shell:     "testsh",
		IsGitRepo: true,
		GitBranch: "rebuild",
		Model:     "test-model",
		Date:      "2026-08-04",
	}
	got := BuildAgentPrompt(RoleExecution, env, BuildOptions{
		CustomInstructions: "custom guidance",
		KnowledgeSection:   "knowledge guidance",
		MemorySection:      "memory guidance",
	})

	for _, want := range []string{
		"# 身份",
		"故障修复执行代理",
		"validate_plan",
		"execute_step",
		"manual_required",
		"- 工作目录: /repo/oncall",
		"- Git 分支: rebuild",
		"custom guidance",
		"knowledge guidance",
		"memory guidance",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}

func TestBuildAgentPromptUnknownRoleOmitsRoleSection(t *testing.T) {
	got := BuildAgentPrompt(Role("missing"), EnvironmentContext{}, BuildOptions{})
	if strings.Contains(got, "Agent 指令") {
		t.Fatalf("unknown role should not emit a role instruction: %q", got)
	}
}

func TestDetectEnvironmentUsesFallbacks(t *testing.T) {
	env := DetectEnvironment("")
	if env.WorkDir == "" {
		t.Fatal("expected workdir")
	}
	if env.OS == "" || env.Arch == "" {
		t.Fatalf("expected platform fields, got %#v", env)
	}
	if env.Shell == "" {
		t.Fatalf("expected shell fallback, got %#v", env)
	}
	if env.Date == "" {
		t.Fatalf("expected date, got %#v", env)
	}
}

func TestToolUseSectionMentionsDeferredGateway(t *testing.T) {
	got := ToolUseSection().Content
	for _, want := range []string{"ToolSearch", "InvokeDeferredTool", "allow", "ask", "deny", "工具失败"} {
		if !strings.Contains(got, want) {
			t.Fatalf("tool use section missing %q", want)
		}
	}
}

func TestRolePromptsDescribeRoleSpecificDeferredTools(t *testing.T) {
	execution := BuildAgentPrompt(RoleExecution, EnvironmentContext{}, BuildOptions{})
	for _, want := range []string{"normalize_plan", "validate_plan", "execute_step", "validate_result", "ToolSearch", "InvokeDeferredTool"} {
		if !strings.Contains(execution, want) {
			t.Fatalf("execution prompt missing %q", want)
		}
	}
	if strings.Contains(execution, "web_search") {
		t.Fatalf("execution prompt should not advertise dialogue deferred tools")
	}

	dialogue := BuildAgentPrompt(RoleDialogue, EnvironmentContext{}, BuildOptions{})
	for _, want := range []string{"intent_analysis", "request_detail_selection", "web_search", "bash_execute_with_approval"} {
		if !strings.Contains(dialogue, want) {
			t.Fatalf("dialogue prompt missing %q", want)
		}
	}
}

func TestBuildAgentPromptIncludesDeferredToolGuidance(t *testing.T) {
	got := BuildAgentPrompt(RoleOps, EnvironmentContext{}, BuildOptions{})
	for _, want := range []string{"ops_incident_agent deferred", "es_log_query", "infer_root_cause", "不要执行变更"} {
		if !strings.Contains(got, want) {
			t.Fatalf("ops prompt missing deferred guidance %q", want)
		}
	}
}
