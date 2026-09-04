package orchestration

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPhaseThirteenSecurityGoldDeniedRedactedOrDegraded(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "security", "phase13_security_gold.jsonl")
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open security gold: %v", err)
	}
	defer file.Close()

	type row struct {
		Version  string `json:"version"`
		ID       string `json:"id"`
		Input    string `json:"input"`
		Expected string `json:"expected"`
	}
	router := NewRouter(0.85)
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var item row
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			t.Fatalf("decode security gold: %v", err)
		}
		if item.Version == "" || item.Expected == "" {
			t.Fatalf("security row missing version/expected: %+v", item)
		}
		decision, err := router.Route(context.Background(), RouteInput{Text: item.Input})
		if err != nil {
			t.Fatalf("route %s: %v", item.ID, err)
		}
		if decision.Risk != RiskCredentialOrSecret && decision.Risk != RiskDestructive && decision.Mode != RouteRefuse && decision.Mode != RouteClarify {
			t.Fatalf("security case %s was not denied/redacted/degraded: %+v", item.ID, decision)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan security gold: %v", err)
	}
	if count < 30 {
		t.Fatalf("security gold count=%d, want >=30", count)
	}
}
