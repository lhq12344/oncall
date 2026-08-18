package main

import (
	"bufio"
	"context"
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
	if err := idx.Upsert(context.Background(), []rag.DocumentChunk{{ID: "doc1", ChunkID: "doc1", Content: "redis timeout runbook"}}); err != nil {
		t.Fatal(err)
	}
	gold := filepath.Join(t.TempDir(), "gold.jsonl")
	if err := os.WriteFile(gold, []byte("{\"id\":\"case1\",\"profile\":\"knowledge\",\"query\":\"redis timeout\",\"expected_ids\":[\"doc1\"]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() error {
		return evalCases(context.Background(), []string{"-dataset", gold, "-profile", "knowledge"})
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
		return rebuildBM25(context.Background(), []string{"-profile", "all", "-input", input})
	})
	knowledge, err := rag.NewProfileBM25Index(root, rag.ProfileKnowledge)
	if err != nil {
		t.Fatal(err)
	}
	ops, err := rag.NewProfileBM25Index(root, rag.ProfileOpsCase)
	if err != nil {
		t.Fatal(err)
	}
	kgot, err := knowledge.Search(context.Background(), "redis timeout", 3)
	if err != nil {
		t.Fatal(err)
	}
	ogot, err := ops.Search(context.Background(), "crashloop final report", 3)
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
		return rebuildBM25(context.Background(), []string{"-profile", "all", "-input", input})
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

func TestEvalCasesUsesCorpusForTemporaryBM25(t *testing.T) {
	corpus := filepath.Join(t.TempDir(), "corpus.jsonl")
	chunks := []rag.DocumentChunk{
		{ID: "knowledge_doc1", ChunkID: "knowledge_doc1", SourceType: "knowledge", Content: "redis timeout runbook connection troubleshooting"},
		{ID: "ops_doc1", ChunkID: "ops_doc1", SourceType: "ops_final_report", Content: "pod crashloop final report root cause oom"},
	}
	if err := writeChunks(corpus, chunks); err != nil {
		t.Fatal(err)
	}
	dataset := filepath.Join(t.TempDir(), "gold.jsonl")
	payload := "{\"id\":\"case1\",\"profile\":\"knowledge\",\"query\":\"redis timeout troubleshooting\",\"expected_ids\":[\"knowledge_doc1\"]}\n" +
		"{\"id\":\"case2\",\"profile\":\"ops_case\",\"query\":\"pod crashloop root cause\",\"expected_ids\":[\"ops_doc1\"]}\n"
	if err := os.WriteFile(dataset, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() error {
		return evalCases(context.Background(), []string{"-dataset", dataset, "-profile", "all", "-corpus", corpus})
	})
	got := decodeJSONMap(t, output)
	if got["status"] != "success" || !boolField(got, "quality_gate_shape_ready") || intField(got, "scored_count") != 2 || intField(got, "unscored_count") != 0 {
		t.Fatalf("corpus-backed eval should be successful and shape-ready: output=%s parsed=%#v", output, got)
	}
	if got["corpus_mode"] != "temporary_bm25_rebuild" {
		t.Fatalf("corpus eval should report temporary rebuild mode: output=%s parsed=%#v", output, got)
	}
	summary, ok := got["summary"].(map[string]any)
	if !ok || intField(summary, "hits") != 2 {
		t.Fatalf("expected both corpus-backed cases to hit: output=%s parsed=%#v", output, got)
	}
}

func TestEvalCasesCorpusRejectsMissingExpectedIDs(t *testing.T) {
	corpus := filepath.Join(t.TempDir(), "corpus.jsonl")
	chunks := []rag.DocumentChunk{
		{ID: "doc1", ChunkID: "doc1", SourceType: "knowledge", Content: "redis timeout runbook connection troubleshooting"},
	}
	if err := writeChunks(corpus, chunks); err != nil {
		t.Fatal(err)
	}
	dataset := filepath.Join(t.TempDir(), "gold.jsonl")
	if err := os.WriteFile(dataset, []byte("{\"id\":\"case1\",\"profile\":\"knowledge\",\"query\":\"redis timeout troubleshooting\",\"expected_ids\":[\"not_in_corpus\"]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() error {
		return evalCases(context.Background(), []string{"-dataset", dataset, "-profile", "knowledge", "-corpus", corpus})
	})
	got := decodeJSONMap(t, output)
	if got["status"] != "degraded" || boolField(got, "quality_gate_shape_ready") || boolField(got, "expected_ids_complete") {
		t.Fatalf("missing corpus expected_id should degrade and fail shape readiness: output=%s parsed=%#v", output, got)
	}
	if !strings.Contains(strings.Join(stringSliceField(got, "degraded_reasons"), "\n"), "missing from the supplied corpus") {
		t.Fatalf("expected missing-corpus degraded reason: %#v", got["degraded_reasons"])
	}
	missing, ok := got["missing_expected_ids"].([]any)
	if !ok || len(missing) != 1 {
		t.Fatalf("expected one missing expected_id entry: output=%s parsed=%#v", output, got)
	}
	entry, ok := missing[0].(map[string]any)
	if !ok || entry["case_id"] != "case1" || entry["expected_id"] != "not_in_corpus" {
		t.Fatalf("unexpected missing expected_id entry: %#v", missing[0])
	}
	summary, ok := got["summary"].(map[string]any)
	if !ok || intField(summary, "hits") != 0 {
		t.Fatalf("expected miss to remain visible in metrics: output=%s parsed=%#v", output, got)
	}
}

func TestEvalCasesCorpusFailsRetrievalMetricGateOnMiss(t *testing.T) {
	corpus := filepath.Join(t.TempDir(), "corpus.jsonl")
	chunks := []rag.DocumentChunk{
		{ID: "doc1", ChunkID: "doc1", SourceType: "knowledge", Content: "redis timeout runbook connection troubleshooting"},
	}
	if err := writeChunks(corpus, chunks); err != nil {
		t.Fatal(err)
	}
	dataset := filepath.Join(t.TempDir(), "gold.jsonl")
	if err := os.WriteFile(dataset, []byte("{\"id\":\"case1\",\"profile\":\"knowledge\",\"query\":\"mysql deadlock\",\"expected_ids\":[\"doc1\"]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() error {
		return evalCases(context.Background(), []string{"-dataset", dataset, "-profile", "knowledge", "-corpus", corpus})
	})
	got := decodeJSONMap(t, output)
	if got["status"] != "degraded" || !boolField(got, "quality_gate_shape_ready") || !boolField(got, "expected_ids_complete") || boolField(got, "retrieval_metric_gate_pass") {
		t.Fatalf("corpus eval miss should degrade metric gate while preserving shape readiness: output=%s parsed=%#v", output, got)
	}
	if !strings.Contains(strings.Join(stringSliceField(got, "degraded_reasons"), "\n"), "retrieval metrics did not pass") {
		t.Fatalf("expected retrieval metric degraded reason: %#v", got["degraded_reasons"])
	}
	summary, ok := got["summary"].(map[string]any)
	if !ok || intField(summary, "hits") != 0 {
		t.Fatalf("expected miss to remain visible in metrics: output=%s parsed=%#v", output, got)
	}
}

func TestEvalCasesFlagsUnscoredDatasetNotQualityGate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RAG_BM25_ROOT", root)
	idx, err := rag.NewProfileBM25Index(root, rag.ProfileKnowledge)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(context.Background(), []rag.DocumentChunk{{ID: "doc1", ChunkID: "doc1", Content: "redis timeout runbook"}}); err != nil {
		t.Fatal(err)
	}
	dataset := filepath.Join(t.TempDir(), "seed.jsonl")
	if err := os.WriteFile(dataset, []byte("{\"id\":\"case1\",\"profile\":\"knowledge\",\"query\":\"redis timeout\",\"expected_ids\":[]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() error {
		return evalCases(context.Background(), []string{"-dataset", dataset, "-profile", "knowledge"})
	})
	got := decodeJSONMap(t, output)
	if got["status"] != "degraded" || boolField(got, "quality_gate_shape_ready") || boolField(got, "expected_ids_complete") || intField(got, "scored_count") != 0 || intField(got, "unscored_count") != 1 {
		t.Fatalf("unscored dataset should be marked non-gating: output=%s parsed=%#v", output, got)
	}
	if _, ok := got["quality_gate_ready"]; ok {
		t.Fatalf("eval output must not mechanically claim quality_gate_ready: output=%s", output)
	}
	if !strings.Contains(strings.Join(stringSliceField(got, "degraded_reasons"), "\n"), "expected_ids") {
		t.Fatalf("expected explicit expected_ids guidance: %#v", got["degraded_reasons"])
	}
}

