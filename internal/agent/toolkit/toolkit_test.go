package toolkit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_agent/internal/permissions"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type stubEinoTool struct {
	name string
	desc string
	out  string
}

func (s stubEinoTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: s.name,
		Desc: s.desc,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"value": {Type: schema.String, Desc: "value"},
		}),
	}, nil
}

func (s stubEinoTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	return s.out, nil
}

func TestRegistrySearchAndInvokeRequiresDiscovery(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	target := NewDeferredEinoTool(context.Background(), stubEinoTool{name: "k8s_monitor", desc: "inspect kubernetes", out: "ok"})
	reg.RegisterDeferred(target)

	invoke := &InvokeDeferredTool{Registry: reg, Checker: permissions.NewChecker(permissions.Options{Mode: permissions.ModeBypass})}
	res := invoke.Execute(context.Background(), map[string]any{"tool_name": "k8s_monitor", "arguments": map[string]any{"value": "pod"}})
	if !res.IsError || !strings.Contains(res.Output, "ToolSearch") {
		t.Fatalf("expected undiscovered tool to fail, got %#v", res)
	}

	search := &ToolSearchTool{Registry: reg}
	res = search.Execute(context.Background(), map[string]any{"query": "select:k8s_monitor"})
	if res.IsError || !reg.IsDiscovered("k8s_monitor") {
		t.Fatalf("expected discovery to succeed, got %#v", res)
	}

	res = invoke.Execute(context.Background(), map[string]any{"tool_name": "k8s_monitor", "arguments": map[string]any{"value": "pod"}})
	if res.IsError || res.Output != "ok" {
		t.Fatalf("expected invoked output, got %#v", res)
	}
}

func TestInvokeDeferredToolAllowsSafeReadToolInDefaultMode(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	reg.RegisterDeferred(NewDeferredEinoTool(context.Background(), stubEinoTool{name: "k8s_monitor", desc: "inspect kubernetes", out: "ok"}))
	reg.MarkDiscovered("k8s_monitor")

	checker := permissions.NewChecker(permissions.Options{ProjectRoot: t.TempDir(), Mode: permissions.ModeDefault})
	invoke := &InvokeDeferredTool{Registry: reg, Checker: checker}
	res := invoke.Execute(context.Background(), map[string]any{"tool_name": "k8s_monitor", "arguments": map[string]any{"value": "pod"}})
	if res.IsError || res.Output != "ok" {
		t.Fatalf("expected safe deferred read tool to run without approval, got %#v", res)
	}
}

func TestInvokeDeferredToolChecksTargetPermission(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	reg := NewRegistry()
	reg.RegisterDeferred(NewDeferredEinoTool(context.Background(), stubEinoTool{name: "WriteFile", desc: "write file", out: "should not run"}))
	reg.MarkDiscovered("WriteFile")

	checker := permissions.NewChecker(permissions.Options{ProjectRoot: root, Mode: permissions.ModeBypass})
	invoke := &InvokeDeferredTool{Registry: reg, Checker: checker}
	res := invoke.Execute(context.Background(), map[string]any{
		"tool_name": "WriteFile",
		"arguments": map[string]any{"file_path": filepath.Join(root, ".env"), "content": "SECRET=x"},
	})
	if !res.IsError || !strings.Contains(res.Output, "permission denied") {
		t.Fatalf("expected target permission denial, got %#v", res)
	}
}

func TestFileToolsReadBeforeEditAndWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "a.txt")
	if err := os.WriteFile(file, []byte("one\ntwo\nthree"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := NewFileStateCache()
	edit := &EditFileTool{FileStateCache: cache}
	if res := edit.Execute(context.Background(), map[string]any{"file_path": file, "old_string": "two", "new_string": "2"}); !res.IsError {
		t.Fatalf("expected edit before read to fail, got %#v", res)
	}

	read := &ReadFileTool{FileStateCache: cache}
	res := read.Execute(context.Background(), map[string]any{"file_path": file, "offset": 1, "limit": 1})
	if res.IsError || !strings.Contains(res.Output, "2\ttwo") {
		t.Fatalf("unexpected read output: %#v", res)
	}

	res = edit.Execute(context.Background(), map[string]any{"file_path": file, "old_string": "two", "new_string": "2"})
	if res.IsError {
		t.Fatalf("expected edit success: %#v", res)
	}
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), "2") {
		t.Fatalf("expected file edited: %s", data)
	}

	write := &WriteFileTool{FileStateCache: cache}
	newFile := filepath.Join(root, "nested", "new.txt")
	res = write.Execute(context.Background(), map[string]any{"file_path": newFile, "content": "created"})
	if res.IsError {
		t.Fatalf("expected new file write success: %#v", res)
	}
}

func TestGlobGrepSkipDirsAndStableOrder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, ".git", "hidden.go"), []byte("needle"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "pkg", "b.go"), []byte("needle b"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "pkg", "a.go"), []byte("needle a"), 0o644)

	glob := &GlobTool{}
	res := glob.Execute(context.Background(), map[string]any{"path": root, "pattern": "**/*.go"})
	if res.IsError || strings.Contains(res.Output, ".git") {
		t.Fatalf("unexpected glob output: %#v", res)
	}

	grep := &GrepTool{}
	res = grep.Execute(context.Background(), map[string]any{"path": root, "pattern": "needle", "include": "*.go"})
	if res.IsError || strings.Contains(res.Output, ".git") {
		t.Fatalf("unexpected grep output: %#v", res)
	}
	lines := strings.Split(res.Output, "\n")
	if len(lines) != 2 || lines[0] > lines[1] {
		t.Fatalf("expected stable sorted grep results, got %q", res.Output)
	}
}

func TestAdapterDeniesSensitiveWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	checker := permissions.NewChecker(permissions.Options{ProjectRoot: root, Mode: permissions.ModeBypass})
	adapter := NewEinoAdapter(&WriteFileTool{FileStateCache: NewFileStateCache()}, checker).(einotool.InvokableTool)
	args, err := json.Marshal(map[string]any{
		"file_path": filepath.Join(root, ".env"),
		"content":   "SECRET=x",
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := adapter.InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["success"] != false {
		t.Fatalf("expected denied JSON result, got %s", out)
	}
}
