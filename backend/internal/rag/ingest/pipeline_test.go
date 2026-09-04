package ingest

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPipelineProducesStableChunkIDsAndManifest(t *testing.T) {
	now := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	req := Request{Source: Source{URI: "runbook.md", SourceType: "runbook", Content: "# Fix\nrestart safely"}, Profile: ProfileRunbook, IndexVersion: "idx1", Now: now}
	first, err := NewPipeline(nil, nil).Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run first: %v", err)
	}
	second, err := NewPipeline(nil, nil).Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run second: %v", err)
	}
	if first.Chunks[0].ChunkID != second.Chunks[0].ChunkID || first.Chunks[0].ContentHash != second.Chunks[0].ContentHash {
		t.Fatalf("chunk ids not stable: %+v %+v", first.Chunks[0], second.Chunks[0])
	}
	if first.Chunks[0].ContextualPrefix == "" || first.Chunks[0].ChunkProfile != string(ProfileRunbook) || first.Chunks[0].IndexVersion != "idx1" {
		t.Fatalf("missing v2 chunk metadata: %+v", first.Chunks[0])
	}
	firstHash, _ := first.Hash()
	secondHash, _ := second.Hash()
	if firstHash != secondHash {
		t.Fatalf("manifest hash not stable: %s %s", firstHash, secondHash)
	}
}

func TestChunkProfilesCoverRunbookIncidentK8sAndLogs(t *testing.T) {
	for _, profile := range []ChunkProfile{ProfileRunbook, ProfileIncident, ProfileK8s, ProfileLog} {
		cfg := ConfigFor(profile)
		if cfg.Profile != profile || cfg.ChildTokens <= 0 || cfg.Strategy == "" {
			t.Fatalf("bad config for %s: %+v", profile, cfg)
		}
	}
}

func TestLogProfileUsesAggregationStrategy(t *testing.T) {
	cfg := ConfigFor(ProfileLog)
	if cfg.Strategy != "aggregate_by_trace_pod_container_window_signature" {
		t.Fatalf("log profile must not directly semantic chunk raw logs: %+v", cfg)
	}
}

func TestProfileFixturesHaveAtLeastTenExamplesEach(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "rag", "ingest_profiles.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()
	counts := map[string]int{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var row struct {
			Version string `json:"version"`
			Profile string `json:"profile"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		if row.Version == "" {
			t.Fatal("fixture missing version")
		}
		counts[row.Profile]++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	for _, profile := range []ChunkProfile{ProfileRunbook, ProfileIncident, ProfileK8s, ProfileLog} {
		if counts[string(profile)] < 10 {
			t.Fatalf("profile %s has %d fixtures, want at least 10", profile, counts[string(profile)])
		}
	}
}
