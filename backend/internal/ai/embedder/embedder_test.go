package embedder

import "testing"

func TestFirstEnvOrValueUsesEnvironmentOverride(t *testing.T) {
	t.Setenv("ONCALL_EMBEDDING_API_KEY", "env-key")
	if got := firstEnvOrValue("config-key", "ONCALL_EMBEDDING_API_KEY"); got != "env-key" {
		t.Fatalf("key=%q, want env-key", got)
	}
}

func TestFirstEnvOrValueFallsBackToConfig(t *testing.T) {
	t.Setenv("ONCALL_EMBEDDING_API_KEY", "")
	if got := firstEnvOrValue(" config-key ", "ONCALL_EMBEDDING_API_KEY"); got != "config-key" {
		t.Fatalf("key=%q, want config-key", got)
	}
}
