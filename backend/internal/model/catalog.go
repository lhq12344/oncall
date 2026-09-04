package model

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Role string

const (
	RoleRouter           Role = "router"
	RoleDialogue         Role = "dialogue"
	RoleRAGRewrite       Role = "rag_rewrite"
	RoleDiagnosis        Role = "diagnosis"
	RolePlan             Role = "plan"
	RoleExecutionSummary Role = "execution_summary"
	RoleEvaluator        Role = "evaluator"
	RoleMemoryExtractor  Role = "memory_extractor"
)

type Capabilities struct {
	Streaming bool
	Tools     bool
	Vision    bool
	JSON      bool
	Tokenizer string
}

type RetryPolicy struct {
	MaxAttempts int
	Backoff     time.Duration
}

// Profile describes one model option available to the runtime.
type Profile struct {
	ID              string
	Provider        string
	DisplayName     string
	Model           string
	Role            string
	Roles           []Role
	ContextWindow   int
	MaxInputTokens  int
	MaxOutputTokens int
	SupportsTools   bool
	SupportsStream  bool
	Capabilities    Capabilities
	Timeout         time.Duration
	RetryPolicy     RetryPolicy
	CostClass       string
	Default         bool
}

// Catalog resolves model profiles without exposing provider SDKs to callers.
type Catalog struct {
	profiles map[string]Profile
	defaults map[string]string
}

func NewCatalog(profiles []Profile) (*Catalog, error) {
	c := &Catalog{profiles: map[string]Profile{}, defaults: map[string]string{}}
	for _, p := range profiles {
		p.ID = strings.TrimSpace(p.ID)
		if p.ID == "" {
			return nil, fmt.Errorf("model profile id is required")
		}
		if _, exists := c.profiles[p.ID]; exists {
			return nil, fmt.Errorf("duplicate model profile %q", p.ID)
		}
		if p.Provider == "" {
			p.Provider = "unknown"
		}
		if p.Model == "" {
			p.Model = p.ID
		}
		if p.Role == "" {
			p.Role = "default"
		}
		if len(p.Roles) == 0 {
			p.Roles = []Role{Role(p.Role)}
		}
		if p.ContextWindow == 0 {
			p.ContextWindow = p.MaxInputTokens
		}
		if p.ContextWindow < 0 {
			return nil, fmt.Errorf("model profile %q context window must not be negative", p.ID)
		}
		if p.Timeout < 0 {
			return nil, fmt.Errorf("model profile %q timeout must not be negative", p.ID)
		}
		if p.Capabilities == (Capabilities{}) {
			p.Capabilities = Capabilities{Streaming: p.SupportsStream, Tools: p.SupportsTools}
		}
		c.profiles[p.ID] = p
		if p.Default {
			for _, role := range p.Roles {
				roleKey := string(role)
				if existing := c.defaults[roleKey]; existing != "" {
					return nil, fmt.Errorf("multiple default model profiles for role %q: %s and %s", roleKey, existing, p.ID)
				}
				c.defaults[roleKey] = p.ID
			}
		}
	}
	return c, nil
}

func DefaultCatalog() *Catalog {
	c, _ := NewCatalog([]Profile{{
		ID:             "default-chat",
		Provider:       "local",
		DisplayName:    "Default Chat Model",
		Role:           "dialogue",
		Roles:          []Role{RoleRouter, RoleDialogue, RoleRAGRewrite, RoleDiagnosis, RolePlan, RoleExecutionSummary, RoleEvaluator, RoleMemoryExtractor},
		ContextWindow:  128000,
		SupportsTools:  true,
		SupportsStream: true,
		Capabilities:   Capabilities{Streaming: true, Tools: true, JSON: true, Tokenizer: "default"},
		Timeout:        60 * time.Second,
		RetryPolicy:    RetryPolicy{MaxAttempts: 2, Backoff: time.Second},
		CostClass:      "standard",
		Default:        true,
	}, {
		ID:          "default-embedding",
		Provider:    "local",
		DisplayName: "Default Embedding Model",
		Role:        "embedding",
		Roles:       []Role{Role("embedding")},
		Timeout:     30 * time.Second,
		RetryPolicy: RetryPolicy{MaxAttempts: 2, Backoff: time.Second},
		Default:     true,
	}})
	return c
}

func (c *Catalog) ResolveRole(ctx context.Context, role Role) (Profile, error) {
	_ = ctx
	p, ok := c.Resolve(string(role))
	if !ok {
		return Profile{}, fmt.Errorf("no model profile for role %q", role)
	}
	return p, nil
}

func (c *Catalog) RequireCapability(ctx context.Context, role Role, capability string) (Profile, error) {
	p, err := c.ResolveRole(ctx, role)
	if err != nil {
		return Profile{}, err
	}
	switch strings.TrimSpace(strings.ToLower(capability)) {
	case "streaming", "stream":
		if !p.Capabilities.Streaming && !p.SupportsStream {
			return Profile{}, fmt.Errorf("model profile %q for role %q does not support streaming", p.ID, role)
		}
	case "tools", "tool":
		if !p.Capabilities.Tools && !p.SupportsTools {
			return Profile{}, fmt.Errorf("model profile %q for role %q does not support tools", p.ID, role)
		}
	case "json":
		if !p.Capabilities.JSON {
			return Profile{}, fmt.Errorf("model profile %q for role %q does not support json", p.ID, role)
		}
	case "vision":
		if !p.Capabilities.Vision {
			return Profile{}, fmt.Errorf("model profile %q for role %q does not support vision", p.ID, role)
		}
	case "":
		return p, nil
	default:
		return Profile{}, fmt.Errorf("unknown model capability %q", capability)
	}
	return p, nil
}

func (c *Catalog) Resolve(idOrRole string) (Profile, bool) {
	if c == nil {
		return Profile{}, false
	}
	key := strings.TrimSpace(idOrRole)
	if p, ok := c.profiles[key]; ok {
		return p, true
	}
	if id := c.defaults[key]; id != "" {
		p, ok := c.profiles[id]
		return p, ok
	}
	return Profile{}, false
}

func (c *Catalog) List() []Profile {
	if c == nil {
		return nil
	}
	out := make([]Profile, 0, len(c.profiles))
	for _, p := range c.profiles {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
