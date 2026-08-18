package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go_agent/internal/rag"
	"go_agent/utility/common"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}
	switch args[0] {
	case "inspect":
		return inspect(ctx, args[1:])
	case "rebuild-bm25":
		return rebuildBM25(ctx, args[1:])
	case "eval":
		return evalCases(ctx, args[1:])
	case "backfill-v2":
		return backfillV2(ctx, args[1:])
	case "-h", "--help", "help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage() {
	fmt.Println("usage: ragctl <inspect|rebuild-bm25|eval|backfill-v2> [flags]")
	fmt.Println("  inspect      --profile knowledge|ops_case [--query Q] [--top-k 20] [--final-top-k 3]")
	fmt.Println("  rebuild-bm25 --profile knowledge|ops_case|all --input chunks.jsonl")
	fmt.Println("  eval         --dataset testdata/rag_eval_gold.jsonl --profile knowledge|ops_case|all")
	fmt.Println("  backfill-v2  --profile knowledge|ops_case|all [--input legacy.jsonl --output v2.jsonl --dry-run=false]")
}

func inspect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	profile := fs.String("profile", "knowledge", "knowledge or ops_case")
	query := fs.String("query", "", "query to inspect against the local BM25 index")
	topK := fs.Int("top-k", 20, "candidate count")
	finalTopK := fs.Int("final-top-k", 0, "final result count, default from rag.final_top_k")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateProfile(*profile); err != nil {
		return err
	}
	ragConfig := rag.LoadConfig(ctx)
	milvusConfig := common.LoadMilvusConfig(ctx)
	indexPath := filepath.Join(ragConfig.BM25Root, *profile+".jsonl")
	out := map[string]any{
		"status": "ok",
		"rag": map[string]any{
			"hybrid_enabled":   ragConfig.HybridEnabled,
			"rewrite_enabled":  ragConfig.RewriteEnabled,
			"bm25_enabled":     ragConfig.BM25Enabled,
			"reranker_enabled": ragConfig.RerankerEnabled,
			"bm25_root":        ragConfig.BM25Root,
			"embedding_top_k":  ragConfig.EmbeddingTopK,
			"bm25_top_k":       ragConfig.BM25TopK,
			"fusion_top_k":     ragConfig.FusionTopK,
			"final_top_k":      ragConfig.FinalTopK,
			"max_final_top_k":  ragConfig.MaxFinalTopK,
			"rrf_k":            ragConfig.RRFK,
		},
		"milvus": map[string]any{
			"database":                milvusConfig.Database,
			"auto_create_collection":  milvusConfig.AutoCreateCollection,
			"legacy_knowledge":        milvusConfig.Collection,
			"legacy_ops_cases":        common.MilvusOpsCollection,
			"knowledge_v2_collection": milvusConfig.KnowledgeV2Collection,
			"ops_v2_collection":       milvusConfig.OpsV2Collection,
		},
		"bm25": map[string]any{
			"profile": *profile,
			"path":    indexPath,
		},
	}
	if strings.TrimSpace(*query) != "" {
		index, err := rag.NewProfileBM25Index(ragConfig.BM25Root, rag.RetrievalProfile(*profile))
		if err != nil {
			out["status"] = "degraded"
			out["degraded_reasons"] = []string{"bm25 index unavailable: " + err.Error()}
		} else {
			start := time.Now()
			results, err := index.Search(ctx, *query, *topK)
			results = nonNilRetrievedResults(results)
			out["latency_ms"] = float64(time.Since(start).Microseconds()) / 1000
			if err != nil {
				out["status"] = "degraded"
				out["degraded_reasons"] = []string{"bm25 search failed: " + err.Error()}
			} else {
				finalCount := ragConfig.CapFinalTopK(*finalTopK)
				finalResults := limitRetrievedResults(results, finalCount)
				out["query"] = *query
				out["retrieval_mode"] = "bm25_offline"
				out["rewriter"] = "not invoked by offline CLI"
				out["candidate_counts"] = map[string]int{
					rag.CandidateCountSourceBM25Docs: len(results),
					rag.CandidateCountStageFinalDocs: len(finalResults),
				}
				out["candidate_lists"] = map[string]any{"bm25": results}
				out["candidates"] = results
				out["final_results"] = finalResults
			}
		}
	}
	return writeJSON(out)
}

