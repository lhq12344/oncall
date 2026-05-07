package models

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type captureRoundTripper struct {
	header http.Header
}

func (c *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.header = req.Header.Clone()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
	}, nil
}

func TestRetryRoundTripperUsesConfiguredAPIKeyHeader(t *testing.T) {
	base := &captureRoundTripper{}
	transport := &retryRoundTripper{
		base:         base,
		apiKeyHeader: "x-litellm-api-key",
		apiKey:       "test-key",
	}
	req, err := http.NewRequest(http.MethodPost, "https://example.test/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-key")

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := base.header.Get("x-litellm-api-key"); got != "test-key" {
		t.Fatalf("x-litellm-api-key = %q, want test-key", got)
	}
	if got := base.header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization header = %q, want empty", got)
	}
}
