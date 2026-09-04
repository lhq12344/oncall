package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"go_agent/internal/rag"
)

type Pipeline struct {
	Loader Loader
	Writer IndexWriter
}

type Request struct {
	Source       Source
	Profile      ChunkProfile
	IndexVersion string
	Now          time.Time
}

func NewPipeline(loader Loader, writer IndexWriter) *Pipeline {
	if loader == nil {
		loader = InlineLoader{}
	}
	return &Pipeline{Loader: loader, Writer: writer}
}

func (p *Pipeline) Run(ctx context.Context, req Request) (Manifest, error) {
	if p == nil {
		p = NewPipeline(nil, nil)
	}
	doc, err := p.Loader.Load(ctx, req.Source)
	if err != nil {
		return Manifest{}, err
	}
	doc = Normalize(doc)
	if strings.TrimSpace(doc.Content) == "" {
		return Manifest{}, fmt.Errorf("document content is required")
	}
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	if req.IndexVersion == "" {
		req.IndexVersion = "index.v1"
	}
	if doc.DocID == "" {
		doc.DocID = stableID(doc.SourceURI + "\n" + doc.SourceType + "\n" + doc.Content)
	}
	sections := ParseSections(doc)
	chunks := make([]rag.DocumentChunk, 0, len(sections))
	for i, section := range sections {
		contentHash := stableID(section.Content)
		chunkID := stableID(fmt.Sprintf("%s\n%s\n%d\n%s", doc.DocID, req.Profile, i, contentHash))
		chunks = append(chunks, rag.DocumentChunk{ID: chunkID, DocID: doc.DocID, ChunkID: chunkID, ParentID: doc.DocID, SourceType: doc.SourceType, Content: section.Content, ContextualPrefix: ContextualPrefix(doc, section), Metadata: doc.Metadata, SourceURI: doc.SourceURI, Owner: doc.Owner, Version: doc.Version, UpdatedAt: req.Now, ContentHash: contentHash, ChunkProfile: string(req.Profile), IndexVersion: req.IndexVersion})
	}
	manifest := Manifest{Version: "rag.ingest.manifest/v1", IndexVersion: req.IndexVersion, Chunks: chunks}
	if p.Writer != nil {
		if err := p.Writer.WriteChunks(ctx, manifest, chunks); err != nil {
			return Manifest{}, err
		}
	}
	return manifest, nil
}

func stableID(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}
