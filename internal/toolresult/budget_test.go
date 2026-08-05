package toolresult

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestApplySpillsSingleLargeToolResult(t *testing.T) {
	t.Parallel()

	state := NewContentReplacementState()
	msgs := []*schema.Message{
		schema.ToolMessage(strings.Repeat("x", 120), "call-1", schema.WithToolName("Grep")),
	}
	cfg := Config{SingleResultBytes: 50, MessageAggregateBytes: 1_000, PreviewBytes: 20}

	out, records, err := Apply(msgs, t.TempDir(), state, cfg)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one spill record, got %d", len(records))
	}
	if !strings.Contains(out[0].Content, "<persisted-tool-result>") {
		t.Fatalf("expected persisted preview, got %q", out[0].Content)
	}
	if _, err := os.Stat(records[0].SpillPath); err != nil {
		t.Fatalf("expected spill file: %v", err)
	}
	if got, _ := os.ReadFile(records[0].SpillPath); string(got) != strings.Repeat("x", 120) {
		t.Fatalf("spill content mismatch")
	}
}

func TestApplySpillsLargestResultsUntilAggregateIsUnderBudget(t *testing.T) {
	t.Parallel()

	state := NewContentReplacementState()
	msgs := []*schema.Message{
		schema.ToolMessage(strings.Repeat("a", 4_500), "a", schema.WithToolName("ReadFile")),
		schema.ToolMessage(strings.Repeat("b", 4_800), "b", schema.WithToolName("ReadFile")),
		schema.ToolMessage(strings.Repeat("c", 4_900), "c", schema.WithToolName("ReadFile")),
	}
	cfg := Config{SingleResultBytes: 10_000, MessageAggregateBytes: 10_000, PreviewBytes: 5}

	out, records, err := Apply(msgs, t.TempDir(), state, cfg)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one aggregate spill, got %d", len(records))
	}
	if records[0].ToolCallID != "c" {
		t.Fatalf("expected largest result c to spill, got %s", records[0].ToolCallID)
	}
	if !strings.Contains(out[2].Content, "<persisted-tool-result>") {
		t.Fatalf("expected c to be previewed")
	}
}

func TestApplyReplaysStableReplacement(t *testing.T) {
	t.Parallel()

	state := NewContentReplacementState()
	workDir := t.TempDir()
	cfg := Config{SingleResultBytes: 10, MessageAggregateBytes: 1_000, PreviewBytes: 4}
	msgs := []*schema.Message{
		schema.ToolMessage(strings.Repeat("x", 20), "stable", schema.WithToolName("Grep")),
	}

	first, _, err := Apply(msgs, workDir, state, cfg)
	if err != nil {
		t.Fatalf("first Apply returned error: %v", err)
	}
	secondMsgs := []*schema.Message{
		schema.ToolMessage(strings.Repeat("y", 20), "stable", schema.WithToolName("Grep")),
	}
	second, records, err := Apply(secondMsgs, workDir, state, cfg)
	if err != nil {
		t.Fatalf("second Apply returned error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected replacement replay without new spill, got %d records", len(records))
	}
	if first[0].Content != second[0].Content {
		t.Fatalf("replacement was not stable")
	}
}

func TestApplyKeepsMarkedSpillReadbackOriginal(t *testing.T) {
	t.Parallel()

	state := NewContentReplacementState()
	state.MarkOriginal("readback")
	msgs := []*schema.Message{
		schema.ToolMessage(strings.Repeat("x", 120), "readback", schema.WithToolName("ReadFile")),
	}
	out, records, err := Apply(msgs, t.TempDir(), state, Config{SingleResultBytes: 10})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected no spill for marked readback, got %d", len(records))
	}
	if out[0].Content != msgs[0].Content {
		t.Fatalf("marked readback content changed")
	}
}

func TestIsPathInSpillDir(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	spillPath := filepath.Join(workDir, DefaultSpillDir, "x.txt")
	if !IsPathInSpillDir(spillPath, workDir, Config{}) {
		t.Fatalf("expected spill path to be recognized")
	}
	if IsPathInSpillDir(filepath.Join(workDir, "other.txt"), workDir, Config{}) {
		t.Fatalf("did not expect non-spill path to be recognized")
	}
}
