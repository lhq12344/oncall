package ingest

import "strings"

func Normalize(doc Document) Document {
	doc.Content = strings.TrimSpace(strings.ReplaceAll(doc.Content, "\r\n", "\n"))
	doc.SourceType = strings.TrimSpace(doc.SourceType)
	if doc.Version == "" {
		doc.Version = "v1"
	}
	return doc
}
