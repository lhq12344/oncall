package rag

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type BM25Index interface {
	Upsert(ctx context.Context, docs []DocumentChunk) error
	Delete(ctx context.Context, ids []string) error
	Search(ctx context.Context, query string, topK int) ([]RetrievedResult, error)
	Rebuild(ctx context.Context, docs []DocumentChunk) error
}

type FileBM25Index struct {
	mu   sync.RWMutex
	path string
	docs map[string]DocumentChunk
}

func NewFileBM25Index(path string) (*FileBM25Index, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("bm25 index path is required")
	}
	idx := &FileBM25Index{path: path, docs: map[string]DocumentChunk{}}
	if err := idx.load(); err != nil {
		return nil, err
	}
	return idx, nil
}

func NewProfileBM25Index(root string, profile RetrievalProfile) (*FileBM25Index, error) {
	if strings.TrimSpace(root) == "" {
		root = DefaultConfig().BM25Root
	}
	return NewFileBM25Index(filepath.Join(root, string(profile)+".jsonl"))
}

func (i *FileBM25Index) Upsert(ctx context.Context, docs []DocumentChunk) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, doc := range docs {
		doc.ID = strings.TrimSpace(doc.ID)
		if doc.ID == "" {
			doc.ID = firstNonEmpty(doc.ChunkID, doc.ContentHash, ContentHash(doc.Content))
		}
		if doc.ContentHash == "" {
			doc.ContentHash = ContentHash(doc.Content)
		}
		if doc.ChunkID == "" {
			doc.ChunkID = doc.ID
		}
		i.docs[doc.ID] = doc
	}
	return i.persistLocked()
}

func (i *FileBM25Index) Delete(ctx context.Context, ids []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, id := range ids {
		delete(i.docs, strings.TrimSpace(id))
	}
	return i.persistLocked()
}

func (i *FileBM25Index) Rebuild(ctx context.Context, docs []DocumentChunk) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.docs = map[string]DocumentChunk{}
	for _, doc := range docs {
		doc.ID = strings.TrimSpace(doc.ID)
		if doc.ID == "" {
			doc.ID = firstNonEmpty(doc.ChunkID, doc.ContentHash, ContentHash(doc.Content))
		}
		if doc.ContentHash == "" {
			doc.ContentHash = ContentHash(doc.Content)
		}
		if doc.ChunkID == "" {
			doc.ChunkID = doc.ID
		}
		i.docs[doc.ID] = doc
	}
	return i.persistLocked()
}

func (i *FileBM25Index) Search(ctx context.Context, query string, topK int) ([]RetrievedResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	queryTokens := Tokenize(query)
	if len(queryTokens) == 0 {
		return nil, nil
	}
	if topK <= 0 {
		topK = DefaultConfig().BM25TopK
	}

	i.mu.RLock()
	docs := make([]DocumentChunk, 0, len(i.docs))
	for _, doc := range i.docs {
		docs = append(docs, doc)
	}
	i.mu.RUnlock()
	if len(docs) == 0 {
		return nil, nil
	}

	docTokens := make([][]string, len(docs))
	docFreq := map[string]int{}
	totalLen := 0
	for idx, doc := range docs {
		tokens := Tokenize(doc.Content + " " + metadataText(doc.Metadata))
		docTokens[idx] = tokens
		totalLen += len(tokens)
		seen := map[string]struct{}{}
		for _, token := range tokens {
			if _, ok := seen[token]; ok {
				continue
			}
			seen[token] = struct{}{}
			docFreq[token]++
		}
	}
	avgLen := float64(totalLen) / float64(len(docs))
	if avgLen == 0 {
		avgLen = 1
	}

	results := make([]RetrievedResult, 0, len(docs))
	for idx, doc := range docs {
		score := bm25Score(queryTokens, docTokens[idx], docFreq, len(docs), avgLen)
		if score <= 0 {
			continue
		}
		meta := cloneMeta(doc.Metadata)
		meta["doc_id"] = firstNonEmpty(doc.DocID, fmt.Sprint(meta["doc_id"]))
		meta["chunk_id"] = firstNonEmpty(doc.ChunkID, fmt.Sprint(meta["chunk_id"]))
		meta["content_hash"] = firstNonEmpty(doc.ContentHash, fmt.Sprint(meta["content_hash"]))
		results = append(results, RetrievedResult{
			ID:            firstNonEmpty(doc.ID, doc.ChunkID, doc.ContentHash),
			Content:       doc.Content,
			Score:         score,
			BM25Score:     score,
			Source:        "bm25",
			RetrievalPath: []string{"bm25"},
			Meta:          meta,
		})
	}
	sort.SliceStable(results, func(a, b int) bool {
		if results[a].Score == results[b].Score {
			return results[a].ID < results[b].ID
		}
		return results[a].Score > results[b].Score
	})
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func (i *FileBM25Index) load() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.docs = map[string]DocumentChunk{}
	file, err := os.Open(i.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load bm25 index %s: %w", i.path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var doc DocumentChunk
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			return fmt.Errorf("decode bm25 index %s: %w", i.path, err)
		}
		doc.ID = firstNonEmpty(doc.ID, doc.ChunkID, doc.ContentHash)
		if doc.ID != "" {
			i.docs[doc.ID] = doc
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan bm25 index %s: %w", i.path, err)
	}
	return nil
}

func (i *FileBM25Index) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(i.path), 0o755); err != nil {
		return fmt.Errorf("create bm25 index dir: %w", err)
	}
	tmp := i.path + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create bm25 index temp file: %w", err)
	}
	enc := json.NewEncoder(file)
	keys := make([]string, 0, len(i.docs))
	for key := range i.docs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := enc.Encode(i.docs[key]); err != nil {
			_ = file.Close()
			_ = os.Remove(tmp)
			return fmt.Errorf("write bm25 index: %w", err)
		}
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, i.path)
}

func bm25Score(queryTokens, docTokens []string, docFreq map[string]int, docCount int, avgLen float64) float64 {
	if len(docTokens) == 0 || docCount == 0 {
		return 0
	}
	tf := map[string]int{}
	for _, token := range docTokens {
		tf[token]++
	}
	const k1 = 1.5
	const b = 0.75
	score := 0.0
	for _, token := range queryTokens {
		freq := tf[token]
		if freq == 0 {
			continue
		}
		df := docFreq[token]
		idf := math.Log(1 + (float64(docCount-df)+0.5)/(float64(df)+0.5))
		numer := float64(freq) * (k1 + 1)
		denom := float64(freq) + k1*(1-b+b*float64(len(docTokens))/avgLen)
		score += idf * numer / denom
	}
	return score
}

func metadataText(meta map[string]any) string {
	if len(meta) == 0 {
		return ""
	}
	keys := make([]string, 0, len(meta))
	for key := range meta {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%v", meta[key]))
	}
	return strings.Join(parts, " ")
}
