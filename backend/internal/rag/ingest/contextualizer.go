package ingest

import "strings"

func ContextualPrefix(doc Document, section Section) string {
	parts := []string{"source=" + doc.SourceType}
	if doc.SourceURI != "" {
		parts = append(parts, "uri="+doc.SourceURI)
	}
	if section.Title != "" {
		parts = append(parts, "section="+section.Title)
	}
	return strings.Join(parts, "; ")
}
