package retriever

import (
	"context"
	"encoding/json"
	"fmt"
	clientutil "go_agent/internal/logic/ai/client"
	"go_agent/internal/logic/ai/common"
	"go_agent/internal/logic/ai/embedder"
	"strings"

	"github.com/cloudwego/eino-ext/components/retriever/milvus"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
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

	const (
		defaultTopK           = 3
		defaultScoreThreshold = 0.8
	)

	// 使用 AUTOINDEX 搜索参数，并显式设置 range_filter。
	// 说明：eino-ext retriever 仅从 Sp.Params()["range_filter"] 读取实际阈值；
	// 若未设置该参数，ScoreThreshold 会被重置为 0（等同无阈值过滤）。
	sp, err := entity.NewIndexAUTOINDEXSearchParam(1)
	if err != nil {
		return nil, err
	}
	sp.AddRangeFilter(defaultScoreThreshold)

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
		Client:            cli,
		Collection:        collection,
		VectorField:       "vector",
		OutputFields:      outputFields,
		DocumentConverter: documentConverterWithScore,
		MetricType:        entity.COSINE,
		TopK:              defaultTopK,
		ScoreThreshold:    defaultScoreThreshold,
		Sp:                sp,

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

func documentConverterWithScore(_ context.Context, result milvusclient.SearchResult) ([]*schema.Document, error) {
	count := result.IDs.Len()
	docs := make([]*schema.Document, count)
	for i := 0; i < count; i++ {
		docs[i] = &schema.Document{
			MetaData: make(map[string]any),
		}
		if i < len(result.Scores) {
			docs[i].WithScore(float64(result.Scores[i]))
		}
	}

	for _, field := range result.Fields {
		switch field.Name() {
		case "id":
			for i := range docs {
				id, err := result.IDs.GetAsString(i)
				if err != nil {
					return nil, fmt.Errorf("failed to get id: %w", err)
				}
				docs[i].ID = id
			}
		case "content":
			for i := range docs {
				content, err := field.GetAsString(i)
				if err != nil {
					return nil, fmt.Errorf("failed to get content: %w", err)
				}
				docs[i].Content = content
			}
		case "metadata":
			for i := range docs {
				raw, err := field.Get(i)
				if err != nil {
					return nil, fmt.Errorf("failed to get metadata: %w", err)
				}
				bytes, ok := raw.([]byte)
				if !ok {
					docs[i].MetaData[field.Name()] = raw
					continue
				}
				if err := json.Unmarshal(bytes, &docs[i].MetaData); err != nil {
					return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
				}
			}
		default:
			for i := range docs {
				value, err := field.Get(i)
				if err != nil {
					return nil, fmt.Errorf("failed to get field %s: %w", field.Name(), err)
				}
				docs[i].MetaData[field.Name()] = value
			}
		}
	}

	return docs, nil
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
