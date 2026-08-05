package toolkit

import (
	"context"
	"sort"
	"strings"
	"sync"
)

const MaxOutputChars = 10000

var SkipDirs = map[string]bool{
	".git": true, ".venv": true, "node_modules": true,
	"__pycache__": true, ".tox": true, ".mypy_cache": true,
	".cache": true, ".gocache": true, "dist": true, "build": true,
}

type ToolResult struct {
	Output  string
	IsError bool
}

type ToolCategory string

const (
	CategoryRead    ToolCategory = "read"
	CategoryWrite   ToolCategory = "write"
	CategoryCommand ToolCategory = "command"
)

type Tool interface {
	Name() string
	Description() string
	Category() ToolCategory
	Schema() map[string]any
	Execute(ctx context.Context, args map[string]any) ToolResult
}

type DeferrableTool interface {
	ShouldDefer() bool
}

type Registry struct {
	mu              sync.RWMutex
	tools           map[string]Tool
	deferred        map[string]bool
	discoveredTools map[string]bool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool), deferred: make(map[string]bool), discoveredTools: make(map[string]bool)}
}

func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
	if dt, ok := t.(DeferrableTool); ok && dt.ShouldDefer() {
		r.deferred[t.Name()] = true
	}
}

func (r *Registry) RegisterDeferred(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
	r.deferred[t.Name()] = true
}

func (r *Registry) Get(name string) Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

func (r *Registry) IsDeferred(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.deferred[name]
}

func (r *Registry) MarkDiscovered(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deferred[name] {
		r.discoveredTools[name] = true
	}
}

func (r *Registry) IsDiscovered(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.discoveredTools[name]
}

func (r *Registry) ListAlways() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Tool, 0, len(r.tools))
	for name, t := range r.tools {
		if !r.deferred[name] {
			result = append(result, t)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name() < result[j].Name() })
	return result
}

func (r *Registry) GetDeferredToolNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for name := range r.deferred {
		if !r.discoveredTools[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func (r *Registry) SearchDeferred(query string, maxResults int) []map[string]any {
	query = strings.ToLower(strings.TrimSpace(query))
	r.mu.RLock()
	defer r.mu.RUnlock()
	var matches []map[string]any
	for name, t := range r.tools {
		if !r.deferred[name] {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(name), query) || strings.Contains(strings.ToLower(t.Description()), query) {
			matches = append(matches, t.Schema())
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		ni, _ := matches[i]["name"].(string)
		nj, _ := matches[j]["name"].(string)
		return ni < nj
	})
	if maxResults > 0 && len(matches) > maxResults {
		matches = matches[:maxResults]
	}
	return matches
}

func (r *Registry) FindDeferredByNames(names []string) []map[string]any {
	wanted := map[string]bool{}
	for _, n := range names {
		wanted[strings.ToLower(strings.TrimSpace(n))] = true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var matches []map[string]any
	for name, t := range r.tools {
		if r.deferred[name] && wanted[strings.ToLower(name)] {
			matches = append(matches, t.Schema())
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		ni, _ := matches[i]["name"].(string)
		nj, _ := matches[j]["name"].(string)
		return ni < nj
	})
	return matches
}
