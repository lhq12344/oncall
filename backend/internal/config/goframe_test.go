package config

import "testing"

func TestCozeLoopExporterFromEnvRequiresCompleteConfiguration(t *testing.T) {
	t.Setenv("COZELOOP_API_BASE_URL", "http://127.0.0.1:18082")
	t.Setenv("COZELOOP_WORKSPACE_ID", "workspace")
	t.Setenv("COZELOOP_API_TOKEN", "token")
	if got := cozeloopExporterFromEnv(); got != "cozeloop" {
		t.Fatalf("exporter=%q, want cozeloop", got)
	}
}

func TestCozeLoopExporterFromEnvDoesNotEnablePartialConfiguration(t *testing.T) {
	t.Setenv("COZELOOP_API_BASE_URL", "http://127.0.0.1:18082")
	t.Setenv("COZELOOP_WORKSPACE_ID", "")
	t.Setenv("COZELOOP_API_TOKEN", "")
	if got := cozeloopExporterFromEnv(); got != "" {
		t.Fatalf("exporter=%q, want empty", got)
	}
}
