package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"
)

const ReadFileDescription = "Read a file and return its contents with 1-based line numbers. Use offset and limit for large files."
const EditFileDescription = "Replace one exact unique string in a file. The file must be read with ReadFile before editing."
const WriteFileDescription = "Write content to a file. New files are allowed; overwriting existing files requires a prior ReadFile."
const GlobDescription = "Find files matching a glob pattern. Skips .git, node_modules, .cache, __pycache__, and similar directories."
const GrepDescription = "Search file contents using a regex pattern and return file:line:content matches."
const ToolSearchDescription = "Search and discover deferred business tools. Use select:ToolName for exact discovery or keywords for search."
const InvokeDeferredToolDescription = "Invoke a previously discovered deferred business tool by name with JSON arguments."

type ReadFileTool struct{ FileStateCache *FileStateCache }

func (t *ReadFileTool) Name() string           { return "ReadFile" }
func (t *ReadFileTool) Description() string    { return ReadFileDescription }
func (t *ReadFileTool) Category() ToolCategory { return CategoryRead }
func (t *ReadFileTool) Schema() map[string]any {
	return schemaMap(t.Name(), t.Description(), map[string]any{
		"file_path": map[string]any{"type": "string", "description": "Absolute or relative file path"},
		"offset":    map[string]any{"type": "integer", "description": "0-based line offset", "default": 0},
		"limit":     map[string]any{"type": "integer", "description": "Maximum lines to read", "default": 2000},
	}, []string{"file_path"})
}
func (t *ReadFileTool) Execute(_ context.Context, args map[string]any) ToolResult {
	filePath, _ := args["file_path"].(string)
	if err := ensureFilePath(filePath); err != nil {
		return errorResult(err.Error())
	}
	offset := intArg(args, "offset", 0)
	limit := intArg(args, "limit", 2000)
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 2000 {
		limit = 2000
	}
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return errorResult(fmt.Sprintf("Error: file not found: %s", filePath))
	}
	if err != nil {
		return errorResult(fmt.Sprintf("Error: %s", err))
	}
	if info.IsDir() {
		return errorResult(fmt.Sprintf("Error: not a file: %s", filePath))
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return errorResult(fmt.Sprintf("Error reading file: %s", err))
	}
	lines := strings.Split(string(data), "\n")
	if offset >= len(lines) {
		return ToolResult{Output: ""}
	}
	end := offset + limit
	if end > len(lines) {
		end = len(lines)
	}
	if t.FileStateCache != nil {
		t.FileStateCache.Record(filePath, string(data), info.ModTime().UnixMilli())
	}
	var sb strings.Builder
	for i, line := range lines[offset:end] {
		if i > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "%d\t%s", offset+i+1, line)
	}
	return ToolResult{Output: truncateOutput(sb.String())}
}

func intArg(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return def
}

func schemaMap(name, desc string, properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"name":        name,
		"description": desc,
		"input_schema": map[string]any{
			"type":       "object",
			"properties": properties,
			"required":   required,
		},
	}
}

func errorResult(msg string) ToolResult { return ToolResult{Output: msg, IsError: true} }

func truncateOutput(out string) string {
	if len([]rune(out)) <= MaxOutputChars {
		return out
	}
	r := []rune(out)
	return string(r[:MaxOutputChars]) + "\n... (truncated)"
}
