package rag

import (
	"strings"
	"time"
)

type RetrievalLatency struct {
	RewriteMS  float64 `json:"rewrite_ms,omitempty"`
	RetrieveMS float64 `json:"retrieve_ms,omitempty"`
	FuseMS     float64 `json:"fuse_ms,omitempty"`
	RerankMS   float64 `json:"rerank_ms,omitempty"`
	QualityMS  float64 `json:"quality_ms,omitempty"`
}

type RetrievalCandidate struct {
	ID              string         `json:"id"`
	Content         string         `json:"content"`
	Source          string         `json:"source"`
	Score           float64        `json:"score"`
	FusionRank      int            `json:"fusion_rank,omitempty"`
	RerankerVersion string         `json:"reranker_version,omitempty"`
	RerankerScore   float64        `json:"reranker_score,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type Evidence struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	Scope     string    `json:"scope,omitempty"`
	Freshness string    `json:"freshness,omitempty"`
	Citation  string    `json:"citation,omitempty"`
}

type RetrievalSnapshot struct {
	SnapshotID       string               `json:"snapshot_id"`
	IndexVersion     string               `json:"index_version"`
	Query            string               `json:"query"`
	RewrittenQueries []string             `json:"rewritten_queries"`
	Filters          map[string]any       `json:"filters,omitempty"`
	Candidates       []RetrievalCandidate `json:"candidates,omitempty"`
	Fused            []RetrievalCandidate `json:"fused,omitempty"`
	Reranked         []RetrievalCandidate `json:"reranked,omitempty"`
	FinalEvidence    []Evidence           `json:"final_evidence,omitempty"`
	DegradedReasons  []string             `json:"degraded_reasons,omitempty"`
	Latency          RetrievalLatency     `json:"latency"`
}

func NewRetrievalSnapshot(indexVersion, query string, rewrites []string, evidence []Evidence) RetrievalSnapshot {
	variants := NormalizeQueryVariants(query, rewrites, 3)
	return RetrievalSnapshot{SnapshotID: ContentHash(indexVersion + "\n" + query + "\n" + strings.Join(variants, "\n")), IndexVersion: indexVersion, Query: query, RewrittenQueries: variants, FinalEvidence: evidence}
}
