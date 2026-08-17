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
}

func inspect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	profile := fs.String("profile", "knowledge", "knowledge or ops_case")
	query := fs.String("query", "", "query to inspect against the local BM25 index")
	topK := fs.Int("top-k", 20, "candidate count")
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
			out["latency_ms"] = float64(time.Since(start).Microseconds()) / 1000
			if err != nil {
				out["status"] = "degraded"
				out["degraded_reasons"] = []string{"bm25 search failed: " + err.Error()}
			} else {
				out["query"] = *query
				out["retrieval_mode"] = "bm25_offline"
				out["rewriter"] = "not invoked by offline CLI"
				out["candidates"] = results
			}
		}
	}
	return writeJSON(out)
}

func rebuildBM25(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rebuild-bm25", flag.ContinueOnError)
	profile := fs.String("profile", "knowledge", "knowledge or ops_case")
	input := fs.String("input", "", "jsonl file containing rag.DocumentChunk records")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateProfile(*profile); err != nil {
		return err
	}
	if strings.TrimSpace(*input) == "" {
		return fmt.Errorf("-input is required")
	}
	chunks, err := readChunks(*input)
	if err != nil {
		return err
	}
	ragConfig := rag.LoadConfig(ctx)
	idx, err := rag.NewProfileBM25Index(ragConfig.BM25Root, rag.RetrievalProfile(*profile))
	if err != nil {
		return err
	}
	if err := idx.Rebuild(ctx, chunks); err != nil {
		return err
	}
	return writeJSON(map[string]any{
		"status":  "ok",
		"profile": *profile,
		"count":   len(chunks),
	})
}

func evalCases(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	gold := fs.String("gold", "testdata/rag_eval_gold.jsonl", "jsonl file containing rag.EvalCase records")
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
	cases, err := readEvalCases(*gold)
	if err != nil {
		return err
	}
	ragConfig := rag.LoadConfig(ctx)
	contexts := make(map[string]*rag.RetrievedContext, len(cases))
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
			continue
		}
		index := indexes[item.Profile]
		if index == nil {
			index, err = rag.NewProfileBM25Index(ragConfig.BM25Root, item.Profile)
			if err != nil {
				degraded = append(degraded, string(item.Profile)+": "+err.Error())
				continue
			}
			indexes[item.Profile] = index
		}
		start := time.Now()
		results, searchErr := index.Search(ctx, item.Query, *topK)
		latencyTotal += time.Since(start)
		ran++
		if searchErr != nil {
			degraded = append(degraded, item.ID+": "+searchErr.Error())
			continue
		}
		contexts[item.ID] = &rag.RetrievedContext{
			Status:  "success",
			Profile: item.Profile,
			Query:   item.Query,
			Count:   len(results),
			Results: results,
		}
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
		"retrieval_mode":     "bm25_offline",
		"average_latency_ms": averageLatency,
		"degraded_count":     len(degraded),
		"degraded_reasons":   degraded,
		"summary":            summary,
	})
}

func backfillV2(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("backfill-v2", flag.ContinueOnError)
	profile := fs.String("profile", "knowledge", "knowledge or ops_case")
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
		chunk.SourceType = strings.TrimSpace(chunk.SourceType)
		if chunk.SourceType == "" {
			if profile == string(rag.ProfileOpsCase) {
				chunk.SourceType = "ops_case"
			} else {
				chunk.SourceType = "knowledge"
			}
		}
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
		out = append(out, chunk)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
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
		line := strings.TrimSpace(scanner.Text())
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
		line := strings.TrimSpace(scanner.Text())
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
