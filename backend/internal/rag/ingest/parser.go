package ingest

import "strings"

type Section struct {
	Title   string
	Content string
}

func ParseSections(doc Document) []Section {
	content := strings.TrimSpace(doc.Content)
	if content == "" {
		return nil
	}
	return []Section{{Title: "document", Content: content}}
}
