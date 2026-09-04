package rag

import (
	"context"
	"fmt"
	"strings"
	"time"

	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
)

type HybridRetrieverConfig struct {
	Profile         RetrievalProfile
	Config          Config
	VectorRetriever einoretriever.Retriever
	BM25Index       BM25Index
	Rewriter        QueryRewriter
	Reranker        Reranker
}

type HybridRetriever struct {
	profile         RetrievalProfile
	config          Config
	vectorRetriever einoretriever.Retriever
	bm25Index       BM25Index
	rewriter        QueryRewriter
	reranker        Reranker
}

func NewHybridRetriever(cfg HybridRetrieverConfig) *HybridRetriever {
	if cfg.Config.EmbeddingTopK == 0 {
		cfg.Config = DefaultConfig()
	} else {
		cfg.Config.normalize()
	}
	if cfg.Profile == "" {
		cfg.Profile = ProfileKnowledge
	}
	if cfg.Rewriter == nil {
		cfg.Rewriter = NoopRewriter{}
	}
	return &HybridRetriever{
		profile:         cfg.Profile,
		config:          cfg.Config,
		vectorRetriever: cfg.VectorRetriever,
		bm25Index:       cfg.BM25Index,
		rewriter:        cfg.Rewriter,
		reranker:        cfg.Reranker,
	}
}

func (h *HybridRetriever) Retrieve(ctx context.Context, query string, opts ...einoretriever.Option) ([]*schema.Document, error) {
	common := einoretriever.GetCommonOptions(nil, opts...)
	topK := 0
	if common != nil && common.TopK != nil {
		topK = *common.TopK
	}
	out, err := h.RetrieveContext(ctx, query, topK)
	if err != nil {
		return nil, err
	}
	docs := make([]*schema.Document, 0, len(out.Results))
	for _, item := range out.Results {
		meta := cloneMeta(item.Meta)
		meta["retrieval_source"] = item.Source
		if len(item.RetrievalPath) > 0 {
			meta["retrieval_path"] = append([]string(nil), item.RetrievalPath...)
		}
		meta["rrf_score"] = item.RRFScore
		if item.VectorScore != 0 {
			meta["vector_score"] = item.VectorScore
		}
		if item.BM25Score != 0 {
			meta["bm25_score"] = item.BM25Score
		}
		doc := &schema.Document{ID: item.ID, Content: item.Content, MetaData: meta}
		doc.WithScore(item.Score)
		docs = append(docs, doc)
	}
	return docs, nil
}

