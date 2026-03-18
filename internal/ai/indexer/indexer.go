package indexer

import (
	"context"
	"fmt"
	embedder2 "go_agent/internal/ai/embedder"
	"go_agent/utility/client"
	"go_agent/utility/common"
	"strings"

	"github.com/cloudwego/eino-ext/components/indexer/milvus"
	"github.com/cloudwego/eino/schema"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

func NewMilvusIndexer(ctx context.Context) (*milvus.Indexer, error) {
	return NewMilvusIndexerWithCollection(ctx, common.LoadMilvusConfig(ctx).Collection)
}

func NewMilvusIndexerWithCollection(ctx context.Context, collection string) (*milvus.Indexer, error) {
	cli, err := client.NewMilvusClient(ctx)
	if err != nil {
		return nil, err
	}

	eb, err := embedder2.DoubaoEmbedding(ctx)
	if err != nil {
		return nil, err
	}
	collection = strings.TrimSpace(collection)
	if collection == "" {
		collection = common.LoadMilvusConfig(ctx).Collection
	}

	config := &milvus.IndexerConfig{
		Client:            cli,
		Collection:        collection,
		Fields:            fields,
		Embedding:         eb,
		MetricType:        milvus.MetricType(entity.COSINE),
		DocumentConverter: docConverterFloatVector("vector"),
	}
	indexer, err := milvus.NewIndexer(ctx, config)
	if err != nil {
		return nil, err
	}
	return indexer, nil
}

func float64ToFloat32(v []float64) []float32 {
	out := make([]float32, len(v))
	for i := range v {
		out[i] = float32(v[i])
	}
	return out
}

func docConverterFloatVector(vectorField string) func(ctx context.Context, docs []*schema.Document, vectors [][]float64) ([]interface{}, error) {
	return func(ctx context.Context, docs []*schema.Document, vectors [][]float64) ([]interface{}, error) {
		if len(docs) != len(vectors) {
			return nil, fmt.Errorf("docs/vectors mismatch: docs=%d vectors=%d", len(docs), len(vectors))
		}
		rows := make([]interface{}, 0, len(docs))
		for i := range docs {
			row := map[string]any{
				"id":        docs[i].ID,
				"content":   docs[i].Content,
				vectorField: float64ToFloat32(vectors[i]), // 关键：FloatVector 必须是 []float32
				"metadata":  docs[i].MetaData,
			}
			rows = append(rows, row)
		}
		return rows, nil
	}
}

var fields = []*entity.Field{
	{
		Name:     "id",
		DataType: entity.FieldTypeVarChar,
		TypeParams: map[string]string{
			"max_length": "255",
		},
		PrimaryKey: true,
	},
	{
		Name:     "vector", // 确保字段名匹配
		DataType: entity.FieldTypeFloatVector,
		TypeParams: map[string]string{
			"dim": "2048",
		},
	},
	{
		Name:     "content",
		DataType: entity.FieldTypeVarChar,
		TypeParams: map[string]string{
			"max_length": "8192",
		},
	},
	{
		Name:     "metadata",
		DataType: entity.FieldTypeJSON,
	},
}
