package retriever

import (
	"context"
	"fmt"
	"go_agent/internal/ai/embedder"
	clientutil "go_agent/utility/client"
	"go_agent/utility/common"
	"strings"

	"github.com/cloudwego/eino-ext/components/retriever/milvus"
	"github.com/cloudwego/eino/components/retriever"
	milvusclient "github.com/milvus-io/milvus-sdk-go/v2/client"
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
	return NewMilvusRetrieverWithCollection(ctx, common.LoadMilvusConfig(ctx).Collection)
}

func NewMilvusRetrieverWithCollection(ctx context.Context, collection string) (rtr retriever.Retriever, err error) {
	cli, err := clientutil.NewMilvusClient(ctx)
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

	collection = strings.TrimSpace(collection)
	if collection == "" {
		collection = common.LoadMilvusConfig(ctx).Collection
	}

	if err := cli.LoadCollection(ctx, collection, false); err != nil {
		return nil, fmt.Errorf("failed to load milvus collection %s: %w", collection, err)
	}

	outputFields, err := resolveOutputFields(ctx, cli, collection)
	if err != nil {
		return nil, err
	}
	if collection == common.MilvusOpsCollection {
		// ops_cases 在当前 Milvus 环境下 Search 不返回字段数据，显式请求字段会报：
		// extra output fields [content metadata] found and result does not dynamic field
		// 这里先不请求输出字段，避免检索阶段直接失败。
		outputFields = []string{}
	}

	r, err := milvus.NewRetriever(ctx, &milvus.RetrieverConfig{
		Client:         cli,
		Collection:     collection,
		VectorField:    "vector",
		OutputFields:   outputFields,
		MetricType:     entity.COSINE,
		TopK:           3,
		ScoreThreshold: 0.8,
		Sp:             sp,

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

func resolveOutputFields(ctx context.Context, cli milvusclient.Client, collection string) ([]string, error) {
	collectionInfo, err := cli.DescribeCollection(ctx, collection)
	if err != nil {
		return nil, fmt.Errorf("failed to describe milvus collection %s: %w", collection, err)
	}
	if collectionInfo == nil || collectionInfo.Schema == nil {
		return []string{}, nil
	}

	exists := make(map[string]struct{}, len(collectionInfo.Schema.Fields))
	for _, field := range collectionInfo.Schema.Fields {
		if field == nil {
			continue
		}
		exists[field.Name] = struct{}{}
	}

	fields := make([]string, 0, 2)
	if _, ok := exists["content"]; ok {
		fields = append(fields, "content")
	}
	if _, ok := exists["metadata"]; ok {
		fields = append(fields, "metadata")
	}
	return fields, nil
}
