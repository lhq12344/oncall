package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type Reranker interface {
	Rerank(ctx context.Context, query string, docs []RetrievedResult, topK int) ([]RetrievedResult, error)
}

type NoopReranker struct{}

func (NoopReranker) Rerank(ctx context.Context, query string, docs []RetrievedResult, topK int) ([]RetrievedResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := limitResults(docs, topK)
	for i := range out {
		out[i].RetrievalPath = appendRetrievalPath(out[i].RetrievalPath, "rerank")
	}
	return out, nil
}

type HTTPReranker struct {
	URL     string
	Client  *http.Client
	Timeout time.Duration
}

func NewHTTPReranker(url string, timeout time.Duration) *HTTPReranker {
	if timeout <= 0 {
		timeout = DefaultConfig().RerankerTimeout
	}
	return &HTTPReranker{
		URL:     strings.TrimRight(strings.TrimSpace(url), "/"),
		Client:  &http.Client{Timeout: timeout},
		Timeout: timeout,
	}
}

func (r *HTTPReranker) Rerank(ctx context.Context, query string, docs []RetrievedResult, topK int) ([]RetrievedResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || strings.TrimSpace(r.URL) == "" {
		return limitResults(docs, topK), nil
	}
	if len(docs) == 0 {
		return nil, nil
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultConfig().RerankerTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type docReq struct {
		ID       string         `json:"id"`
		Content  string         `json:"content"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}
	reqBody := struct {
		Query     string   `json:"query"`
		Documents []docReq `json:"documents"`
		TopK      int      `json:"top_k"`
	}{Query: query, TopK: topK}
	for _, doc := range docs {
		reqBody.Documents = append(reqBody.Documents, docReq{ID: doc.ID, Content: doc.Content, Metadata: doc.Meta})
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(runCtx, http.MethodPost, r.URL+"/rerank", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("reranker returned status %d", resp.StatusCode)
	}
	var decoded struct {
		Results []struct {
			ID    string  `json:"id"`
			Score float64 `json:"score"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	byID := make(map[string]int, len(docs))
	for idx, doc := range docs {
		byID[doc.ID] = idx
	}
	out := make([]RetrievedResult, 0, len(docs))
	used := map[int]struct{}{}
	for _, item := range decoded.Results {
		idx, ok := byID[item.ID]
		if !ok {
			continue
		}
		doc := docs[idx]
		doc.Score = item.Score
		doc.RetrievalPath = appendRetrievalPath(doc.RetrievalPath, "rerank")
		doc.Meta = cloneMeta(doc.Meta)
		doc.Meta["reranker_score"] = item.Score
		out = append(out, doc)
		used[idx] = struct{}{}
	}
	for idx, doc := range docs {
		if _, ok := used[idx]; !ok {
			doc.RetrievalPath = appendRetrievalPath(doc.RetrievalPath, "rerank")
			out = append(out, doc)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	return limitResults(out, topK), nil
}

func limitResults(docs []RetrievedResult, topK int) []RetrievedResult {
	if topK <= 0 || len(docs) <= topK {
		return docs
	}
	return docs[:topK]
}
