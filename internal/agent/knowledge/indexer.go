package knowledge

import (
	"context"
	"fmt"

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
	return impl.inner.Store(ctx, docs, opts...)
}