func TestEvalCasesExpectedIDsCompleteIsOnlyShapeReady(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RAG_BM25_ROOT", root)
	idx, err := rag.NewProfileBM25Index(root, rag.ProfileKnowledge)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(context.Background(), []rag.DocumentChunk{{ID: "doc1", ChunkID: "doc1", Content: "redis timeout runbook"}}); err != nil {
		t.Fatal(err)
	}
	dataset := filepath.Join(t.TempDir(), "gold.jsonl")
	if err := os.WriteFile(dataset, []byte("{\"id\":\"case1\",\"profile\":\"knowledge\",\"query\":\"redis timeout\",\"expected_ids\":[\"model_guess_id\"]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() error {
		return evalCases(context.Background(), []string{"-dataset", dataset, "-profile", "knowledge"})
	})
	got := decodeJSONMap(t, output)
	if !boolField(got, "quality_gate_shape_ready") || !boolField(got, "expected_ids_complete") {
		t.Fatalf("non-empty expected_ids should only make the dataset shape-ready: output=%s parsed=%#v", output, got)
	}
	if _, ok := got["quality_gate_ready"]; ok {
		t.Fatalf("eval output must not claim true quality readiness without corpus confirmation metadata: output=%s", output)
	}
	if !strings.Contains(stringField(got, "dataset_boundary"), "manual") || !strings.Contains(stringField(got, "quality_gate_ready_note"), "cannot prove manual corpus confirmation") {
		t.Fatalf("output should explain manual confirmation boundary: output=%s parsed=%#v", output, got)
	}
}

