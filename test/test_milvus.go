package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"go_agent/utility/client"
	"go_agent/utility/common"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Println("🔗 Testing Milvus connection...")
	fmt.Println("📍 Address: localhost:31953")

	milvusClient, err := client.NewMilvusClient(ctx)
	if err != nil {
		log.Fatalf("❌ Failed to connect to Milvus: %v", err)
	}
	defer milvusClient.Close()

	fmt.Println("✅ Successfully connected to Milvus!")
	fmt.Printf("📊 Database: %s\n", common.MilvusDBName)
	fmt.Printf("📁 Collection: %s\n", common.MilvusCollectionName)

	// 列出所有 collections
	collections, err := milvusClient.ListCollections(ctx)
	if err != nil {
		log.Fatalf("❌ Failed to list collections: %v", err)
	}

	fmt.Println("\n📋 Collections:")
	for _, coll := range collections {
		fmt.Printf("  - %s\n", coll.Name)
	}

	fmt.Println("\n✅ Milvus is ready for oncall agent!")
}
