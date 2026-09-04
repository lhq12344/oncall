package tools

import "testing"

func TestOptimizeWithRulesReturnsStrategyCopyWithHints(t *testing.T) {
	t.Parallel()

	tool := &OptimizeStrategyTool{}
	strategy := map[string]interface{}{
		"name": "restart-check",
		"steps": []interface{}{
			map[string]interface{}{"id": "inspect", "timeout_ms": float64(45000)},
		},
	}
	evaluation := map[string]interface{}{
		"success_rate": float64(0.7),
		"avg_duration": float64(45000),
	}

	result := tool.optimizeWithRules(strategy, evaluation)
	optimized, ok := result.OptimizedStrategy.(map[string]interface{})
	if !ok {
		t.Fatalf("optimized strategy type = %T, want map", result.OptimizedStrategy)
	}
	if optimized["name"] != strategy["name"] {
		t.Fatalf("optimized strategy lost original fields: %#v", optimized)
	}
	if _, same := result.OriginalStrategy.(map[string]interface{})["optimization_hints"]; same {
		t.Fatalf("original strategy was mutated: %#v", result.OriginalStrategy)
	}
	hints, ok := optimized["optimization_hints"].(map[string]interface{})
	if !ok {
		t.Fatalf("optimization hints missing: %#v", optimized)
	}
	if hints["source"] != "rules" {
		t.Fatalf("hint source = %#v, want rules", hints["source"])
	}
	if len(result.Changes) < 2 || result.ExpectedImprovement["success_rate"] <= 0 || result.ExpectedImprovement["avg_duration"] >= 0 {
		t.Fatalf("expected rule changes for low success and long duration: %#v", result)
	}
}

func TestPruneKnowledgeSuggestsMergingSimilarRetainedCases(t *testing.T) {
	t.Parallel()

	tool := &PruneKnowledgeTool{}
	cases := []pruneCase{
		{CaseID: "case-a", Title: "Redis Timeout", Weight: 0.9, UsageCount: 3, SuccessRate: 0.9},
		{CaseID: "case-b", Title: "redis-timeout", Weight: 0.8, UsageCount: 1, SuccessRate: 0.8},
		{CaseID: "case-c", Title: "K8s Image Pull", Weight: 0.1, UsageCount: 0, SuccessRate: 0.9},
	}

	result := tool.pruneKnowledge(cases, 90, 0.3)
	if len(result.DeletedCases) != 1 || result.DeletedCases[0] != "case-c" {
		t.Fatalf("deleted cases = %v, want case-c", result.DeletedCases)
	}
	if len(result.MergedCases) != 1 || result.MergedCases[0] != "case-b" {
		t.Fatalf("merged cases = %v, want case-b", result.MergedCases)
	}
	if result.RetainedCases != 1 {
		t.Fatalf("retained cases = %d, want 1", result.RetainedCases)
	}
	if result.Reason["case-b"] == "" || result.Reason["case-c"] == "" {
		t.Fatalf("expected merge and delete reasons: %#v", result.Reason)
	}
}
