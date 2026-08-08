package toolkit

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
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
	mu                sync.RWMutex
	tools             map[string]Tool
	deferred          map[string]bool
	scopedDiscoveries map[string]map[string]bool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool), deferred: make(map[string]bool), scopedDiscoveries: make(map[string]map[string]bool)}
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

const deferredDiscoverySessionKey = "toolkit.deferred_discovered_tools"

type deferredDiscoveryContextKey struct{}

// ContextWithDeferredDiscoverySession scopes deferred tool discovery outside an ADK run.
// Production ADK runs use session values keyed by session_id; tests and direct callers can
// use this helper to avoid cross-session discovery leakage.
func ContextWithDeferredDiscoverySession(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, deferredDiscoveryContextKey{}, strings.TrimSpace(sessionID))
}

func (r *Registry) MarkDiscovered(ctx context.Context, name string) {
	name = strings.TrimSpace(name)
	if name == "" || !r.IsDeferred(name) {
		return
	}
	if discoveries, ok := sessionDiscoverySet(ctx, true); ok {
		discoveries[name] = true
		return
	}
	scope := deferredDiscoveryScope(ctx)
	if scope == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.scopedDiscoveries == nil {
		r.scopedDiscoveries = make(map[string]map[string]bool)
	}
	if r.scopedDiscoveries[scope] == nil {
		r.scopedDiscoveries[scope] = make(map[string]bool)
	}
	r.scopedDiscoveries[scope][name] = true
}

func (r *Registry) IsDiscovered(ctx context.Context, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if discoveries, ok := sessionDiscoverySet(ctx, false); ok {
		return discoveries[name]
	}
	scope := deferredDiscoveryScope(ctx)
	if scope == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.scopedDiscoveries[scope] != nil && r.scopedDiscoveries[scope][name]
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

func (r *Registry) GetDeferredToolNames(ctx context.Context) []string {
	discoveries, hasSessionDiscoveries := sessionDiscoverySet(ctx, false)
	scope := deferredDiscoveryScope(ctx)
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for name := range r.deferred {
		discovered := false
		if hasSessionDiscoveries {
			discovered = discoveries[name]
		} else if scope != "" && r.scopedDiscoveries[scope] != nil {
			discovered = r.scopedDiscoveries[scope][name]
		}
		if !discovered {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func sessionDiscoverySet(ctx context.Context, create bool) (map[string]bool, bool) {
	if ctx == nil {
		return nil, false
	}
	if raw, ok := adk.GetSessionValue(ctx, deferredDiscoverySessionKey); ok {
		switch typed := raw.(type) {
		case map[string]bool:
			return typed, true
		case map[string]any:
			out := make(map[string]bool, len(typed))
			for name, value := range typed {
				if discovered, ok := value.(bool); ok && discovered {
					out[name] = true
				}
			}
			adk.AddSessionValue(ctx, deferredDiscoverySessionKey, out)
			return out, true
		}
	}
	if !create {
		return nil, false
	}
	out := make(map[string]bool)
	adk.AddSessionValue(ctx, deferredDiscoverySessionKey, out)
	if raw, ok := adk.GetSessionValue(ctx, deferredDiscoverySessionKey); ok {
		if typed, ok := raw.(map[string]bool); ok {
			return typed, true
		}
	}
	return nil, false
}

func deferredDiscoveryScope(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value, ok := ctx.Value(deferredDiscoveryContextKey{}).(string); ok {
		if scope := strings.TrimSpace(value); scope != "" {
			return scope
		}
	}
	if value, ok := adk.GetSessionValue(ctx, "session_id"); ok && value != nil {
		if scope := strings.TrimSpace(fmt.Sprint(value)); scope != "" {
			return scope
		}
	}
	return ""
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
