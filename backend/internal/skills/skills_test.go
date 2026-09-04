package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogLoadsMetadataBeforeFullContent(t *testing.T) {
	catalog, err := NewCatalog([]Metadata{{Name: "diagnose", Description: "diagnose incidents", Source: SourceProject, Triggers: []string{"故障"}, Path: "diagnose/SKILL.md"}})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	matched := catalog.Match("线上故障")
	if len(matched) != 1 || matched[0].Name != "diagnose" || matched[0].Description == "" {
		t.Fatalf("unexpected matches: %+v", matched)
	}
}

func TestLoadRejectsEscapingPathsAndLoadsSkill(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "diagnose")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Diagnose"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, Metadata{Name: "bad", Path: "../SKILL.md"}); err == nil {
		t.Fatal("expected escaping path error")
	}
	skill, err := Load(root, Metadata{Name: "diagnose", Path: "diagnose/SKILL.md"})
	if err != nil || skill.Content != "# Diagnose" {
		t.Fatalf("Load: skill=%+v err=%v", skill, err)
	}
}

func TestAllowedToolsOnlyNarrowsParentPolicy(t *testing.T) {
	if !AllowedToolsSubset([]string{"k8s_monitor", "metrics_collector"}, []string{"k8s_monitor"}) {
		t.Fatal("expected subset allowed")
	}
	if AllowedToolsSubset([]string{"k8s_monitor"}, []string{"execute_step"}) {
		t.Fatal("skill must not elevate allowed tools")
	}
}
