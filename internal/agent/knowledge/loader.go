package knowledge

import (
	"context"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino-ext/components/document/loader/file"
)

// newLoader creates file loader for knowledge source documents.
func newLoader(ctx context.Context) (ldr document.Loader, err error) {
	config := &file.FileLoaderConfig{
		UseNameAsID: true,
	}
	ldr, err = file.NewFileLoader(ctx, config)
	if err != nil {
		return nil, err
	}
	return ldr, nil
}