func rebuildBM25(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rebuild-bm25", flag.ContinueOnError)
	profile := fs.String("profile", "knowledge", "knowledge, ops_case, or all")
	input := fs.String("input", "", "jsonl file containing rag.DocumentChunk records")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *profile != "all" {
		if err := validateProfile(*profile); err != nil {
			return err
		}
	}
	if strings.TrimSpace(*input) == "" {
		return fmt.Errorf("-input is required")
	}
	chunks, err := readChunks(*input)
	if err != nil {
		return err
	}
	if *profile == "all" {
		return rebuildBM25All(ctx, chunks)
	}
	normalized := normalizeChunks(chunks, *profile)
	ragConfig := rag.LoadConfig(ctx)
	idx, err := rag.NewProfileBM25Index(ragConfig.BM25Root, rag.RetrievalProfile(*profile))
	if err != nil {
		return err
	}
	if err := idx.Rebuild(ctx, normalized); err != nil {
		return err
	}
	return writeJSON(map[string]any{
		"status":  "ok",
		"profile": *profile,
		"count":   len(normalized),
	})
}

func rebuildBM25All(ctx context.Context, chunks []rag.DocumentChunk) error {
	partitioned := partitionChunksByProfile(normalizeChunks(chunks, "all"))
	ragConfig := rag.LoadConfig(ctx)
	counts := map[string]int{}
	for _, profile := range []rag.RetrievalProfile{rag.ProfileKnowledge, rag.ProfileOpsCase} {
		idx, err := rag.NewProfileBM25Index(ragConfig.BM25Root, profile)
		if err != nil {
			return err
		}
		profileChunks := partitioned.Chunks[profile]
		if err := idx.Rebuild(ctx, profileChunks); err != nil {
			return err
		}
		counts[string(profile)] = len(profileChunks)
	}
	status := "ok"
	degraded := []string{}
	if len(partitioned.Ambiguous) > 0 {
		status = "degraded"
		degraded = append(degraded, "ambiguous profile classification defaulted to knowledge")
	}
	out := map[string]any{
		"status":          "ok",
		"profile":         "all",
		"counts":          counts,
		"ambiguous_count": len(partitioned.Ambiguous),
	}
	out["status"] = status
	if len(degraded) > 0 {
		out["degraded_reasons"] = degraded
		out["ambiguous_chunks"] = partitioned.Ambiguous
	}
	return writeJSON(out)
}

type profilePartitions struct {
	Chunks    map[rag.RetrievalProfile][]rag.DocumentChunk
	Ambiguous []ambiguousProfileChunk
}

type ambiguousProfileChunk struct {
	ID              string               `json:"id,omitempty"`
	ChunkID         string               `json:"chunk_id,omitempty"`
	SourceType      string               `json:"source_type,omitempty"`
	AssignedProfile rag.RetrievalProfile `json:"assigned_profile"`
	Reason          string               `json:"reason"`
}

type chunkProfileDecision struct {
	Profile              rag.RetrievalProfile
	NormalizedSourceType string
	DeclaredSourceType   string
	Ambiguous            bool
	Reason               string
}

func partitionChunksByProfile(chunks []rag.DocumentChunk) profilePartitions {
	out := profilePartitions{
		Chunks: map[rag.RetrievalProfile][]rag.DocumentChunk{
			rag.ProfileKnowledge: {},
			rag.ProfileOpsCase:   {},
		},
	}
	for _, chunk := range chunks {
		decision := classifyChunkProfile(chunk)
		if decision.NormalizedSourceType != "" {
			chunk.SourceType = decision.NormalizedSourceType
		}
		out.Chunks[decision.Profile] = append(out.Chunks[decision.Profile], chunk)
		if decision.Ambiguous {
			out.Ambiguous = append(out.Ambiguous, ambiguousProfileChunk{
				ID:              chunk.ID,
				ChunkID:         chunk.ChunkID,
				SourceType:      decision.DeclaredSourceType,
				AssignedProfile: decision.Profile,
				Reason:          decision.Reason,
			})
		}
	}
	return out
}

func chunkRetrievalProfile(chunk rag.DocumentChunk) rag.RetrievalProfile {
	return classifyChunkProfile(chunk).Profile
}

