package common

import (
	"testing"
	"time"
)

func TestResolveMilvusSetting(t *testing.T) {
	tests := []struct {
		name         string
		configValue  string
		envValue     string
		defaultValue string
		expected     string
	}{
		{
			name:         "prefer config value",
			configValue:  "milvus.infra.svc:19530",
			envValue:     "127.0.0.1:19530",
			defaultValue: DefaultMilvusAddress,
			expected:     "milvus.infra.svc:19530",
		},
		{
			name:         "fallback to env value",
			configValue:  "",
			envValue:     "127.0.0.1:19530",
			defaultValue: DefaultMilvusAddress,
			expected:     "127.0.0.1:19530",
		},
		{
			name:         "fallback to default value",
			configValue:  "",
			envValue:     "",
			defaultValue: DefaultMilvusAddress,
			expected:     DefaultMilvusAddress,
		},
		{
			name:         "trim whitespace",
			configValue:  "  oncall_knowledge  ",
			envValue:     "  biz  ",
			defaultValue: MilvusCollectionName,
			expected:     "oncall_knowledge",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := resolveMilvusSetting(tc.configValue, tc.envValue, tc.defaultValue)
			if actual != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, actual)
			}
		})
	}
}

func TestResolveMilvusDuration(t *testing.T) {
	tests := []struct {
		name         string
		configValue  string
		envValue     string
		defaultValue time.Duration
		expected     time.Duration
	}{
		{
			name:         "prefer config value",
			configValue:  "12s",
			envValue:     "20s",
			defaultValue: DefaultMilvusTimeout,
			expected:     12 * time.Second,
		},
		{
			name:         "fallback to env value",
			configValue:  "",
			envValue:     "15s",
			defaultValue: DefaultMilvusTimeout,
			expected:     15 * time.Second,
		},
		{
			name:         "invalid duration falls back to default",
			configValue:  "bad",
			envValue:     "",
			defaultValue: DefaultMilvusTimeout,
			expected:     DefaultMilvusTimeout,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := resolveMilvusDuration(tc.configValue, tc.envValue, tc.defaultValue)
			if actual != tc.expected {
				t.Fatalf("expected %s, got %s", tc.expected, actual)
			}
		})
	}
}

func TestMilvusV2DefaultsAndEmptyOverride(t *testing.T) {
	if got := resolveMilvusSetting("", "", MilvusKnowledgeV2Collection); got != "biz_v2" {
		t.Fatalf("knowledge v2 default=%q", got)
	}
	if got := resolveMilvusSetting("  ", "  ", MilvusOpsV2Collection); got != "ops_cases_v2" {
		t.Fatalf("ops v2 default=%q", got)
	}
	if got := resolveMilvusSetting("", "custom_v2", MilvusKnowledgeV2Collection); got != "custom_v2" {
		t.Fatalf("env override=%q", got)
	}
	if got := resolveMilvusSetting("cfg_v2", "custom_v2", MilvusKnowledgeV2Collection); got != "cfg_v2" {
		t.Fatalf("config override=%q", got)
	}
}

func TestResolveMilvusBool(t *testing.T) {
	if !resolveMilvusBool("", "", true) {
		t.Fatal("default true was not preserved")
	}
	if resolveMilvusBool("false", "true", true) {
		t.Fatal("config false should override env true")
	}
	if !resolveMilvusBool("", "on", false) {
		t.Fatal("env on should resolve true")
	}
	if !resolveMilvusBool("maybe", "", true) {
		t.Fatal("invalid value should preserve default")
	}
}