func TestEvalCasesFlagsNoSelectedProfileCases(t *testing.T) {
	dataset := filepath.Join(t.TempDir(), "ops_only.jsonl")
	if err := os.WriteFile(dataset, []byte("{\"id\":\"case1\",\"profile\":\"ops_case\",\"query\":\"pod crashloop\",\"expected_ids\":[\"ops1\"]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() error {
		return evalCases(context.Background(), []string{"-dataset", dataset, "-profile", "knowledge"})
	})
	got := decodeJSONMap(t, output)
	if got["status"] != "degraded" || boolField(got, "quality_gate_shape_ready") || intField(got, "scored_count") != 0 || intField(got, "unscored_count") != 0 {
		t.Fatalf("no selected profile cases should be degraded and non-ready: output=%s parsed=%#v", output, got)
	}
	if !strings.Contains(strings.Join(stringSliceField(got, "degraded_reasons"), "\n"), "no cases") {
		t.Fatalf("expected no-cases degraded reason: %#v", got["degraded_reasons"])
	}
}

func TestEvalCasesFlagsMixedScoredAndUnscoredCases(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RAG_BM25_ROOT", root)
	idx, err := rag.NewProfileBM25Index(root, rag.ProfileKnowledge)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(context.Background(), []rag.DocumentChunk{{ID: "doc1", ChunkID: "doc1", Content: "redis timeout runbook"}}); err != nil {
		t.Fatal(err)
	}
	dataset := filepath.Join(t.TempDir(), "mixed.jsonl")
	payload := "{\"id\":\"case1\",\"profile\":\"knowledge\",\"query\":\"redis timeout\",\"expected_ids\":[\"doc1\"]}\n" +
		"{\"id\":\"case2\",\"profile\":\"knowledge\",\"query\":\"memory oom\",\"expected_ids\":[]}\n"
	if err := os.WriteFile(dataset, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() error {
		return evalCases(context.Background(), []string{"-dataset", dataset, "-profile", "knowledge"})
	})
	got := decodeJSONMap(t, output)
	if got["status"] != "degraded" || boolField(got, "quality_gate_shape_ready") || intField(got, "scored_count") != 1 || intField(got, "unscored_count") != 1 {
		t.Fatalf("mixed scored/unscored cases should be degraded and non-ready: output=%s parsed=%#v", output, got)
	}
	if !strings.Contains(strings.Join(stringSliceField(got, "degraded_reasons"), "\n"), "unscored cases") {
		t.Fatalf("expected unscored-cases degraded reason: %#v", got["degraded_reasons"])
	}
}

