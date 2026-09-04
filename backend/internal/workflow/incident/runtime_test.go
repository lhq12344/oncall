package incident

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go_agent/internal/events"
	"go_agent/internal/telemetry"
	"go_agent/internal/tools/policy"
)

func TestRuntimeReplaysFixtureToExpectedTerminal(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 20 {
		t.Fatalf("expected at least 20 fixtures, got %d", len(fixtures))
	}
	for _, fixture := range fixtures {
		state, err := Runtime{}.Run(context.Background(), fixture)
		if err != nil {
			t.Fatalf("Run(%s): %v", fixture.ID, err)
		}
		if state.Terminal != fixture.ExpectedTerminal {
			t.Fatalf("Run(%s) terminal=%s want %s", fixture.ID, state.Terminal, fixture.ExpectedTerminal)
		}
	}
}

func TestRuntimeEmitsEventsAndSpansForNodes(t *testing.T) {
	eventSink := &events.MemorySink{}
	emitter, _ := events.NewEmitter("run", "trace", eventSink)
	telemetrySink := telemetry.NewMemorySink()
	state, err := Runtime{Events: emitter, Telemetry: telemetry.NewRecorder(telemetrySink)}.Run(context.Background(), Fixture{ID: "ok", ExpectedTerminal: TerminalComplete, Evidence: []string{"pod ready"}, DiagnosisConfidence: 0.9})
	if err != nil || state.Terminal != TerminalComplete {
		t.Fatalf("Run state=%+v err=%v", state, err)
	}
	if len(eventSink.Events()) == 0 || len(telemetrySink.Spans()) == 0 {
		t.Fatalf("expected events and spans, events=%d spans=%d", len(eventSink.Events()), len(telemetrySink.Spans()))
	}
}

func TestReceiptsPreventDuplicateExecutionOnResume(t *testing.T) {
	state, err := Runtime{}.Run(context.Background(), Fixture{ID: "resume", ExpectedTerminal: TerminalComplete, Evidence: []string{"ok"}, DiagnosisConfidence: 0.9})
	if err != nil {
		t.Fatal(err)
	}
	count := len(state.ExecutionReceipts)
	state.ExecutionReceipts["read-resume"] = Receipt{Key: "read-resume", Status: "success"}
	if len(state.ExecutionReceipts) != count {
		t.Fatalf("receipt should remain idempotent")
	}
}

func TestApprovalSnapshotInvalidatesWhenPlanRevisionChanges(t *testing.T) {
	req := policy.Request{ToolID: "execute_step", ToolVersion: "v1", Args: map[string]any{"plan": "p1", "revision": 1, "hash": "h1"}}
	snapshot, err := policy.BindApproval(req)
	if err != nil {
		t.Fatal(err)
	}
	changed := policy.Request{ToolID: "execute_step", ToolVersion: "v1", Args: map[string]any{"plan": "p1", "revision": 2, "hash": "h1"}, Approved: &snapshot, Risk: policy.RiskHigh, Capability: "execution.mutation"}
	decision := policy.NewEngine("").Decide(context.Background(), changed)
	if decision.Effect != policy.Ask {
		t.Fatalf("changed revision should invalidate approval: %+v", decision)
	}
}

func loadFixtures(t *testing.T) []Fixture {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "replay", "incident", "phase10_incident_replay.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()
	var fixtures []Fixture
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var fixture Fixture
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		fixtures = append(fixtures, fixture)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return fixtures
}
