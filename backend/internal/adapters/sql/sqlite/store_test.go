package sqlite

import (
	"context"
	"sync"
	"testing"

	"go_agent/internal/improvement"
)

func TestSQLiteStoreImplementsCaseStore(t *testing.T) {
	store := NewStore(":memory:")
	if err := store.SaveCase(context.Background(), improvement.Case{ID: "c1"}); err != nil {
		t.Fatal(err)
	}
	cases, err := store.ListCases(context.Background())
	if err != nil || len(cases) != 1 {
		t.Fatalf("cases=%+v err=%v", cases, err)
	}
}

func TestSQLiteStoreSerializesConcurrentReopenedWriters(t *testing.T) {
	path := t.TempDir() + "/review-cases.json"
	left := NewStore(path)
	right := NewStore(path)
	const writes = 8
	var wg sync.WaitGroup
	for i := 0; i < writes; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if err := left.SaveCase(context.Background(), improvement.Case{ID: "left-" + string(rune('a'+index))}); err != nil {
				t.Errorf("left SaveCase: %v", err)
			}
		}(i)
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if err := right.SaveCase(context.Background(), improvement.Case{ID: "right-" + string(rune('a'+index))}); err != nil {
				t.Errorf("right SaveCase: %v", err)
			}
		}(i)
	}
	wg.Wait()
	cases, err := NewStore(path).ListCases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != writes*2 {
		t.Fatalf("persisted case count=%d, want %d", len(cases), writes*2)
	}
}

func TestSQLiteStorePersistsCasesAcrossStoreReopen(t *testing.T) {
	path := t.TempDir() + "/review-cases.json"
	first := NewStore(path)
	if err := first.SaveCase(context.Background(), improvement.Case{ID: "persisted"}); err != nil {
		t.Fatal(err)
	}

	second := NewStore(path)
	cases, err := second.ListCases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].ID != "persisted" {
		t.Fatalf("reopened cases=%+v", cases)
	}
}
