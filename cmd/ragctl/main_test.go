package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_agent/internal/rag"
)

func TestNormalizeChunksAddsV2Fields(t *testing.T) {
	got := normalizeChunks([]rag.DocumentChunk{{ID: "a", Content: " redis timeout "}}, "knowledge")
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
	if got[0].SourceType != "knowledge" || got[0].ChunkID != "a" || got[0].ContentHash == "" {
		t.Fatalf("chunk not normalized: %#v", got[0])
	}
}

func TestEvalCasesRunsOfflineBM25(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RAG_BM25_ROOT", root)
	idx, err := rag.NewProfileBM25Index(root, rag.ProfileKnowledge)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(nil, []rag.DocumentChunk{{ID: "doc1", ChunkID: "doc1", Content: "redis timeout runbook"}}); err != nil {
		t.Fatal(err)
	}
	gold := filepath.Join(t.TempDir(), "gold.jsonl")
	if err := os.WriteFile(gold, []byte("{\"id\":\"case1\",\"profile\":\"knowledge\",\"query\":\"redis timeout\",\"expected_ids\":[\"doc1\"]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() error {
		return evalCases(nil, []string{"-dataset", gold, "-profile", "knowledge"})
	})
	var got struct {
		CaseTelemetry map[string]struct {
			Status          string         `json:"status"`
			CandidateCounts map[string]int `json:"candidate_counts"`
			Count           int            `json:"count"`
		} `json:"case_telemetry"`
	}
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatal(err)
	}
	case1 := got.CaseTelemetry["case1"]
	if case1.Status != "success" || case1.Count == 0 {
		t.Fatalf("case telemetry not populated: %#v", got.CaseTelemetry)
	}
	if case1.CandidateCounts[rag.CandidateCountSourceBM25Docs] == 0 || case1.CandidateCounts[rag.CandidateCountStageFinalDocs] == 0 {
		t.Fatalf("candidate telemetry missing namespaced counts: %#v", case1.CandidateCounts)
	}
}

func TestRebuildBM25AllPartitionsProfiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RAG_BM25_ROOT", root)
	input := filepath.Join(t.TempDir(), "chunks.jsonl")
	chunks := []rag.DocumentChunk{
		{ID: "k1", Content: "redis timeout runbook", SourceType: "knowledge"},
		{ID: "o1", Content: "pod crashloop final report", Metadata: map[string]any{"type": "final_report"}},
	}
	if err := writeChunks(input, chunks); err != nil {
		t.Fatal(err)
	}
	_ = captureStdout(t, func() error {
		return rebuildBM25(nil, []string{"-profile", "all", "-input", input})
	})
	knowledge, err := rag.NewProfileBM25Index(root, rag.ProfileKnowledge)
	if err != nil {
		t.Fatal(err)
	}
	ops, err := rag.NewProfileBM25Index(root, rag.ProfileOpsCase)
	if err != nil {
		t.Fatal(err)
	}
	kgot, err := knowledge.Search(nil, "redis timeout", 3)
	if err != nil {
		t.Fatal(err)
	}
	ogot, err := ops.Search(nil, "crashloop final report", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(kgot) != 1 || kgot[0].ID != "k1" {
		t.Fatalf("knowledge partition search=%#v", kgot)
	}
	if len(ogot) != 1 || ogot[0].ID != "o1" {
		t.Fatalf("ops partition search=%#v", ogot)
	}
}

func TestRebuildBM25AllReportsAmbiguousDefaultedKnowledge(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RAG_BM25_ROOT", root)
	input := filepath.Join(t.TempDir(), "chunks.jsonl")
	chunks := []rag.DocumentChunk{
		{ID: "k1", Content: "general runbook without source type"},
	}
	if err := writeChunks(input, chunks); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() error {
		return rebuildBM25(nil, []string{"-profile", "all", "-input", input})
	})
	var got struct {
		Status         string         `json:"status"`
		AmbiguousCount int            `json:"ambiguous_count"`
		Counts         map[string]int `json:"counts"`
	}
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "degraded" || got.AmbiguousCount != 1 || got.Counts[string(rag.ProfileKnowledge)] != 1 {
		t.Fatalf("ambiguous partition not visible: output=%s parsed=%#v", output, got)
	}
}

func TestLimitRetrievedResultsReturnsJSONArraysForNoHits(t *testing.T) {
	got := limitRetrievedResults(nil, 3)
	if got == nil {
		t.Fatal("expected empty slice, got nil")
	}
	encoded, err := json.Marshal(map[string]any{"results": got})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "\"results\":[]") {
		t.Fatalf("expected empty JSON array, got %s", encoded)
	}
}

func TestSeedEvalDatasetHasPlanCoverage(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "..", "testdata", "rag_eval_seed.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	total := 0
	profiles := map[string]int{}
	for scanner.Scan() {
		line := strings.TrimPrefix(strings.TrimSpace(scanner.Text()), "\ufeff")
		if line == "" {
			continue
		}
		item := rag.EvalCase{}
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			t.Fatal(err)
		}
		total++
		profiles[string(item.Profile)]++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if total < 40 {
		t.Fatalf("seed case count=%d want >=40", total)
	}
	if profiles[string(rag.ProfileKnowledge)] == 0 || profiles[string(rag.ProfileOpsCase)] == 0 {
		t.Fatalf("seed profiles missing coverage: %#v", profiles)
	}
}

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	runErr := fn()
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	os.Stdout = oldStdout
	output, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	return string(output)
}
