package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type EditFileTool struct{ FileStateCache *FileStateCache }

func (t *EditFileTool) Name() string           { return "EditFile" }
func (t *EditFileTool) Description() string    { return EditFileDescription }
func (t *EditFileTool) Category() ToolCategory { return CategoryWrite }
func (t *EditFileTool) Schema() map[string]any {
	return schemaMap(t.Name(), t.Description(), map[string]any{
		"file_path":  map[string]any{"type": "string", "description": "Path to edit"},
		"old_string": map[string]any{"type": "string", "description": "Exact unique string to replace"},
		"new_string": map[string]any{"type": "string", "description": "Replacement string"},
	}, []string{"file_path", "old_string", "new_string"})
}
func (t *EditFileTool) Execute(_ context.Context, args map[string]any) ToolResult {
	filePath, _ := args["file_path"].(string)
	oldStr, _ := args["old_string"].(string)
	newStr, _ := args["new_string"].(string)
	if err := ensureFilePath(filePath); err != nil {
		return errorResult(err.Error())
	}
	if oldStr == "" {
		return errorResult("Error: old_string is required")
	}
	if oldStr == newStr {
		return errorResult("Error: new_string must differ from old_string")
	}
	if ok, msg := t.FileStateCache.Check(filePath); !ok {
		return errorResult(msg)
	}
	data, err := os.ReadFile(filePath)
	if os.IsNotExist(err) {
		return errorResult(fmt.Sprintf("Error: file not found: %s", filePath))
	}
	if err != nil {
		return errorResult(fmt.Sprintf("Error reading file: %s", err))
	}
	content := string(data)
	count := strings.Count(content, oldStr)
	if count == 0 {
		return errorResult("Error: old_string not found in file")
	}
	if count > 1 {
		return errorResult(fmt.Sprintf("Error: old_string found %d times, must be unique", count))
	}
	newContent := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(filePath, []byte(newContent), 0o644); err != nil {
		return errorResult(fmt.Sprintf("Error writing file: %s", err))
	}
	t.FileStateCache.Update(filePath, newContent)
	return ToolResult{Output: fmt.Sprintf("Updated %s", filePath)}
}

type WriteFileTool struct{ FileStateCache *FileStateCache }

func (t *WriteFileTool) Name() string           { return "WriteFile" }
func (t *WriteFileTool) Description() string    { return WriteFileDescription }
func (t *WriteFileTool) Category() ToolCategory { return CategoryWrite }
func (t *WriteFileTool) Schema() map[string]any {
	return schemaMap(t.Name(), t.Description(), map[string]any{
		"file_path": map[string]any{"type": "string", "description": "Path to write"},
		"content":   map[string]any{"type": "string", "description": "Full content to write"},
	}, []string{"file_path", "content"})
}
func (t *WriteFileTool) Execute(_ context.Context, args map[string]any) ToolResult {
	filePath, _ := args["file_path"].(string)
	content, _ := args["content"].(string)
	if err := ensureFilePath(filePath); err != nil {
		return errorResult(err.Error())
	}
	if _, err := os.Stat(filePath); err == nil {
		if ok, msg := t.FileStateCache.Check(filePath); !ok {
			return errorResult(msg)
		}
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return errorResult(fmt.Sprintf("Error creating directories: %s", err))
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return errorResult(fmt.Sprintf("Error writing file: %s", err))
	}
	t.FileStateCache.Update(filePath, content)
	return ToolResult{Output: fmt.Sprintf("Successfully wrote to %s", filePath)}
}
