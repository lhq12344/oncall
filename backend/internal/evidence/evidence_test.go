package evidence

import (
	"context"
	"testing"
	"time"
)

type fakeSource struct{ item Evidence }

func (f fakeSource) Collect(context.Context, Query) Evidence { return f.item }

func TestIngressDeduplicatesFingerprintWithinWindow(t *testing.T) {
	now := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	ingress := NewIngress(time.Minute)
	signal := IncidentSignal{Source: "alert", Cluster: "c", Namespace: "n", Service: "svc", Labels: map[string]string{"a": "b"}}
	first, ok := ingress.Accept(signal, now)
	if !ok || first.Fingerprint == "" {
		t.Fatalf("expected first accepted: %+v %v", first, ok)
	}
	_, ok = ingress.Accept(signal, now.Add(30*time.Second))
	if ok {
		t.Fatal("duplicate should be rejected inside window")
	}
	_, ok = ingress.Accept(signal, now.Add(2*time.Minute))
	if !ok {
		t.Fatal("duplicate should be accepted after window")
	}
}

func TestCollectorReturnsTypedEvidenceWithArtifactRef(t *testing.T) {
	now := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	collector := Collector{Now: func() time.Time { return now }, Sources: []Source{fakeSource{item: Evidence{Source: "kubernetes", Scope: Scope{Namespace: "infra"}, Summary: "ok"}}}}
	items := collector.Collect(context.Background(), Query{Scope: Scope{Namespace: "infra"}})
	if len(items) != 1 || items[0].Timestamp.IsZero() || items[0].ArtifactRef.ID == "" || items[0].Source != "kubernetes" {
		t.Fatalf("unexpected evidence: %+v", items)
	}
}

func TestCollectorNoSourcesReturnsPermissionEvidence(t *testing.T) {
	items := (Collector{}).Collect(context.Background(), Query{Scope: Scope{Namespace: "infra"}})
	if len(items) != 1 || !items[0].Degraded || items[0].ArtifactRef.Kind != "permission" {
		t.Fatalf("expected permission evidence: %+v", items)
	}
}
