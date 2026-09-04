package retrieval

import (
	"context"
	"testing"

	"go_agent/internal/rag"
)

func TestQueryPlannerPreservesOriginalAndAvoidsLowConfidenceFilters(t *testing.T) {
	plan, err := NewQueryPlanner(fakeRewriter{result: rag.RewriteResult{RewrittenQueries: []string{"redis timeout"}, Entities: map[string]any{"namespace": "prod"}, Confidence: 0.4}}).Plan(context.Background(), rag.RewriteInput{Query: "redis 超时"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.RewrittenQueries[0] != "redis 超时" || len(plan.Filters) != 0 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

type fakeRewriter struct{ result rag.RewriteResult }

func (f fakeRewriter) Rewrite(context.Context, rag.RewriteInput) (rag.RewriteResult, error) {
	return f.result, nil
}
