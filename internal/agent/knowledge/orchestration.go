package knowledge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/compose"
)

func BuildKnowledgeGraph(ctx context.Context) (compose.Runnable[document.Source, []string], error) {

	var err error
	idx, err := newIndexer(ctx)
	if err != nil {
		return nil, err
	}

	fileLoader, err := newLoader(ctx)
	if err != nil {
		return nil, err
	}

	markdownTransformer, err := newDocumentTransformer(ctx)
	if err != nil {
		return nil, err
	}

	g := compose.NewGraph[document.Source, []string]()
	if err := g.AddLoaderNode("file_loader", fileLoader); err != nil {
		return nil, err
	}
	if err := g.AddDocumentTransformerNode("markdown_splitter", markdownTransformer); err != nil {
		return nil, err
	}
	if err := g.AddIndexerNode("milvus_indexer", idx); err != nil {
		return nil, err
	}
	if err := g.AddEdge(compose.START, "file_loader"); err != nil {
		return nil, err
	}
	if err := g.AddEdge("file_loader", "markdown_splitter"); err != nil {
		return nil, err
	}
	if err := g.AddEdge("markdown_splitter", "milvus_indexer"); err != nil {
		return nil, err
	}
	if err := g.AddEdge("milvus_indexer", compose.END); err != nil {
		return nil, err
	}

	return g.Compile(ctx, compose.WithGraphName("knowledge_indexing"))
}

func BuildKnowledgeUploadChain(ctx context.Context) (compose.Runnable[*uploadInput, *uploadResult], error) {
	indexingRunnable, err := BuildKnowledgeGraph(ctx)
	if err != nil {
		return nil, err
	}

	chain := compose.NewChain[*uploadInput, *uploadResult]()
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, in *uploadInput) (document.Source, error) {
		return writeUploadToSource(in)
	}))
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, src document.Source) ([]string, error) {
		ids, err := indexingRunnable.Invoke(ctx, src)
		_ = os.Remove(src.URI)
		return ids, err
	}))
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, ids []string) (*uploadResult, error) {
		return &uploadResult{IDs: ids}, nil
	}))

	return chain.Compile(ctx)
}

func writeUploadToSource(in *uploadInput) (document.Source, error) {
	if in == nil || strings.TrimSpace(in.Content) == "" {
		return document.Source{}, fmt.Errorf("upload content is required")
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "knowledge_upload"
	}
	fileName := sanitizeFileName(title)
	if fileName == "" {
		fileName = "knowledge_upload"
	}
	fileName = fmt.Sprintf("%d_%s.md", time.Now().UnixNano(), fileName)

	fullContent := strings.TrimSpace(in.Content)
	if len(in.Meta) > 0 {
		fullContent = fmt.Sprintf("%s\n\nmeta: %v", fullContent, in.Meta)
	}

	path := filepath.Join(os.TempDir(), fileName)
	if err := os.WriteFile(path, []byte(fullContent), 0o600); err != nil {
		return document.Source{}, fmt.Errorf("failed to create temp knowledge file: %w", err)
	}

	return document.Source{URI: path}, nil
}

func sanitizeFileName(name string) string {
	replaced := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		"\n", "_",
		"\r", "_",
		"\t", "_",
	).Replace(strings.TrimSpace(name))
	return strings.Trim(replaced, "._ ")
}
