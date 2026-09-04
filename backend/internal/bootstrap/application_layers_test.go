package bootstrap

import (
	"context"
	"errors"
	"testing"

	"go_agent/internal/knowledge"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
)

func TestBuildOptionalKnowledgeAgentDegradesOnInitializationFailure(t *testing.T) {
	t.Parallel()

	agent := buildOptionalKnowledgeAgentWith(
		context.Background(),
		zap.NewNop(),
		func(context.Context, *knowledge.Config) (adk.Agent, error) {
			return nil, errors.New("embedding credentials unavailable")
		},
	)
	if agent != nil {
		t.Fatal("knowledge agent should be disabled when its optional dependencies are unavailable")
	}
}
