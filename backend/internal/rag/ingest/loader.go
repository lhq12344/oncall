package ingest

import "context"

type Source struct {
	URI        string
	SourceType string
	Content    string
	Metadata   map[string]any
}

type Loader interface {
	Load(context.Context, Source) (Document, error)
}

type Document struct {
	DocID      string
	SourceURI  string
	SourceType string
	Content    string
	Metadata   map[string]any
	Owner      string
	Version    string
}

type InlineLoader struct{}

func (InlineLoader) Load(_ context.Context, source Source) (Document, error) {
	return Document{SourceURI: source.URI, SourceType: source.SourceType, Content: source.Content, Metadata: source.Metadata}, nil
}
