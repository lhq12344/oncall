package notice

import (
	"sort"
	"strings"
	"time"
)

func Render(items []Notice, now time.Time) string {
	items = Active(items, now)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority == items[j].Priority {
			return items[i].DedupKey < items[j].DedupKey
		}
		return items[i].Priority < items[j].Priority
	})
	parts := make([]string, 0, len(items))
	for _, item := range items {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		parts = append(parts, "["+string(item.Kind)+" trust="+string(item.Trust)+" source="+item.Source+"]\n"+content)
	}
	return strings.Join(parts, "\n\n")
}
