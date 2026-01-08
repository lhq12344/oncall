package main

import (
	"context"
	"fmt"
	"go_agent/internal/ai/agent/knowledge_index_pipeline"
	loader2 "go_agent/internal/ai/loader"
	"go_agent/utility/client"
	"go_agent/utility/common"
	"go_agent/utility/log_call_back"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/compose"
)

func main() {
	ctx := context.Background()

	r, err := knowledge_index_pipeline.BuildKnowledgeIndexing(ctx)
	if err != nil {
		panic(err)
	}

	// 这些组件不要每个文件 new 一次，复用
	loader, err := loader2.NewFileLoader(ctx)
	if err != nil {
		panic(err)
	}

	//创建访问Milvus的客户端
	cli, err := client.NewMilvusClient(ctx)
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	var failed int

	//filepath.WalkDir 是 Go 标准库 path/filepath 提供的目录递归遍历函数
	//从指定根目录开始，按层级向下遍历所有子目录和文件，并对每一个路径回调你提供的处理函数
	err = filepath.WalkDir("./docs", func(path string, d fs.DirEntry, walkErr error) error {
		// 1) WalkDir 传进来的 walkErr 不为空，说明目录项本身就读不到
		if walkErr != nil {
			fmt.Printf("[warn] walk error on %s: %v\n", path, walkErr)
			failed++
			return nil // 不中断全局
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			fmt.Printf("[skip] not a markdown file: %s\n", path)
			return nil
		}

		fmt.Printf("[start] indexing file: %s\n", path)

		// 2) load
		docs, err := loader.Load(ctx, document.Source{URI: path}) //加载文件
		if err != nil {
			fmt.Printf("[error] load failed: %s: %v\n", path, err)
			failed++
			return nil
		}
		if len(docs) == 0 {
			fmt.Printf("[warn] empty docs after load: %s\n", path)
			return nil
		}

		// 3) 删除旧数据（按 _source）
		src, _ := docs[0].MetaData["_source"].(string)
		if src != "" {
			expr := fmt.Sprintf(`metadata["_source"] == "%s"`, src)
			queryResult, err := cli.Query(ctx, common.MilvusCollectionName, []string{}, expr, []string{"id"})
			if err != nil {
				fmt.Printf("[warn] milvus query failed: %s: %v\n", path, err)
				// 删除失败不影响索引，可继续
			} else if len(queryResult) > 0 {
				var idsToDelete []string
				for _, column := range queryResult {
					if column.Name() == "id" {
						for i := 0; i < column.Len(); i++ {
							id, e := column.GetAsString(i)
							if e == nil {
								idsToDelete = append(idsToDelete, id)
							}
						}
					}
				}
				if len(idsToDelete) > 0 {
					deleteExpr := fmt.Sprintf(`id in ["%s"]`, strings.Join(idsToDelete, `","`))
					if err := cli.Delete(ctx, common.MilvusCollectionName, "", deleteExpr); err != nil {
						fmt.Printf("[warn] delete existing data failed: %s: %v\n", path, err)
					} else {
						fmt.Printf("[info] deleted %d existing records with _source: %s\n", len(idsToDelete), src)
					}
				}
			}
		}

		// 4) 重新构建
		ids, err := r.Invoke(ctx, document.Source{URI: path}, compose.WithCallbacks(log_call_back.LogCallback(nil)))
		if err != nil {
			fmt.Printf("[error] invoke index graph failed: %s: %v\n", path, err)
			failed++
			return nil // 不中断全局
		}

		fmt.Printf("[done] indexing file: %s, len of parts: %d, ids=%v\n", path, len(ids), ids)
		return nil
	})

	// 5) 一定要检查 WalkDir 返回值
	if err != nil {
		fmt.Printf("[fatal] WalkDir stopped with error: %v\n", err)
	}

	fmt.Printf("[finish] all done, failed=%d\n", failed)
}
