package rag

import "time"

type RetrievalProfile string

const (
	ProfileKnowledge RetrievalProfile = "knowledge"
	ProfileOpsCase   RetrievalProfile = "ops_case"
)

type DocumentChunk struct {
	ID          string         `json:"id"`
	DocID       string         `json:"doc_id,omitempty"`
	ChunkID     string         `json:"chunk_id,omitempty"`
	SourceType  string         `json:"source_type,omitempty"`
	Content     string         `json:"content"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	UpdatedAt   time.Time      `json:"updated_at,omitempty"`
	ContentHash string         `json:"content_hash,omitempty"`
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
	Count                 int               `json:"count"`
	Results               []RetrievedResult `json:"results"`
}
