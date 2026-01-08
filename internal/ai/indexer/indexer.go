package indexer

import (
	"context"
	"fmt"
	embedder2 "go_agent/internal/ai/embedder"
	"go_agent/utility/client"
	"go_agent/utility/common"

	"github.com/cloudwego/eino-ext/components/indexer/milvus"
	"github.com/cloudwego/eino/schema"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

func NewMilvusIndexer(ctx context.Context) (*milvus.Indexer, error) {
	cli, err := client.NewMilvusClient(ctx)
	if err != nil {
		return nil, err
	}
	// ======= 关键：删掉旧 schema 的 collection =======
	//has, err := cli.HasCollection(ctx, common.MilvusCollectionName)
	//if err != nil {
	//	return nil, err
	//}
	//if has {
	//	// 开发环境直接删掉重建（生产环境不要这么干）
	//	if err := cli.DropCollection(ctx, common.MilvusCollectionName); err != nil {
	//		return nil, err
	//	}
	//}
	// ============================================
	eb, err := embedder2.DoubaoEmbedding(ctx)
	if err != nil {
		return nil, err
	}
	config := &milvus.IndexerConfig{
		Client:            cli,
		Collection:        common.MilvusCollectionName,
		Fields:            fields,
		Embedding:         eb,
		DocumentConverter: binaryDocConverter(2048),
	}
	indexer, err := milvus.NewIndexer(ctx, config)
	if err != nil {
		return nil, err
	}
	return indexer, nil
}

func floatToBinaryBytes(vec []float64, dimBits int) ([]byte, error) {
	if dimBits%8 != 0 {
		return nil, fmt.Errorf("binary dim must be multiple of 8, got=%d", dimBits)
	}
	if len(vec) != dimBits {
		return nil, fmt.Errorf("vector length != dimBits: len=%d dimBits=%d", len(vec), dimBits)
	}
	out := make([]byte, dimBits/8) // 每 8 bit 1 byte

	// bit packing：第 i 个 float 决定第 i 个 bit（>0 为 1）
	for i := 0; i < dimBits; i++ {
		if vec[i] > 0 {
			byteIdx := i / 8
			bitIdx := uint(i % 8)
			out[byteIdx] |= (1 << bitIdx)
		}
	}
	return out, nil
}

func binaryDocConverter(dimBits int) func(ctx context.Context, docs []*schema.Document, vectors [][]float64) ([]interface{}, error) {
	return func(ctx context.Context, docs []*schema.Document, vectors [][]float64) ([]interface{}, error) {
		if len(docs) != len(vectors) {
			return nil, fmt.Errorf("docs/vectors mismatch: docs=%d vectors=%d", len(docs), len(vectors))
		}
		rows := make([]interface{}, 0, len(docs))
		for i := range docs {
			bvec, err := floatToBinaryBytes(vectors[i], dimBits)
			if err != nil {
				return nil, fmt.Errorf("binarize failed at i=%d: %w", i, err)
			}

			// 返回一行数据，顺序要对应 fields：id, vector, content, metadata
			row := map[string]interface{}{
				"id":       docs[i].ID,
				"vector":   bvec, // []byte
				"content":  docs[i].Content,
				"metadata": docs[i].MetaData,
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
		DataType: entity.FieldTypeBinaryVector,
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
