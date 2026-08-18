package knowledge

import (
	"context"

	aiembedder "go_agent/internal/ai/embedder"

	"github.com/cloudwego/eino/components/embedding"
)

// newEmbedding creates embedding component used by knowledge indexing.
func newEmbedding(ctx context.Context) (eb embedding.Embedder, err error) {
	eb, err = aiembedder.DoubaoEmbedding(ctx)
	if err != nil {
		return nil, err
	}
	return eb, nil
}