func classifyChunkProfile(chunk rag.DocumentChunk) chunkProfileDecision {
	declared, hasDeclared := declaredChunkSourceType(chunk)
	normalized := normalizeSourceTypeValue(declared)
	if isOpsSourceType(normalized) {
		return chunkProfileDecision{Profile: rag.ProfileOpsCase, NormalizedSourceType: normalized, DeclaredSourceType: declared}
	}
	if isKnowledgeSourceType(normalized) {
		return chunkProfileDecision{Profile: rag.ProfileKnowledge, NormalizedSourceType: "knowledge", DeclaredSourceType: declared}
	}
	if hasDeclared {
		return chunkProfileDecision{
			Profile:              rag.ProfileKnowledge,
			NormalizedSourceType: "knowledge",
			DeclaredSourceType:   declared,
			Ambiguous:            true,
			Reason:               "unrecognized source_type defaulted to knowledge",
		}
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(chunk.ID)), "ops_") ||
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(chunk.ChunkID)), "ops_") {
		return chunkProfileDecision{Profile: rag.ProfileOpsCase, NormalizedSourceType: "ops_case"}
	}
	return chunkProfileDecision{
		Profile:              rag.ProfileKnowledge,
		NormalizedSourceType: "knowledge",
		Ambiguous:            true,
		Reason:               "missing source_type defaulted to knowledge",
	}
}

func declaredChunkSourceType(chunk rag.DocumentChunk) (string, bool) {
	if sourceType := strings.ToLower(strings.TrimSpace(chunk.SourceType)); sourceType != "" && sourceType != "<nil>" {
		return sourceType, true
	}
	if chunk.Metadata != nil {
		for _, key := range []string{"source_type", "type", "profile"} {
			value := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", chunk.Metadata[key])))
			if value != "" && value != "<nil>" {
				return value, true
			}
		}
	}
	return "", false
}

func normalizeSourceTypeValue(sourceType string) string {
	sourceType = strings.ToLower(strings.TrimSpace(sourceType))
	switch sourceType {
	case "final_report", "ops_final_report":
		return "ops_final_report"
	default:
		return sourceType
	}
}

func isOpsSourceType(sourceType string) bool {
	return sourceType == "ops_case" ||
		sourceType == "ops_final_report" ||
		strings.Contains(sourceType, "ops") ||
		strings.Contains(sourceType, "incident")
}

func isKnowledgeSourceType(sourceType string) bool {
	return sourceType == "knowledge" ||
		strings.Contains(sourceType, "knowledge") ||
		strings.Contains(sourceType, "runbook") ||
		strings.Contains(sourceType, "document") ||
		strings.Contains(sourceType, "doc") ||
		strings.Contains(sourceType, "guide") ||
		strings.Contains(sourceType, "kb") ||
		strings.Contains(sourceType, "faq")
}

func limitRetrievedResults(results []rag.RetrievedResult, topK int) []rag.RetrievedResult {
	results = nonNilRetrievedResults(results)
	if topK <= 0 || len(results) <= topK {
		return results
	}
	return results[:topK]
}

func nonNilRetrievedResults(results []rag.RetrievedResult) []rag.RetrievedResult {
	if results == nil {
		return []rag.RetrievedResult{}
	}
	return results
}

type evalCaseTelemetry struct {
	Status          string               `json:"status"`
	Profile         rag.RetrievalProfile `json:"profile"`
	LatencyMS       float64              `json:"latency_ms,omitempty"`
	CandidateCounts map[string]int       `json:"candidate_counts,omitempty"`
	Count           int                  `json:"count"`
	DegradedReasons []string             `json:"degraded_reasons,omitempty"`
}

