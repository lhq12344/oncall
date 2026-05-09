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
			name:         "prefer env value",
			configValue:  "milvus.infra.svc:19530",
			envValue:     "127.0.0.1:19530",
			defaultValue: DefaultMilvusAddress,
			expected:     "127.0.0.1:19530",
		},
		{
			name:         "fallback to config value",
			configValue:  "milvus.infra.svc:19530",
			envValue:     "",
			defaultValue: DefaultMilvusAddress,
			expected:     "milvus.infra.svc:19530",
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
			expected:     "biz",
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
			name:         "prefer env value",
			configValue:  "12s",
			envValue:     "20s",
			defaultValue: DefaultMilvusTimeout,
			expected:     20 * time.Second,
		},
		{
			name:         "fallback to config value",
			configValue:  "15s",
			envValue:     "",
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

func TestResolvePositiveInt(t *testing.T) {
	tests := []struct {
		name         string
		candidates   []string
		defaultValue int
		expected     int
	}{
		{
			name:         "first positive integer",
			candidates:   []string{"", "2048", "1024"},
			defaultValue: DefaultEmbeddingDimension,
			expected:     2048,
		},
		{
			name:         "trim whitespace",
			candidates:   []string{" 1024 "},
			defaultValue: DefaultEmbeddingDimension,
			expected:     1024,
		},
		{
			name:         "invalid falls back",
			candidates:   []string{"bad", "0", "-1"},
			defaultValue: DefaultEmbeddingDimension,
			expected:     DefaultEmbeddingDimension,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := resolvePositiveInt(tc.candidates, tc.defaultValue)
			if actual != tc.expected {
				t.Fatalf("expected %d, got %d", tc.expected, actual)
			}
		})
	}
}
