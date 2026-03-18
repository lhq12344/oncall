package client

import (
	"context"
	"fmt"
	"go_agent/utility/common"

	cli "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

func NewMilvusClient(ctx context.Context) (cli.Client, error) {
	milvusConfig := common.LoadMilvusConfig(ctx)
	address := milvusConfig.Address
	database := milvusConfig.Database
	collection := milvusConfig.Collection
	timeout := milvusConfig.Timeout
	if timeout <= 0 {
		timeout = common.DefaultMilvusTimeout
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 1. 先连接default数据库
	defaultClient, err := cli.NewClient(runCtx, cli.Config{
		Address: address,
		DBName:  "default",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to default milvus database at %s within %s: %w", address, timeout, err)
	}
	defer defaultClient.Close()
	// 2. 检查agent数据库是否存在，不存在则创建
	databases, err := defaultClient.ListDatabases(runCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to list milvus databases within %s: %w", timeout, err)
	}
	if database != "default" {
		targetDBExists := false
		for _, db := range databases {
			if db.Name == database {
				targetDBExists = true
				break
			}
		}
		if !targetDBExists {
			err = defaultClient.CreateDatabase(runCtx, database)
			if err != nil {
				return nil, fmt.Errorf("failed to create milvus database %s: %w", database, err)
			}
		}
	}

	// 3. 创建连接到目标数据库的客户端
	dbClient, err := cli.NewClient(runCtx, cli.Config{
		Address: address,
		DBName:  database,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to milvus database %s at %s within %s: %w", database, address, timeout, err)
	}
	// 4. 检查默认知识 collection 是否存在，不存在则创建
	collections, err := dbClient.ListCollections(runCtx)
	if err != nil {
		dbClient.Close()
		return nil, fmt.Errorf("failed to list collections: %w", err)
	}

	knowledgeCollectionExists := false
	for _, item := range collections {
		if item.Name == collection {
			knowledgeCollectionExists = true
			break
		}
	}

	if !knowledgeCollectionExists {
		// 创建默认知识 collection 的 schema
		schema := &entity.Schema{
			CollectionName:     collection,
			Description:        "Business knowledge collection",
			Fields:             fields,
			EnableDynamicField: true, // 启用动态字段支持
		}

		err = dbClient.CreateCollection(runCtx, schema, entity.DefaultShardNumber)
		if err != nil {
			dbClient.Close()
			return nil, fmt.Errorf("failed to create milvus collection %s: %w", collection, err)
		}

		// 为id字段创建autoindex索引
		idIndex, err := entity.NewIndexAUTOINDEX(entity.L2)
		if err != nil {
			return nil, fmt.Errorf("failed to create id index: %w", err)
		}
		err = dbClient.CreateIndex(runCtx, collection, "id", idIndex, false)
		if err != nil {
			dbClient.Close()
			return nil, fmt.Errorf("failed to create id index: %w", err)
		}

		// 为content字段创建autoindex索引
		contentIndex, err := entity.NewIndexAUTOINDEX(entity.L2)
		if err != nil {
			return nil, fmt.Errorf("failed to create content index: %w", err)
		}
		err = dbClient.CreateIndex(runCtx, collection, "content", contentIndex, false)
		if err != nil {
			dbClient.Close()
			return nil, fmt.Errorf("failed to create content index: %w", err)
		}

		// 为vector字段创建autoindex索引
		vectorIndex, err := entity.NewIndexAUTOINDEX(entity.COSINE)
		if err != nil {
			return nil, fmt.Errorf("failed to create vector index: %w", err)
		}
		err = dbClient.CreateIndex(runCtx, collection, "vector", vectorIndex, false)
		if err != nil {
			dbClient.Close()
			return nil, fmt.Errorf("failed to create vector index: %w", err)
		}
	}

	return dbClient, nil
}

var fields = []*entity.Field{
	{
		Name:     "id",
		DataType: entity.FieldTypeVarChar,
		TypeParams: map[string]string{
			"max_length": "256",
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