func evalCases(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	gold := fs.String("gold", "", "jsonl file containing rag.EvalCase records")
	dataset := fs.String("dataset", "", "alias for -gold; preferred by the implementation plan")
	profile := fs.String("profile", "all", "knowledge, ops_case, or all")
	topK := fs.Int("top-k", 20, "candidate count used for Recall@20")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *profile != "all" {
		if err := validateProfile(*profile); err != nil {
			return err
		}
	}
	datasetPath := firstNonEmpty(*dataset, *gold, "testdata/rag_eval_gold.jsonl")
	cases, err := readEvalCases(datasetPath)
	if err != nil {
		return err
	}
	ragConfig := rag.LoadConfig(ctx)
	contexts := make(map[string]*rag.RetrievedContext, len(cases))
	caseTelemetry := make(map[string]evalCaseTelemetry, len(cases))
	indexes := make(map[rag.RetrievalProfile]*rag.FileBM25Index)
	degraded := make([]string, 0)
	var latencyTotal time.Duration
	ran := 0
	filteredCases := make([]rag.EvalCase, 0, len(cases))
	for _, item := range cases {
		if *profile != "all" && string(item.Profile) != *profile {
			continue
		}
		filteredCases = append(filteredCases, item)
		if item.Profile != rag.ProfileKnowledge && item.Profile != rag.ProfileOpsCase {
			degraded = append(degraded, item.ID+": unsupported profile")
			caseTelemetry[item.ID] = evalCaseTelemetry{
				Status:          "degraded",
				Profile:         item.Profile,
				DegradedReasons: []string{"unsupported profile"},
			}
			continue
		}
		index := indexes[item.Profile]
		if index == nil {
			index, err = rag.NewProfileBM25Index(ragConfig.BM25Root, item.Profile)
			if err != nil {
				degraded = append(degraded, string(item.Profile)+": "+err.Error())
				caseTelemetry[item.ID] = evalCaseTelemetry{
					Status:          "degraded",
					Profile:         item.Profile,
					DegradedReasons: []string{err.Error()},
				}
				continue
			}
			indexes[item.Profile] = index
		}
		start := time.Now()
		results, searchErr := index.Search(ctx, item.Query, *topK)
		results = nonNilRetrievedResults(results)
		elapsed := time.Since(start)
		latencyTotal += elapsed
		ran++
		if searchErr != nil {
			degraded = append(degraded, item.ID+": "+searchErr.Error())
			caseTelemetry[item.ID] = evalCaseTelemetry{
				Status:          "degraded",
				Profile:         item.Profile,
				LatencyMS:       float64(elapsed.Microseconds()) / 1000,
				DegradedReasons: []string{searchErr.Error()},
			}
			continue
		}
		context := &rag.RetrievedContext{
			Status:    "success",
			Profile:   item.Profile,
			Query:     item.Query,
			LatencyMS: float64(elapsed.Microseconds()) / 1000,
			CandidateCounts: map[string]int{
				rag.CandidateCountSourceBM25Docs: len(results),
				rag.CandidateCountStageFinalDocs: len(results),
			},
			Count:   len(results),
			Results: results,
		}
		contexts[item.ID] = context
		caseTelemetry[item.ID] = telemetryFromRetrievedContext(context)
	}
	if *profile != "all" {
		cases = filteredCases
	}
	summary := rag.EvaluateRetrievedContexts(cases, contexts)
	status := "success"
	if len(degraded) > 0 {
		status = "degraded"
	}
	var averageLatency float64
	if ran > 0 {
		averageLatency = float64(latencyTotal.Microseconds()) / 1000 / float64(ran)
	}
	return writeJSON(map[string]any{
		"status":             status,
		"dataset":            datasetPath,
		"retrieval_mode":     "bm25_offline",
		"average_latency_ms": averageLatency,
		"degraded_count":     len(degraded),
		"degraded_reasons":   degraded,
		"case_telemetry":     caseTelemetry,
		"summary":            summary,
	})
}

func telemetryFromRetrievedContext(context *rag.RetrievedContext) evalCaseTelemetry {
	if context == nil {
		return evalCaseTelemetry{}
	}
	return evalCaseTelemetry{
		Status:          context.Status,
		Profile:         context.Profile,
		LatencyMS:       context.LatencyMS,
		CandidateCounts: context.CandidateCounts,
		Count:           context.Count,
		DegradedReasons: context.DegradedReasons,
	}
}

func backfillV2(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("backfill-v2", flag.ContinueOnError)
	profile := fs.String("profile", "knowledge", "knowledge, ops_case, or all")
	input := fs.String("input", "", "legacy JSONL file containing rag.DocumentChunk records")
	output := fs.String("output", "", "normalized v2 JSONL output path")
	dryRun := fs.Bool("dry-run", true, "report intended backfill without writing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *profile != "all" {
		if err := validateProfile(*profile); err != nil {
			return err
		}
	}
	milvusConfig := common.LoadMilvusConfig(ctx)
	out := map[string]any{
		"status":                  "planned",
		"mode":                    "canonical_jsonl_export",
		"dry_run":                 *dryRun,
		"profile":                 *profile,
		"legacy_knowledge":        milvusConfig.Collection,
		"legacy_ops_cases":        common.MilvusOpsCollection,
		"knowledge_v2_collection": milvusConfig.KnowledgeV2Collection,
		"ops_v2_collection":       milvusConfig.OpsV2Collection,
	}
	if strings.TrimSpace(*input) == "" {
		if !*dryRun {
			return fmt.Errorf("-input is required when -dry-run=false; live Milvus migration is intentionally not implicit")
		}
		return writeJSON(out)
	}
	chunks, err := readChunks(*input)
	if err != nil {
		return err
	}
	normalized := normalizeChunks(chunks, *profile)
	out["input_count"] = len(chunks)
	out["normalized_count"] = len(normalized)
	if !*dryRun {
		if strings.TrimSpace(*output) == "" {
			return fmt.Errorf("-output is required when -dry-run=false")
		}
		if err := writeChunks(*output, normalized); err != nil {
			return err
		}
		out["status"] = "ok"
		out["output"] = *output
	} else {
		out["status"] = "dry_run"
	}
	return writeJSON(out)
}

