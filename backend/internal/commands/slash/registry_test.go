package slash

import (
	"strings"
	"testing"
)

func TestRegistryFindAliasAndComplete(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	cmd := Command{
		Name:        "k8s",
		Aliases:     []string{"pods"},
		Type:        TypePrompt,
		Source:      SourceBuiltin,
		Builtin:     true,
		Description: "inspect k8s",
		Handler: func(ctx *Context) (Result, error) {
			return Result{Type: TypePrompt, Prompt: "ok"}, nil
		},
	}
	if err := reg.Register(cmd); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if got, ok := reg.Find("pods"); !ok || got.Name != "k8s" {
		t.Fatalf("Find alias=%#v ok=%v, want k8s", got, ok)
	}
	matches := reg.Complete("po", 8)
	if len(matches) != 1 || matches[0].Name != "k8s" {
		t.Fatalf("Complete=%#v, want k8s", matches)
	}
}

func TestRegistryRejectsBuiltinOverride(t *testing.T) {
	t.Parallel()
	reg := CreateDefaultRegistry(t.TempDir())
	err := reg.Register(Command{
		Name:        "help",
		Type:        TypePrompt,
		Source:      SourceProject,
		Description: "override",
		Handler: func(ctx *Context) (Result, error) {
			return Result{Type: TypePrompt, Prompt: "bad"}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "builtin") {
		t.Fatalf("Register override error=%v, want builtin conflict", err)
	}
}

func TestBuiltinPromptsContainSafetyConstraints(t *testing.T) {
	t.Parallel()
	reg := CreateDefaultRegistry(t.TempDir())
	cmd, ok := reg.Find("k8s")
	if !ok {
		t.Fatal("missing k8s builtin")
	}
	result, err := cmd.Handler(&Context{Args: "pods -n prod"})
	if err != nil {
		t.Fatalf("k8s handler failed: %v", err)
	}
	for _, want := range []string{"k8s_monitor", "禁止", "kubectl delete"} {
		if !strings.Contains(result.Prompt, want) {
			t.Fatalf("k8s prompt missing %q: %s", want, result.Prompt)
		}
	}
}

func TestRegistryRejectsProjectNameCollision(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	first := Command{Name: "triage", Type: TypePrompt, Source: SourceProject, Description: "first", Handler: func(ctx *Context) (Result, error) {
		return Result{Type: TypePrompt, Prompt: "first"}, nil
	}}
	second := Command{Name: "triage", Type: TypePrompt, Source: SourceProject, Description: "second", Handler: func(ctx *Context) (Result, error) {
		return Result{Type: TypePrompt, Prompt: "second"}, nil
	}}
	if err := reg.Register(first); err != nil {
		t.Fatalf("Register first failed: %v", err)
	}
	if err := reg.Register(second); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("Register duplicate error=%v, want already registered", err)
	}
}

func TestDefaultRegistryIncludesPhaseFiveControlCommands(t *testing.T) {
	t.Parallel()
	reg := CreateDefaultRegistry(t.TempDir())
	for _, name := range []string{"workflow", "rag", "model", "tools", "skills", "incident", "diagnose", "logs", "metrics", "k8s"} {
		if _, ok := reg.Find(name); !ok {
			t.Fatalf("expected command %q", name)
		}
	}
}

func TestRegistryRejectsProjectAliasCollision(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	first := Command{Name: "logs", Aliases: []string{"last"}, Type: TypePrompt, Source: SourceProject, Description: "first", Handler: func(ctx *Context) (Result, error) {
		return Result{Type: TypePrompt, Prompt: "first"}, nil
	}}
	second := Command{Name: "errors", Aliases: []string{"last"}, Type: TypePrompt, Source: SourceProject, Description: "second", Handler: func(ctx *Context) (Result, error) {
		return Result{Type: TypePrompt, Prompt: "second"}, nil
	}}
	if err := reg.Register(first); err != nil {
		t.Fatalf("Register first failed: %v", err)
	}
	if err := reg.Register(second); err == nil || !strings.Contains(err.Error(), "already points") {
		t.Fatalf("Register alias duplicate error=%v, want alias conflict", err)
	}
}
