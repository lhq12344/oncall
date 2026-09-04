package improvement

import (
	"context"
	"testing"
)

type memoryStore struct{ items []ReviewCase }

func (s *memoryStore) Save(_ context.Context, item ReviewCase) error {
	s.items = append(s.items, item)
	return nil
}

func TestCaptureDoesNotBlockWhenStoreMissingAndDefaultsStatus(t *testing.T) {
	if err := Capture(context.Background(), nil, ReviewCase{Reason: ReasonWeakEvidence}); err != nil {
		t.Fatalf("nil store should not block: %v", err)
	}
	store := &memoryStore{}
	if err := Capture(context.Background(), store, ReviewCase{ID: "case1", Reason: ReasonRetrievalDegraded}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if len(store.items) != 1 || store.items[0].Status != "new" {
		t.Fatalf("unexpected items: %+v", store.items)
	}
}

func TestCaptureSupportsAllPhaseElevenEntryPoints(t *testing.T) {
	store := &memoryStore{}
	reasons := []CaptureReason{
		ReasonWeakEvidence,
		ReasonEvidenceGateFail,
		ReasonUserDownvote,
		ReasonWorkflowFailure,
		ReasonToolDegraded,
		ReasonHighValueSuccess,
	}

	for _, reason := range reasons {
		if err := Capture(context.Background(), store, ReviewCase{ID: string(reason), Reason: reason}); err != nil {
			t.Fatalf("Capture(%s): %v", reason, err)
		}
	}

	if len(store.items) != len(reasons) {
		t.Fatalf("captured %d review cases, want %d", len(store.items), len(reasons))
	}
	seen := map[CaptureReason]bool{}
	for _, item := range store.items {
		seen[item.Reason] = true
		if item.Status != "new" {
			t.Fatalf("case %s status=%q, want new", item.ID, item.Status)
		}
	}
	for _, reason := range reasons {
		if !seen[reason] {
			t.Fatalf("missing captured reason %s", reason)
		}
	}
}
