package mcp

import (
	"strings"
	"testing"
)

func TestNamespaceAvoidsToolNameCollision(t *testing.T) {
	if Namespace("github", "search") == Namespace("slack", "search") {
		t.Fatal("namespaces should avoid collisions")
	}
}

func TestServerInstructionIsRuntimeNoticeNotSystemPrompt(t *testing.T) {
	n := InstructionNotice(ServerConfig{Name: "docs", Instruction: "use docs"})
	if !strings.Contains(n.Source, "mcp:docs") || n.Trust != "untrusted_evidence" {
		t.Fatalf("unexpected notice: %+v", n)
	}
}

func TestUnavailableServerDegradesOnlyToolSource(t *testing.T) {
	m := NewManager([]ServerConfig{{Name: "docs"}})
	h := m.Health("docs")
	if h.Available || h.Reason == "" {
		t.Fatalf("expected degraded server health: %+v", h)
	}
}
