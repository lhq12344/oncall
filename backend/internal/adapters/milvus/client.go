package milvus

import (
	"context"
	"fmt"
	"strings"

	"go_agent/utility/common"

	cli "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

func NewClient(ctx context.Context) (cli.Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
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

	defaultClient, err := cli.NewClient(runCtx, cli.Config{
		Address: address,
		DBName:  "default",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to default milvus database at %s within %s: %w", address, timeout, err)
	}
	defer defaultClient.Close()

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

	dbClient, err := cli.NewClient(runCtx, cli.Config{
		Address: address,
		DBName:  database,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to milvus database %s at %s within %s: %w", database, address, timeout, err)
	}

	if milvusConfig.AutoCreateCollection {
		if err := EnsureCollection(runCtx, dbClient, collection); err != nil {
			dbClient.Close()
			return nil, err
		}
	}

	return dbClient, nil
}

func EnsureCollection(ctx context.Context, dbClient cli.Client, collection string) error {
	collection = strings.TrimSpace(collection)
	if collection == "" {
		collection = common.LoadMilvusConfig(ctx).Collection
	}
	if dbClient == nil {
		return fmt.Errorf("milvus client is nil")
	}

	collections, err := dbClient.ListCollections(ctx)
	if err != nil {
		return fmt.Errorf("failed to list milvus collections: %w", err)
	}
	for _, item := range collections {
		if item != nil && item.Name == collection {
			return nil
		}
	}

	schema := &entity.Schema{
		CollectionName:     collection,
		Description:        fmt.Sprintf("OnCall retrieval collection %s", collection),
		Fields:             fields,
		EnableDynamicField: true,
	}
	if err := dbClient.CreateCollection(ctx, schema, entity.DefaultShardNumber); err != nil {
		return fmt.Errorf("failed to create milvus collection %s: %w", collection, err)
	}

	vectorIndex, err := entity.NewIndexAUTOINDEX(entity.COSINE)
	if err != nil {
		return fmt.Errorf("failed to create vector index for collection %s: %w", collection, err)
	}
	if err := dbClient.CreateIndex(ctx, collection, "vector", vectorIndex, false); err != nil {
		return fmt.Errorf("failed to create vector index for collection %s: %w", collection, err)
	}
	return nil
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
		Name:     "vector",
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
