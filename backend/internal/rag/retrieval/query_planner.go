package retrieval

import (
	"context"

	"go_agent/internal/rag"
)

type Plan struct {
	OriginalQuery       string
	RewrittenQueries    []string
	Entities            map[string]any
	TemporalConstraints map[string]string
	Filters             map[string]any
	Confidence          float64
	NeedsClarification  bool
}

type QueryPlanner struct {
	Rewriter rag.QueryRewriter
}

func NewQueryPlanner(rewriter rag.QueryRewriter) QueryPlanner {
	if rewriter == nil {
		rewriter = rag.NoopRewriter{}
	}
	return QueryPlanner{Rewriter: rewriter}
}

func (p QueryPlanner) Plan(ctx context.Context, input rag.RewriteInput) (Plan, error) {
	result, err := p.Rewriter.Rewrite(ctx, input)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{OriginalQuery: input.Query, RewrittenQueries: rag.NormalizeQueryVariants(input.Query, result.RewrittenQueries, 3), Entities: result.Entities, Confidence: result.Confidence, NeedsClarification: result.NeedsClarification}
	if plan.Confidence >= 0.85 {
		plan.Filters = entityFilters(result.Entities)
	}
	return plan, nil
}

func entityFilters(entities map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"cluster", "service", "namespace"} {
		if value, ok := entities[key]; ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
