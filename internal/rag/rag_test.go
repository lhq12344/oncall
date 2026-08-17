package rag

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
)

func TestFuseRankedListsDedupeByChunkIDAndRRF(t *testing.T) {
	lists := [][]RetrievedResult{
		{
			{ID: "a", Content: "alpha", VectorScore: 0.9, Source: "embedding", Meta: map[string]any{"chunk_id": "a"}},
			{ID: "b1", Content: "bravo vector", VectorScore: 0.8, Source: "embedding", Meta: map[string]any{"chunk_id": "b"}},
		},
		{
			{ID: "b2", Content: "bravo bm25", BM25Score: 3.0, Source: "bm25", Meta: map[string]any{"chunk_id": "b"}},
			{ID: "c", Content: "charlie", BM25Score: 2.0, Source: "bm25", Meta: map[string]any{"chunk_id": "c"}},
		},
	}
	got := FuseRankedLists(lists, 3, 60)
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	if got[0].Meta["chunk_id"] != "b" {
		t.Fatalf("top chunk=%v want b; got=%#v", got[0].Meta["chunk_id"], got)
	}
	if !strings.Contains(got[0].Source, "bm25") {
		t.Fatalf("merged source missing bm25: %#v", got[0])
	}
	for _, want := range []string{"embedding", "bm25", "rrf"} {
		if !containsString(got[0].RetrievalPath, want) {
			t.Fatalf("retrieval path missing %q: %#v", want, got[0].RetrievalPath)
		}
	}
}

