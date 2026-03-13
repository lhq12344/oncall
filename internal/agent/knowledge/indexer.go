package knowledge

import (
	"context"
	"fmt"
	"strings"

	aiindexer "go_agent/internal/ai/indexer"

	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/schema"
)

type IndexerImpl struct {
	inner indexer.Indexer
}

// newIndexer creates milvus-backed indexer for knowledge documents.
func newIndexer(ctx context.Context) (idr indexer.Indexer, err error) {
	milvusIndexer, err := aiindexer.NewMilvusIndexer(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create milvus indexer: %w", err)
	}
	idr = &IndexerImpl{inner: milvusIndexer}
	return idr, nil
}

func (impl *IndexerImpl) Store(ctx context.Context, docs []*schema.Document, opts ...indexer.Option) ([]string, error) {
	if impl.inner == nil {
		return nil, fmt.Errorf("knowledge indexer unavailable")
	}
	assignChunkDocumentIDs(docs)
	return impl.inner.Store(ctx, docs, opts...)
}

// assignChunkDocumentIDs 为分片文档生成稳定且唯一的主键 ID。
// 输入：分片后的文档列表（docs）。
// 输出：原地更新 docs[i].ID，规则为「分片标题 + 原始ID + 分片序号」。
func assignChunkDocumentIDs(docs []*schema.Document) {
	if len(docs) == 0 {
		return
	}

	exists := make(map[string]struct{}, len(docs))
	for index, doc := range docs {
		if doc == nil {
			continue
		}

		chunkTitle := extractChunkTitle(doc)
		originID := sanitizeIDSegment(doc.ID)
		if originID == "" {
			originID = "doc"
		}

		newID := fmt.Sprintf("%s_%s_%d", chunkTitle, originID, index+1)
		// 防御式去重（理论上 index 已保证唯一）
		for suffix := 1; ; suffix++ {
			if _, ok := exists[newID]; !ok {
				break
			}
			newID = fmt.Sprintf("%s_%d", newID, suffix)
		}

		doc.ID = newID
		exists[newID] = struct{}{}
	}
}

// extractChunkTitle 从文档 metadata 中提取分片标题。
// 输入：单个文档 doc。
// 输出：用于 ID 的标题片段，优先 h1/title，兜底 chunk。
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

// sanitizeIDSegment 清理 ID 片段，仅保留字母数字、下划线和短横线。
// 输入：任意字符串 raw。
// 输出：清理后的字符串，若为空则返回空字符串。
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
