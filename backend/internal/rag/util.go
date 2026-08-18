package rag

import "strings"

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" && trimmed != "<nil>" {
			return trimmed
		}
	}
	return ""
}

func cloneMeta(meta map[string]any) map[string]any {
	out := make(map[string]any, len(meta)+4)
	for key, value := range meta {
		out[key] = value
	}
	return out
}

func mergeMeta(a, b map[string]any) map[string]any {
	out := cloneMeta(a)
	for key, value := range b {
		if _, exists := out[key]; !exists {
			out[key] = value
		}
	}
	return out
}
