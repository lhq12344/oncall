package retriever

import (
	"context"
	"fmt"
	"go_agent/internal/ai/embedder"
	"go_agent/utility/client"
	"go_agent/utility/common"

	"github.com/cloudwego/eino-ext/components/retriever/milvus"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

func f64ToF32(v []float64) []float32 {
	out := make([]float32, len(v))
	for i := range v {
		out[i] = float32(v[i])
	}
	return out
}

func NewMilvusRetriever(ctx context.Context) (rtr retriever.Retriever, err error) {
	cli, err := client.NewMilvusClient(ctx)
	if err != nil {
		return nil, err
	}
	eb, err := embedder.DoubaoEmbedding(ctx)
	if err != nil {
		return nil, err
	}

	// 用默认的 AUTOINDEX 搜索参数（不要 AddRadius / AddRangeFilter）
	sp, err := entity.NewIndexAUTOINDEXSearchParam(1)
	if err != nil {
		return nil, err
	}

	r, err := milvus.NewRetriever(ctx, &milvus.RetrieverConfig{
		Client:      cli,
		Collection:  common.MilvusCollectionName,
		VectorField: "vector",
		// 必须显式返回 content / metadata，否则默认文档会出现空内容。
		OutputFields: []string{
			"content",
			"metadata",
		},
		MetricType:     entity.COSINE,
		TopK:           3,
		ScoreThreshold: 0.8, // 关键：必须为 0（否则可能走 range search）
		Sp:             sp,  // 关键：不要带 radius/range_filter

		Embedding: eb,

		VectorConverter: func(ctx context.Context, vectors [][]float64) ([]entity.Vector, error) {
			out := make([]entity.Vector, len(vectors))
			for i, v := range vectors {
				if len(v) == 0 {
					return nil, fmt.Errorf("empty embedding at index=%d", i)
				}
				out[i] = entity.FloatVector(f64ToF32(v))
			}
			return out, nil
		},
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}
