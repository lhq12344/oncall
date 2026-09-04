package config

import (
	"fmt"
	"strings"
	"time"
)

type ModelRole string

const (
	RoleRouter           ModelRole = "router"
	RoleDialogue         ModelRole = "dialogue"
	RoleRAGRewrite       ModelRole = "rag_rewrite"
	RoleDiagnosis        ModelRole = "diagnosis"
	RolePlan             ModelRole = "plan"
	RoleExecutionSummary ModelRole = "execution_summary"
	RoleEvaluator        ModelRole = "evaluator"
	RoleMemoryExtractor  ModelRole = "memory_extractor"
)

type ModelCapabilities struct {
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

type ModelProfile struct {
	ID              string
	Provider        string
	Model           string
	Roles           []ModelRole
	ContextWindow   int
	MaxOutputTokens int
	Capabilities    ModelCapabilities
	Timeout         time.Duration
	Retry           RetryPolicy
	CostClass       string
	Default         bool
}

func DefaultChatModel() ModelProfile {
	return ModelProfile{
		ID:              "default-chat",
		Provider:        "local",
		Model:           "default-chat",
		Roles:           []ModelRole{RoleRouter, RoleDialogue, RoleRAGRewrite, RoleDiagnosis, RolePlan, RoleExecutionSummary, RoleEvaluator, RoleMemoryExtractor},
		ContextWindow:   128000,
		MaxOutputTokens: 4096,
		Capabilities:    ModelCapabilities{Streaming: true, Tools: true, JSON: true, Tokenizer: "default"},
		Timeout:         60 * time.Second,
		Retry:           RetryPolicy{MaxAttempts: 2, Backoff: time.Second},
		CostClass:       "standard",
		Default:         true,
	}
}

func DefaultEmbeddingModel() ModelProfile {
	return ModelProfile{
		ID:              "default-embedding",
		Provider:        "local",
		Model:           "default-embedding",
		Roles:           []ModelRole{},
		ContextWindow:   8192,
		MaxOutputTokens: 0,
		Capabilities:    ModelCapabilities{Tokenizer: "default"},
		Timeout:         30 * time.Second,
		Retry:           RetryPolicy{MaxAttempts: 2, Backoff: time.Second},
		CostClass:       "standard",
		Default:         true,
	}
}

func ValidateModels(profiles []ModelProfile) error {
	if len(profiles) == 0 {
		return fmt.Errorf("at least one model profile is required")
	}
	seen := map[string]struct{}{}
	roleDefaults := map[ModelRole]string{}
	for _, profile := range profiles {
		id := strings.TrimSpace(profile.ID)
		if id == "" {
			return fmt.Errorf("model profile id is required")
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate model profile %q", id)
		}
		seen[id] = struct{}{}
		if strings.TrimSpace(profile.Provider) == "" {
			return fmt.Errorf("model profile %q provider is required", id)
		}
		if strings.TrimSpace(profile.Model) == "" {
			return fmt.Errorf("model profile %q model is required", id)
		}
		if profile.ContextWindow <= 0 {
			return fmt.Errorf("model profile %q context window must be positive", id)
		}
		if profile.Timeout <= 0 {
			return fmt.Errorf("model profile %q timeout must be positive", id)
		}
		if profile.Retry.MaxAttempts < 0 {
			return fmt.Errorf("model profile %q retry attempts must be non-negative", id)
		}
		if profile.Default {
			for _, role := range profile.Roles {
				if existing := roleDefaults[role]; existing != "" {
					return fmt.Errorf("multiple default model profiles for role %q: %s and %s", role, existing, id)
				}
				roleDefaults[role] = id
			}
		}
	}
	return nil
}
