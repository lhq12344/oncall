package chat_pipeline

import (
	"context"
	retriever2 "go_agent/internal/ai/retriever"

	"github.com/cloudwego/eino/components/retriever"
)

func newRetriever(ctx context.Context) (rtr retriever.Retriever, err error) {
	return retriever2.NewMilvusRetriever(ctx)
}
