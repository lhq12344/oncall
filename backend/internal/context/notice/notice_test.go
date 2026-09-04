package notice

import (
	"strings"
	"testing"
	"time"
)

func TestRenderFiltersExpiredDeduplicatesAndMarksTrust(t *testing.T) {
	now := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	got := Render([]Notice{
		{Kind: KindSkill, Trust: TrustUntrustedEvidence, Source: "skill", Lifecycle: LifecycleRun, Content: "older", Priority: 20, DedupKey: "skill"},
		{Kind: KindSkill, Trust: TrustUntrustedEvidence, Source: "skill", Lifecycle: LifecycleRun, Content: "newer", Priority: 10, DedupKey: "skill"},
		{Kind: KindCompaction, Trust: TrustTrustedRuntime, Source: "compact", Lifecycle: LifecycleRun, Content: "expired", Priority: 1, ExpiresAt: now.Add(-time.Second)},
	}, now)
	if strings.Contains(got, "older") || strings.Contains(got, "expired") || !strings.Contains(got, "newer") || !strings.Contains(got, "trust=untrusted_evidence") {
		t.Fatalf("unexpected render: %q", got)
	}
}
