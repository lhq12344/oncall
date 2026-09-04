package memory

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPolicyAllowsOnlyStableSourcedMemory(t *testing.T) {
	policy := Policy{}
	if !policy.AllowCandidate(Candidate{Kind: KindUserPreference, Content: "用户偏好中文回复", Confidence: 0.9, Provenance: "run-1"}) {
		t.Fatal("expected stable sourced preference allowed")
	}
	if policy.AllowCandidate(Candidate{Kind: KindUserPreference, Content: "password=abc", Confidence: 0.99, Provenance: "run-1"}) {
		t.Fatal("secret candidate must be denied")
	}
	if policy.AllowCandidate(Candidate{Kind: KindUserPreference, Content: "未验证猜测", Confidence: 0.99, Provenance: "run-1"}) {
		t.Fatal("unverified guess must be denied")
	}
}

func TestStoreSearchAndForget(t *testing.T) {
	store := NewMemoryStore()
	record := RecordFromCandidate(Candidate{Kind: KindConfirmedEnvironment, Scope: "project", Owner: "ops", Content: "prod namespace is infra", Confidence: 0.9, Provenance: "run-1"}, time.Now().UTC())
	if err := store.Upsert(context.Background(), []Record{record}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	found, err := store.Search(context.Background(), Query{Scope: "project", Text: "infra"})
	if err != nil || len(found) != 1 || found[0].Provenance == "" {
		t.Fatalf("Search found=%+v err=%v", found, err)
	}
	if err := store.Delete(context.Background(), record.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	found, _ = store.Search(context.Background(), Query{Scope: "project", Text: "infra"})
	if len(found) != 0 {
		t.Fatalf("forgotten memory should not be recalled: %+v", found)
	}
}

func TestNoticeIncludesProvenance(t *testing.T) {
	n := Notice([]Record{{Source: string(KindVerifiedOpsConclusion), Content: "rollback fixed issue", Confidence: 0.92, Provenance: "run-2"}})
	if !strings.Contains(n.Content, "run-2") || !strings.Contains(n.Content, "confidence") {
		t.Fatalf("notice missing provenance: %+v", n)
	}
}
