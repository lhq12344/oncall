package dialogue

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudwego/eino/adk/filesystem"
)

func TestReadOnlySkillFilesystemBackendRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	backend := newReadOnlySkillFilesystemBackend(root)

	if _, err := backend.Read(context.Background(), &filesystem.ReadRequest{FilePath: "../secret.txt"}); err == nil {
		t.Fatalf("expected path escape to be rejected")
	}
	if _, err := backend.Read(context.Background(), &filesystem.ReadRequest{FilePath: filepath.Join(root, "..", "secret.txt")}); err == nil {
		t.Fatalf("expected absolute path escape to be rejected")
	}
}

func TestReadOnlySkillFilesystemBackendGlobImmediateSkills(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "sample")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: sample\ndescription: sample\n---\nbody\n"), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	backend := newReadOnlySkillFilesystemBackend(root)
	infos, err := backend.GlobInfo(context.Background(), &filesystem.GlobInfoRequest{Path: root, Pattern: "*/SKILL.md"})
	if err != nil {
		t.Fatalf("glob skill files: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected one skill file, got %d", len(infos))
	}
}