func TestFileBM25IndexSearchAndReload(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/knowledge.jsonl"
	idx, err := NewFileBM25Index(path)
	if err != nil {
		t.Fatal(err)
	}
	err = idx.Upsert(ctx, []DocumentChunk{
		{ID: "1", ChunkID: "1", Content: "redis connection timeout root cause", Metadata: map[string]any{"service": "redis"}},
		{ID: "2", ChunkID: "2", Content: "kubernetes image pull backoff", Metadata: map[string]any{"service": "kubelet"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewFileBM25Index(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.Search(ctx, "redis timeout", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("got %#v, want doc 1", got)
	}
	if got[0].BM25Score <= 0 {
		t.Fatalf("bm25 score not populated: %#v", got[0])
	}
	if !containsString(got[0].RetrievalPath, "bm25") {
		t.Fatalf("bm25 retrieval path not populated: %#v", got[0])
	}
}

func TestTokenizeMixedChineseEnglish(t *testing.T) {
	got := Tokenize("Redis 连接超时 pod-1")
	want := []string{"redis", "连", "接", "超", "时", "pod", "1"}
	if len(got) != len(want) {
		t.Fatalf("tokens=%#v want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tokens=%#v want %#v", got, want)
		}
	}
}

func TestParseRewriteResultFallbackAndLimit(t *testing.T) {
	bad := ParseRewriteResult("{not-json", "original")
	if len(bad.RewrittenQueries) != 1 || bad.RewrittenQueries[0] != "original" {
		t.Fatalf("bad fallback=%#v", bad)
	}
	good := ParseRewriteResult("{\"rewritten_queries\":[\"original\",\"a\",\"b\",\"c\"],\"confidence\":2}", "original")
	if len(good.RewrittenQueries) != 3 {
		t.Fatalf("query variants=%#v", good.RewrittenQueries)
	}
	if good.Confidence != 1 {
		t.Fatalf("confidence=%v want 1", good.Confidence)
	}
}

func TestBuildRewriteInputUsesSummaryAndRecentTurns(t *testing.T) {
	input := BuildRewriteInput("current", []*schema.Message{
		schema.SystemMessage("older summary"),
		schema.UserMessage("first"),
		schema.AssistantMessage("second", nil),
		schema.UserMessage("third"),
		schema.AssistantMessage("fourth", nil),
		schema.UserMessage("fifth"),
		schema.UserMessage("current"),
	})
	if input.SessionSummary != "older summary" {
		t.Fatalf("summary=%q", input.SessionSummary)
	}
	if len(input.RecentTurns) != 4 {
		t.Fatalf("recent turns=%#v", input.RecentTurns)
	}
	if strings.Contains(strings.Join(input.RecentTurns, "\n"), "current") {
		t.Fatalf("current query should not be duplicated in recent turns: %#v", input.RecentTurns)
	}
}

func TestChatModelRewriterParsesModelJSON(t *testing.T) {
	rewriter := ChatModelRewriter{
		Model:       fakeRewriteModel{content: "prefix {\"rewritten_queries\":[\"redis timeout runbook\",\"redis connection failed\"],\"confidence\":0.7} suffix"},
		MaxRewrites: 2,
	}
	got, err := rewriter.Rewrite(context.Background(), RewriteInput{Query: "redis timeout", SessionSummary: "redis incident", RecentTurns: []string{"user: redis down"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Confidence != 0.7 {
		t.Fatalf("confidence=%v", got.Confidence)
	}
	if len(got.RewrittenQueries) != 3 || got.RewrittenQueries[0] != "redis timeout" {
		t.Fatalf("rewrites=%#v", got.RewrittenQueries)
	}
}

func TestConfigCapFinalTopK(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.RewriteEnabled {
		t.Fatal("rewrite should be enabled by default")
	}
	if got := cfg.CapFinalTopK(0); got != 3 {
		t.Fatalf("default topK=%d", got)
	}
	if got := cfg.CapFinalTopK(99); got != 10 {
		t.Fatalf("max topK=%d", got)
	}
}

func TestNoopRerankerPreservesOrder(t *testing.T) {
	got, err := NoopReranker{}.Rerank(nil, "q", []RetrievedResult{{ID: "a"}, {ID: "b"}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("got %#v", got)
	}
	if !containsString(got[0].RetrievalPath, "rerank") {
		t.Fatalf("noop reranker did not mark retrieval path: %#v", got[0])
	}
}

func TestHybridRetrieverDegradesWhenRerankerEnabledButUnavailable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BM25Enabled = false
	cfg.RerankerEnabled = true
	doc := (&schema.Document{ID: "vec", Content: "redis timeout", MetaData: map[string]any{"chunk_id": "vec"}}).WithScore(0.9)
	h := NewHybridRetriever(HybridRetrieverConfig{
		Profile:         ProfileKnowledge,
		Config:          cfg,
		VectorRetriever: fakeRetriever{docs: []*schema.Document{doc}},
	})
	got, err := h.RetrieveContext(context.Background(), "redis timeout", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "degraded" || !containsString(got.DegradedReasons, "reranker_unavailable") {
		t.Fatalf("expected reranker_unavailable degradation, got %#v", got)
	}
	if len(got.Results) != 1 {
		t.Fatalf("results=%#v", got.Results)
	}
	if containsString(got.Results[0].RetrievalPath, "rerank") {
		t.Fatalf("unavailable reranker should not mark rerank path: %#v", got.Results[0].RetrievalPath)
	}
}

func TestHTTPRerankerFallbackIgnoresMissingIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\"results\":[{\"id\":\"missing\",\"score\":9},{\"id\":\"b\",\"score\":0.9}]}"))
	}))
	defer server.Close()
	got, err := NewHTTPReranker(server.URL, 0).Rerank(context.Background(), "q", []RetrievedResult{{ID: "a", Score: 0.1}, {ID: "b", Score: 0.2}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "a" {
		t.Fatalf("got %#v", got)
	}
}

type fakeRetriever struct {
	docs []*schema.Document
	err  error
}

func (f fakeRetriever) Retrieve(ctx context.Context, query string, opts ...einoretriever.Option) ([]*schema.Document, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.docs, nil
}

func TestHybridRetrieverFallsBackToLegacyAndBM25(t *testing.T) {
	ctx := context.Background()
	bm25, err := NewFileBM25Index(t.TempDir() + "/idx.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := bm25.Upsert(ctx, []DocumentChunk{{ID: "bm", ChunkID: "bm", Content: "redis timeout bm25"}}); err != nil {
		t.Fatal(err)
	}
	legacyDoc := (&schema.Document{ID: "legacy", Content: "redis timeout legacy", MetaData: map[string]any{"chunk_id": "legacy"}}).WithScore(0.8)
	h := NewHybridRetriever(HybridRetrieverConfig{
		Profile:         ProfileKnowledge,
		Config:          DefaultConfig(),
		VectorRetriever: fakeRetriever{err: errors.New("primary down")},
		LegacyRetriever: fakeRetriever{docs: []*schema.Document{legacyDoc}},
		BM25Index:       bm25,
	})
	got, err := h.RetrieveContext(ctx, "redis timeout", 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "degraded" {
		t.Fatalf("status=%s want degraded", got.Status)
	}
	if len(got.Results) == 0 {
		t.Fatalf("expected fused fallback results")
	}
}

func TestDocumentsToResultsUsesMetadataContentFallback(t *testing.T) {
	docs := []*schema.Document{{
		ID:       "meta-doc",
		MetaData: map[string]any{"content": "metadata content", "chunk_id": "meta-doc"},
	}}
	got := documentsToResults(docs, "embedding")
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
	if got[0].Content != "metadata content" {
		t.Fatalf("content=%q", got[0].Content)
	}
	if !containsString(got[0].RetrievalPath, "embedding") {
		t.Fatalf("retrieval path=%#v", got[0].RetrievalPath)
	}
}

type fakeRewriteModel struct {
	content string
	err     error
}

func (f fakeRewriteModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	return schema.AssistantMessage(f.content, nil), nil
}

func (f fakeRewriteModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("unused")
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