func (h *HybridRetriever) RetrieveContext(ctx context.Context, query string, topK int) (*RetrievedContext, error) {
	if h == nil {
		return nil, fmt.Errorf("hybrid retriever is nil")
	}
	start := time.Now()
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	finalTopK := h.config.CapFinalTopK(topK)
	degraded := make([]string, 0, 4)
	candidateCounts := map[string]int{}

	rewrite, err := h.rewriter.Rewrite(ctx, RewriteInputFromContext(ctx, query))
	if err != nil {
		degraded = append(degraded, "query_rewrite_failed: "+err.Error())
		rewrite = RewriteResult{RewrittenQueries: []string{query}, Confidence: 0}
	}
	variants := NormalizeQueryVariants(query, rewrite.RewrittenQueries, 3)
	rewrite.RewrittenQueries = variants
	candidateCounts[CandidateCountStageQueryVariants] = len(variants)

	rankedLists := make([][]RetrievedResult, 0, len(variants)*2)
	for _, variant := range variants {
		if h.vectorRetriever != nil {
			docs, err := h.vectorRetriever.Retrieve(ctx, variant, einoretriever.WithTopK(h.config.EmbeddingTopK))
			if err != nil {
				degraded = append(degraded, "embedding_retrieval_failed: "+err.Error())
			} else {
				vectorResults := documentsToResults(docs, "embedding")
				candidateCounts[CandidateCountSourceEmbeddingDocs] += len(vectorResults)
				rankedLists = append(rankedLists, vectorResults)
			}
		} else {
			degraded = append(degraded, "embedding_retriever_unavailable")
		}

		if h.config.BM25Enabled && h.bm25Index != nil {
			results, err := h.bm25Index.Search(ctx, variant, h.config.BM25TopK)
			if err != nil {
				degraded = append(degraded, "bm25_retrieval_failed: "+err.Error())
			} else {
				candidateCounts[CandidateCountSourceBM25Docs] += len(results)
				rankedLists = append(rankedLists, results)
			}
		} else if h.config.BM25Enabled {
			degraded = append(degraded, "bm25_index_unavailable")
		}
	}
	candidateCounts[CandidateCountStageRankedLists] = len(rankedLists)

	fused := FuseRankedLists(rankedLists, h.config.FusionTopK, h.config.RRFK)
	candidateCounts[CandidateCountStageFusedDocs] = len(fused)
	if h.profile == ProfileOpsCase {
		fused = BoostOpsCaseResults(fused)
	}
	if h.config.RerankerEnabled && len(fused) > 0 {
		if h.reranker == nil {
			degraded = append(degraded, "reranker_unavailable")
			fused = limitResults(fused, finalTopK)
		} else {
			reranked, err := h.reranker.Rerank(ctx, query, fused, finalTopK)
			if err != nil {
				degraded = append(degraded, "reranker_failed: "+err.Error())
				fused = limitResults(fused, finalTopK)
			} else {
				fused = reranked
				candidateCounts[CandidateCountStageRerankedDocs] = len(reranked)
			}
		}
	} else {
		fused = limitResults(fused, finalTopK)
	}
	candidateCounts[CandidateCountStageFinalDocs] = len(fused)

	status := "success"
	if len(degraded) > 0 {
		status = "degraded"
	}
	if len(fused) == 0 && status == "success" {
		status = "empty"
	}
	return &RetrievedContext{
		Status:                status,
		Profile:               h.profile,
		Query:                 query,
		RewrittenQueries:      variants,
		RewriteConfidence:     rewrite.Confidence,
		NeedsClarification:    rewrite.NeedsClarification,
		ClarificationQuestion: rewrite.ClarificationQuestion,
		DegradedReasons:       compactReasons(degraded),
		LatencyMS:             float64(time.Since(start).Microseconds()) / 1000,
		CandidateCounts:       compactCandidateCounts(candidateCounts),
		Count:                 len(fused),
		Results:               fused,
	}, nil
}

func documentsToResults(docs []*schema.Document, source string) []RetrievedResult {
	out := make([]RetrievedResult, 0, len(docs))
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		content := documentContent(doc)
		if content == "" {
			continue
		}
		meta := cloneMeta(doc.MetaData)
		if _, ok := meta["chunk_id"]; !ok {
			if id := strings.TrimSpace(doc.ID); id != "" {
				meta["chunk_id"] = id
			}
		}
		if _, ok := meta["content_hash"]; !ok && content != "" {
			meta["content_hash"] = ContentHash(content)
		}
		score := doc.Score()
		out = append(out, RetrievedResult{
			ID:            doc.ID,
			Content:       content,
			Score:         score,
			VectorScore:   score,
			Source:        source,
			RetrievalPath: retrievalPathFromSource(source),
			Meta:          meta,
		})
	}
	return out
}

func documentContent(doc *schema.Document) string {
	if doc == nil {
		return ""
	}
	if content := strings.TrimSpace(doc.Content); content != "" {
		return content
	}
	for _, key := range []string{"content", "text", "chunk", "case", "summary", "description"} {
		if doc.MetaData == nil {
			break
		}
		value := strings.TrimSpace(fmt.Sprintf("%v", doc.MetaData[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func compactReasons(reasons []string) []string {
	if len(reasons) == 0 {
		return nil
	}
	out := make([]string, 0, len(reasons))
	seen := map[string]struct{}{}
	for _, reason := range reasons {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			continue
		}
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		out = append(out, reason)
	}
	return out
}

func compactCandidateCounts(counts map[string]int) map[string]int {
	if len(counts) == 0 {
		return nil
	}
	out := make(map[string]int, len(counts))
	for key, value := range counts {
		key = strings.TrimSpace(key)
		if key == "" || value < 0 {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
