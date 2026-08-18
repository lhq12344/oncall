package toolkit

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type GlobTool struct{}

func (t *GlobTool) Name() string           { return "Glob" }
func (t *GlobTool) Description() string    { return GlobDescription }
func (t *GlobTool) Category() ToolCategory { return CategoryRead }
func (t *GlobTool) Schema() map[string]any {
	return schemaMap(t.Name(), t.Description(), map[string]any{
		"pattern": map[string]any{"type": "string", "description": "Glob pattern, e.g. **/*.go"},
		"path":    map[string]any{"type": "string", "description": "Base directory", "default": "."},
	}, []string{"pattern"})
}
func (t *GlobTool) Execute(_ context.Context, args map[string]any) ToolResult {
	pattern, _ := args["pattern"].(string)
	basePath, _ := args["path"].(string)
	if basePath == "" {
		basePath = "."
	}
	if pattern == "" {
		return errorResult("Error: pattern is required")
	}
	info, err := os.Stat(basePath)
	if err != nil || !info.IsDir() {
		return errorResult(fmt.Sprintf("Error: path not found: %s", basePath))
	}
	recursive := false
	basePattern := pattern
	for strings.HasPrefix(basePattern, "**/") {
		basePattern = basePattern[3:]
		recursive = true
	}
	var matches []string
	err = filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if SkipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(basePath, path)
		matched := false
		if recursive {
			matched, _ = filepath.Match(basePattern, filepath.Base(path))
		} else {
			matched, _ = filepath.Match(pattern, rel)
			if !matched {
				matched, _ = filepath.Match(pattern, filepath.Base(path))
			}
		}
		if matched {
			matches = append(matches, rel)
		}
		return nil
	})
	if err != nil {
		return errorResult(fmt.Sprintf("Error: %s", err))
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return ToolResult{Output: "No files matched the pattern."}
	}
	return ToolResult{Output: truncateOutput(strings.Join(matches, "\n"))}
}

type GrepTool struct{}

func (t *GrepTool) Name() string           { return "Grep" }
func (t *GrepTool) Description() string    { return GrepDescription }
func (t *GrepTool) Category() ToolCategory { return CategoryRead }
func (t *GrepTool) Schema() map[string]any {
	return schemaMap(t.Name(), t.Description(), map[string]any{
		"pattern": map[string]any{"type": "string", "description": "Regex pattern"},
		"path":    map[string]any{"type": "string", "description": "Base directory", "default": "."},
		"include": map[string]any{"type": "string", "description": "Filename glob filter, e.g. *.go"},
	}, []string{"pattern"})
}
func (t *GrepTool) Execute(_ context.Context, args map[string]any) ToolResult {
	pattern, _ := args["pattern"].(string)
	basePath, _ := args["path"].(string)
	include, _ := args["include"].(string)
	if basePath == "" {
		basePath = "."
	}
	if pattern == "" {
		return errorResult("Error: pattern is required")
	}
	info, err := os.Stat(basePath)
	if err != nil || !info.IsDir() {
		return errorResult(fmt.Sprintf("Error: path not found: %s", basePath))
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return errorResult(fmt.Sprintf("Error: invalid regex: %s", err))
	}
	var files []string
	_ = filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if SkipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if include != "" {
			matched, _ := filepath.Match(include, info.Name())
			if !matched {
				return nil
			}
		}
		files = append(files, path)
		return nil
	})
	sort.Strings(files)
	var results []string
	for _, fpath := range files {
		f, err := os.Open(fpath)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				rel, _ := filepath.Rel(basePath, fpath)
				results = append(results, fmt.Sprintf("%s:%d:%s", rel, lineNum, line))
			}
		}
		_ = f.Close()
	}
	if len(results) == 0 {
		return ToolResult{Output: "No matches found."}
	}
	return ToolResult{Output: truncateOutput(strings.Join(results, "\n"))}
}
