package orchestration

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPhaseThirteenIntentGoldMacroF1AndHighRiskRecall(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "intent", "phase13_intent_gold.jsonl")
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open intent gold: %v", err)
	}
	defer file.Close()

	type row struct {
		Version string `json:"version"`
		Text    string `json:"text"`
		Intent  string `json:"intent"`
	}
	labels := map[string]struct{}{}
	tp := map[string]int{}
	fp := map[string]int{}
	fn := map[string]int{}
	changeTotal := 0
	changeHit := 0
	total := 0
	scanner := bufio.NewScanner(file)
	router := NewRouter(0.85)
	for scanner.Scan() {
		var item row
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			t.Fatalf("decode intent gold: %v", err)
		}
		if item.Version == "" {
			t.Fatal("intent gold row missing version")
		}
		want := normalizeGoldIntent(item.Intent)
		decision, err := router.Route(context.Background(), RouteInput{Text: item.Text})
		if err != nil {
			t.Fatalf("route %q: %v", item.Text, err)
		}
		got := normalizeRouteIntent(decision.Intent)
		labels[want] = struct{}{}
		labels[got] = struct{}{}
		if want == got {
			tp[want]++
		} else {
			fp[got]++
			fn[want]++
		}
		if want == "change_request" {
			changeTotal++
			if got == want {
				changeHit++
			}
		}
		total++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan intent gold: %v", err)
	}
	if total < 200 {
		t.Fatalf("intent gold count=%d, want >=200", total)
	}

	macro := 0.0
	for label := range labels {
		precision := ratio(tp[label], tp[label]+fp[label])
		recall := ratio(tp[label], tp[label]+fn[label])
		if precision+recall > 0 {
			macro += 2 * precision * recall / (precision + recall)
		}
	}
	macro /= float64(len(labels))
	if macro < 0.90 {
		t.Fatalf("macro-F1=%.3f, want >=0.90", macro)
	}
	if ratio(changeHit, changeTotal) < 0.98 {
		t.Fatalf("change_request recall=%.3f, want >=0.98", ratio(changeHit, changeTotal))
	}
}

func normalizeGoldIntent(intent string) string {
	switch intent {
	case "knowledge_rag":
		return "knowledge_question"
	case "incident_workflow":
		return "incident_diagnosis"
	case "change_workflow":
		return "change_request"
	case "clarify":
		return "unclear"
	case "refuse":
		return "out_of_scope"
	case "workflow_control":
		return "workflow_control"
	default:
		return intent
	}
}

func normalizeRouteIntent(intent Intent) string { return string(intent) }

func ratio(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}