func TestInspectLabelsOfflineBM25Boundary(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RAG_BM25_ROOT", root)
	idx, err := rag.NewProfileBM25Index(root, rag.ProfileKnowledge)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(context.Background(), []rag.DocumentChunk{{ID: "doc1", ChunkID: "doc1", Content: "redis timeout runbook"}}); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() error {
		return inspect(context.Background(), []string{"-profile", "knowledge", "-query", "redis timeout", "-top-k", "5", "-final-top-k", "3"})
	})
	var got struct {
		InspectionMode  string         `json:"inspection_mode"`
		LiveHybridTrace bool           `json:"live_hybrid_trace"`
		Scope           string         `json:"scope"`
		RetrievalMode   string         `json:"retrieval_mode"`
		Rewriter        string         `json:"rewriter"`
		CandidateCounts map[string]int `json:"candidate_counts"`
	}
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatal(err)
	}
	if got.InspectionMode != "offline_bm25_only" || got.LiveHybridTrace || got.RetrievalMode != "bm25_offline" {
		t.Fatalf("inspect boundary not explicit: output=%s parsed=%#v", output, got)
	}
	if !strings.Contains(got.Scope, "does not invoke query rewrite") || !strings.Contains(got.Rewriter, "not invoked") {
		t.Fatalf("inspect should explain omitted live hybrid steps: output=%s parsed=%#v", output, got)
	}
	if got.CandidateCounts[rag.CandidateCountSourceBM25Docs] == 0 {
		t.Fatalf("inspect candidate counts not populated: %#v", got.CandidateCounts)
	}
}

func TestInspectNoQueryLabelsOfflineBM25Boundary(t *testing.T) {
	output := captureStdout(t, func() error {
		return inspect(context.Background(), []string{"-profile", "knowledge"})
	})
	got := decodeJSONMap(t, output)
	if got["inspection_mode"] != "offline_bm25_only" || boolField(got, "live_hybrid_trace") || got["retrieval_mode"] != "bm25_offline" || got["rewriter"] != "not invoked by offline CLI" {
		t.Fatalf("no-query inspect should still label offline boundary: output=%s parsed=%#v", output, got)
	}
	milvus, ok := got["milvus"].(map[string]any)
	if !ok || !strings.Contains(stringField(milvus, "scope"), "config_only") {
		t.Fatalf("inspect milvus block should be labelled config-only: output=%s parsed=%#v", output, got)
	}
}

func TestSeedEvalDatasetHasNoUTF8BOM(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "rag_eval_seed.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) >= 3 && data[0] == 0xef && data[1] == 0xbb && data[2] == 0xbf {
		t.Fatal("seed eval fixture must not start with a UTF-8 BOM")
	}
}

func decodeJSONMap(t *testing.T, output string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func boolField(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

func intField(values map[string]any, key string) int {
	switch value := values[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		return 0
	}
}

func stringField(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func stringSliceField(values map[string]any, key string) []string {
	raw, _ := values[key].([]any)
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
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
