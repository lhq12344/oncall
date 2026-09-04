package memory

import (
	"fmt"
	"strings"

	"go_agent/internal/context/notice"
)

func Notice(records []Record) notice.Notice {
	parts := make([]string, 0, len(records))
	for _, record := range records {
		parts = append(parts, "- "+record.Content+" (source="+record.Source+", confidence="+formatConfidence(record.Confidence)+", provenance="+record.Provenance+")")
	}
	return notice.Notice{Kind: notice.KindMemoryRecall, Trust: notice.TrustTrustedRuntime, Source: "memory", Lifecycle: notice.LifecycleRun, Content: strings.Join(parts, "\n"), Priority: 50, DedupKey: "memory.recall"}
}

func formatConfidence(confidence float64) string {
	if confidence >= 0.995 {
		return "1.00"
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", confidence), "0"), ".")
}
