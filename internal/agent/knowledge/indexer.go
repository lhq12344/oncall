package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"

	aiindexer "go_agent/internal/ai/indexer"
	"go_agent/internal/rag"
	"go_agent/utility/common"

	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type IndexerImpl struct {
	inner  indexer.Indexer
	logger *zap.Logger
}

// newIndexer creates the Milvus-backed indexer for v2 knowledge chunks.
func newIndexer(ctx context.Context, logger ...*zap.Logger) (idr indexer.Indexer, err error) {
	milvusIndexer, err := aiindexer.NewMilvusIndexerWithCollection(ctx, common.LoadMilvusConfig(ctx).KnowledgeV2Collection)
	if err != nil {
		return nil, fmt.Errorf("failed to create milvus indexer: %w", err)
	}
	idr = &IndexerImpl{inner: milvusIndexer, logger: firstKnowledgeLogger(logger...)}
	return idr, nil
}

func (impl *IndexerImpl) Store(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) ([]string, error) {
	if impl.inner == nil {
		return nil, fmt.Errorf("knowledge indexer unavailable")
	}
	assignChunkDocumentIDs(docs)
	ids, err := impl.inner.Store(ctx, docs, opts...)
	if err != nil {
		return nil, err
	}
	if bm25Err := upsertKnowledgeBM25(ctx, docs); bm25Err != nil && impl.logger != nil {
		impl.logger.Warn("knowledge bm25 index upsert failed; milvus write remains authoritative",
			zap.Int("chunks", len(docs)),
			zap.Error(bm25Err))
	}
	return ids, nil
}

func firstKnowledgeLogger(loggers ...*zap.Logger) *zap.Logger {
	for _, logger := range loggers {
		if logger != nil {
			return logger
		}
	}
	return nil
}

func upsertKnowledgeBM25(ctx context.Context, docs []*schema.Document) error {
	config := rag.LoadConfig(ctx)
	if !config.BM25Enabled {
		return nil
	}
	idx, err := rag.NewProfileBM25Index(config.BM25Root, rag.ProfileKnowledge)
	if err != nil {
		return err
	}
	chunks := make([]rag.DocumentChunk, 0, len(docs))
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		chunks = append(chunks, rag.DocumentChunk{
			ID:          doc.ID,
			DocID:       strings.TrimSpace(fmt.Sprintf("%v", doc.MetaData["doc_id"])),
			ChunkID:     strings.TrimSpace(fmt.Sprintf("%v", doc.MetaData["chunk_id"])),
			SourceType:  strings.TrimSpace(fmt.Sprintf("%v", doc.MetaData["source_type"])),
			Content:     doc.Content,
			Metadata:    doc.MetaData,
			ContentHash: strings.TrimSpace(fmt.Sprintf("%v", doc.MetaData["content_hash"])),
		})
	}
	return idx.Upsert(ctx, chunks)
}

// assignChunkDocumentIDs generates stable v2 chunk metadata and Milvus primary keys.
func assignChunkDocumentIDs(docs []*schema.Document) {
	if len(docs) == 0 {
		return
	}

	exists := make(map[string]struct{}, len(docs))
	now := time.Now().UTC().Format(time.RFC3339)
	for index, doc := range docs {
		if doc == nil {
			continue
		}
		if doc.MetaData == nil {
			doc.MetaData = map[string]any{}
		}

		chunkTitle := extractChunkTitle(doc)
		originID := sanitizeIDSegment(doc.ID)
		if originID == "" {
			originID = "doc"
		}
		contentHash := rag.ContentHash(doc.Content)

		chunkID := strings.TrimSpace(fmt.Sprintf("%v", doc.MetaData["chunk_id"]))
		if chunkID == "" || chunkID == "<nil>" {
			chunkID = fmt.Sprintf("%s_%s_%d", chunkTitle, originID, index+1)
		}
		for suffix := 1; ; suffix++ {
			if _, ok := exists[chunkID]; !ok {
				break
			}
			chunkID = fmt.Sprintf("%s_%d", chunkID, suffix)
		}

		docID := strings.TrimSpace(fmt.Sprintf("%v", doc.MetaData["doc_id"]))
		if docID == "" || docID == "<nil>" {
			docID = originID
			if docID == "doc" && len(contentHash) >= 12 {
				docID = "doc_" + contentHash[:12]
			}
		}
		if _, ok := doc.MetaData["source_type"]; !ok {
			doc.MetaData["source_type"] = "knowledge"
		}
		doc.MetaData["doc_id"] = docID
		doc.MetaData["chunk_id"] = chunkID
		doc.MetaData["updated_at"] = now
		doc.MetaData["content_hash"] = contentHash

		doc.ID = chunkID
		exists[chunkID] = struct{}{}
	}
}

// extractChunkTitle extracts a safe title segment from document metadata.
func extractChunkTitle(doc *schema.Document) string {
	if doc == nil || doc.MetaData == nil {
		return "chunk"
	}

	for _, key := range []string{"h1", "title"} {
		value, ok := doc.MetaData[key]
		if !ok || value == nil {
			continue
		}
		title := sanitizeIDSegment(fmt.Sprintf("%v", value))
		if title != "" {
			return title
		}
	}
	return "chunk"
}

// sanitizeIDSegment keeps only ASCII letters, digits, underscores, and hyphens.
func sanitizeIDSegment(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	var result []rune
	for _, item := range trimmed {
		switch {
		case item >= 'a' && item <= 'z':
			result = append(result, item)
		case item >= 'A' && item <= 'Z':
			result = append(result, item)
		case item >= '0' && item <= '9':
			result = append(result, item)
		case item == '_' || item == '-':
			result = append(result, item)
		}
	}

	clean := strings.Trim(string(result), "_-")
	return clean
}
