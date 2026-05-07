package dialogue

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/cloudwego/eino/adk/filesystem"
)

type readOnlySkillFilesystemBackend struct {
	root string
}

func newReadOnlySkillFilesystemBackend(root string) *readOnlySkillFilesystemBackend {
	absRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		absRoot = strings.TrimSpace(root)
	}
	return &readOnlySkillFilesystemBackend{
		root: filepath.Clean(absRoot),
	}
}

func (b *readOnlySkillFilesystemBackend) LsInfo(context.Context, *filesystem.LsInfoRequest) ([]filesystem.FileInfo, error) {
	return nil, fmt.Errorf("ls is not supported by dialogue skill filesystem backend")
}

func (b *readOnlySkillFilesystemBackend) Read(_ context.Context, req *filesystem.ReadRequest) (*filesystem.FileContent, error) {
	if req == nil {
		return nil, fmt.Errorf("read request is required")
	}
	path, err := b.resolvePath(req.FilePath)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	offset := req.Offset
	if offset < 1 {
		offset = 1
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 2000
	}

	var lines []string
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if lineNo < offset {
			continue
		}
		if len(lines) >= limit {
			break
		}
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &filesystem.FileContent{Content: strings.Join(lines, "\n")}, nil
}

func (b *readOnlySkillFilesystemBackend) GrepRaw(context.Context, *filesystem.GrepRequest) ([]filesystem.GrepMatch, error) {
	return nil, fmt.Errorf("grep is not supported by dialogue skill filesystem backend")
}

func (b *readOnlySkillFilesystemBackend) GlobInfo(_ context.Context, req *filesystem.GlobInfoRequest) ([]filesystem.FileInfo, error) {
	if req == nil {
		return nil, fmt.Errorf("glob request is required")
	}
	basePath := req.Path
	if strings.TrimSpace(basePath) == "" {
		basePath = b.root
	}
	basePath, err := b.resolvePath(basePath)
	if err != nil {
		return nil, err
	}

	matches, err := doublestar.FilepathGlob(filepath.Join(basePath, filepath.FromSlash(req.Pattern)))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	infos := make([]filesystem.FileInfo, 0, len(matches))
	for _, match := range matches {
		path, err := b.resolvePath(match)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		infos = append(infos, filesystem.FileInfo{
			Path:       path,
			IsDir:      info.IsDir(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return infos, nil
}

func (b *readOnlySkillFilesystemBackend) Write(context.Context, *filesystem.WriteRequest) error {
	return fmt.Errorf("write is not supported by dialogue skill filesystem backend")
}

func (b *readOnlySkillFilesystemBackend) Edit(context.Context, *filesystem.EditRequest) error {
	return fmt.Errorf("edit is not supported by dialogue skill filesystem backend")
}

func (b *readOnlySkillFilesystemBackend) resolvePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(b.root, path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absPath = filepath.Clean(absPath)

	rel, err := filepath.Rel(b.root, absPath)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q is outside skill directory", path)
	}
	return absPath, nil
}
