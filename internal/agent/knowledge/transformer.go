package knowledge

import (
	"context"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino-ext/components/document/transformer/splitter/markdown"
)

// newDocumentTransformer creates markdown header splitter.
func newDocumentTransformer(ctx context.Context) (tfr document.Transformer, err error) {
	config := &markdown.HeaderConfig{
		Headers: map[string]string{
			"#": "h1",
		},
		TrimHeaders: false,
	}
	tfr, err = markdown.NewHeaderSplitter(ctx, config)
	if err != nil {
		return nil, err
	}
	return tfr, nil
}
