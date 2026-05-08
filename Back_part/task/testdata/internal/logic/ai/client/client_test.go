package client

import (
	"errors"
	"testing"
)

func TestIsRetryableMilvusStartupError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "proxy not ready",
			err:       errors.New("service unavailable: internal: Milvus Proxy is not ready yet. please wait"),
			retryable: true,
		},
		{
			name:      "connection refused",
			err:       errors.New("failed to connect: connection refused"),
			retryable: true,
		},
		{
			name:      "deadline exceeded",
			err:       errors.New("context deadline exceeded"),
			retryable: true,
		},
		{
			name:      "schema error",
			err:       errors.New("failed to create milvus collection oncall_knowledge: invalid field"),
			retryable: false,
		},
		{
			name:      "nil",
			err:       nil,
			retryable: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := isRetryableMilvusStartupError(tc.err)
			if actual != tc.retryable {
				t.Fatalf("expected %v, got %v", tc.retryable, actual)
			}
		})
	}
}
