package rag

import "time"

type RetrievalProfile string

const (
	ProfileKnowledge RetrievalProfile = "knowledge"
	ProfileOpsCase   RetrievalProfile = "ops_case"
)

type DocumentChunk struct {
	ID               string         `json:"id"`
	DocID            string         `json:"doc_id,omitempty"`
	ChunkID          string         `json:"chunk_id,omitempty"`
	ParentID         string         `json:"parent_id,omitempty"`
	SourceType       string         `json:"source_type,omitempty"`
	Tenant           string         `json:"tenant,omitempty"`
	Project          string         `json:"project,omitempty"`
	Cluster          string         `json:"cluster,omitempty"`
	Service          string         `json:"service,omitempty"`
	Namespace        string         `json:"namespace,omitempty"`
	Content          string         `json:"content"`
	ContextualPrefix string         `json:"contextual_prefix,omitempty"`
	PropositionType  string         `json:"proposition_type,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	SourceURI        string         `json:"source_uri,omitempty"`
	Owner            string         `json:"owner,omitempty"`
	Version          string         `json:"version,omitempty"`
	UpdatedAt        time.Time      `json:"updated_at,omitempty"`
	ValidFrom        time.Time      `json:"valid_from,omitempty"`
	ValidUntil       time.Time      `json:"valid_until,omitempty"`
	ContentHash      string         `json:"content_hash,omitempty"`
	ChunkProfile     string         `json:"chunk_profile,omitempty"`
	IndexVersion     string         `json:"index_version,omitempty"`
}

type RetrievedResult struct {
	ID            string         `json:"id,omitempty"`
	Content       string         `json:"content"`
	Score         float64        `json:"score"`
	RRFScore      float64        `json:"rrf_score,omitempty"`
	VectorScore   float64        `json:"vector_score,omitempty"`
	BM25Score     float64        `json:"bm25_score,omitempty"`
	Source        string         `json:"source,omitempty"`
	RetrievalPath []string       `json:"retrieval_path,omitempty"`
	Meta          map[string]any `json:"meta,omitempty"`
}

type RetrievedContext struct {
	Status                string            `json:"status"`
	Profile               RetrievalProfile  `json:"profile"`
	Query                 string            `json:"query"`
	RewrittenQueries      []string          `json:"rewritten_queries"`
	RewriteConfidence     float64           `json:"rewrite_confidence"`
	NeedsClarification    bool              `json:"needs_clarification"`
	ClarificationQuestion string            `json:"clarification_question,omitempty"`
	DegradedReasons       []string          `json:"degraded_reasons,omitempty"`
	LatencyMS             float64           `json:"latency_ms,omitempty"`
	CandidateCounts       map[string]int    `json:"candidate_counts,omitempty"`
	Count                 int               `json:"count"`
	Results               []RetrievedResult `json:"results"`
}

const (
	CandidateCountStageQueryVariants = "stage.query_variants"
	CandidateCountStageRankedLists   = "stage.ranked_lists"
	CandidateCountStageFusedDocs     = "stage.fused_docs"
	CandidateCountStageRerankedDocs  = "stage.reranked_docs"
	CandidateCountStageFinalDocs     = "stage.final_docs"

	CandidateCountSourceEmbeddingDocs        = "source.embedding_docs"
	CandidateCountSourceBM25Docs             = "source.bm25_docs"
	CandidateCountSourceLocalFinalReportDocs = "source.local_final_report_docs"
)