func validateProfile(profile string) error {
	switch rag.RetrievalProfile(profile) {
	case rag.ProfileKnowledge, rag.ProfileOpsCase:
		return nil
	default:
		return fmt.Errorf("unsupported profile %q", profile)
	}
}

func normalizeChunks(chunks []rag.DocumentChunk, profile string) []rag.DocumentChunk {
	out := make([]rag.DocumentChunk, 0, len(chunks))
	for _, chunk := range chunks {
		chunk.SourceType = normalizeChunkSourceType(chunk, profile)
		chunk.Content = strings.TrimSpace(chunk.Content)
		if chunk.Content == "" {
			continue
		}
		if strings.TrimSpace(chunk.ContentHash) == "" {
			chunk.ContentHash = rag.ContentHash(chunk.Content)
		}
		if strings.TrimSpace(chunk.ChunkID) == "" {
			chunk.ChunkID = strings.TrimSpace(chunk.ID)
		}
		if strings.TrimSpace(chunk.ID) == "" {
			chunk.ID = firstNonEmpty(chunk.ChunkID, chunk.ContentHash)
		}
		if strings.TrimSpace(chunk.DocID) == "" && chunk.Metadata != nil {
			chunk.DocID = firstNonEmpty(fmt.Sprintf("%v", chunk.Metadata["doc_id"]), fmt.Sprintf("%v", chunk.Metadata["source_id"]))
		}
		out = append(out, chunk)
	}
	return out
}

func normalizeChunkSourceType(chunk rag.DocumentChunk, profile string) string {
	if profile == string(rag.ProfileKnowledge) {
		return "knowledge"
	}
	if profile == string(rag.ProfileOpsCase) {
		if sourceType, ok := declaredChunkSourceType(chunk); ok && isOpsSourceType(normalizeSourceTypeValue(sourceType)) {
			return normalizeSourceTypeValue(sourceType)
		}
		return "ops_case"
	}
	if profile == "all" {
		if sourceType, ok := declaredChunkSourceType(chunk); ok {
			return normalizeSourceTypeValue(sourceType)
		}
		return ""
	}
	if sourceType := strings.TrimSpace(chunk.SourceType); sourceType != "" && sourceType != "<nil>" {
		return normalizeSourceTypeValue(sourceType)
	}
	if chunk.Metadata != nil {
		for _, key := range []string{"source_type", "type", "profile"} {
			value := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", chunk.Metadata[key])))
			if value == "" || value == "<nil>" {
				continue
			}
			value = normalizeSourceTypeValue(value)
			if isOpsSourceType(value) {
				return value
			}
			if isKnowledgeSourceType(value) {
				return value
			}
		}
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(chunk.ID)), "ops_") {
		return "ops_case"
	}
	return "knowledge"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func readChunks(path string) ([]rag.DocumentChunk, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var chunks []rag.DocumentChunk
	for scanner.Scan() {
		line := strings.TrimPrefix(strings.TrimSpace(scanner.Text()), "\ufeff")
		if line == "" {
			continue
		}
		var chunk rag.DocumentChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	return chunks, scanner.Err()
}

func writeChunks(path string, chunks []rag.DocumentChunk) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	for _, chunk := range chunks {
		if err := enc.Encode(chunk); err != nil {
			return err
		}
	}
	return nil
}

func readEvalCases(path string) ([]rag.EvalCase, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var cases []rag.EvalCase
	for scanner.Scan() {
		line := strings.TrimPrefix(strings.TrimSpace(scanner.Text()), "\ufeff")
		if line == "" {
			continue
		}
		var item rag.EvalCase
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, err
		}
		cases = append(cases, item)
	}
	return cases, scanner.Err()
}

func writeJSON(value any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
