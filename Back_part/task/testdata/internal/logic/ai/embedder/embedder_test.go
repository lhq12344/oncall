package embedder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDashScopeEmbedderEmbedsTextInOrder(t *testing.T) {
	t.Parallel()

	var gotReq dashScopeEmbeddingRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != dashScopeEmbeddingPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, dashScopeEmbeddingPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q, want Bearer test-key", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"output": {
				"embeddings": [
					{"index": 1, "embedding": [2.1, 2.2], "type": "text"},
					{"index": 0, "embedding": [1.1, 1.2], "type": "text"}
				]
			}
		}`))
	}))
	defer server.Close()

	eb, err := newDashScopeEmbedder("tongyi-embedding-vision-plus", "test-key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("newDashScopeEmbedder() error = %v", err)
	}

	vectors, err := eb.EmbedStrings(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("EmbedStrings() error = %v", err)
	}

	if gotReq.Model != "tongyi-embedding-vision-plus" {
		t.Fatalf("model = %q", gotReq.Model)
	}
	if got := gotReq.Input.Contents[0]["text"]; got != "first" {
		t.Fatalf("first content = %q", got)
	}
	if got := gotReq.Input.Contents[1]["text"]; got != "second" {
		t.Fatalf("second content = %q", got)
	}
	if got := vectors[0][0]; got != 1.1 {
		t.Fatalf("vectors[0][0] = %v", got)
	}
	if got := vectors[1][0]; got != 2.1 {
		t.Fatalf("vectors[1][0] = %v", got)
	}
}

func TestResolveDashScopeEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{
			name:    "empty uses official endpoint",
			baseURL: "",
			want:    defaultDashScopeEmbeddingEndpoint,
		},
		{
			name:    "root appends official path",
			baseURL: "https://dashscope.aliyuncs.com",
			want:    defaultDashScopeEmbeddingEndpoint,
		},
		{
			name:    "full endpoint kept",
			baseURL: defaultDashScopeEmbeddingEndpoint,
			want:    defaultDashScopeEmbeddingEndpoint,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveDashScopeEndpoint(tt.baseURL); got != tt.want {
				t.Fatalf("resolveDashScopeEndpoint(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}
