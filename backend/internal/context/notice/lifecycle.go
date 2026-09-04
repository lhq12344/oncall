package notice

import "time"

func Active(items []Notice, now time.Time) []Notice {
	seen := map[string]Notice{}
	ordered := make([]Notice, 0, len(items))
	for _, item := range items {
		if item.Expired(now) {
			continue
		}
		key := item.DedupKey
		if key == "" {
			key = string(item.Kind) + ":" + item.Source + ":" + item.Content
		}
		if previous, ok := seen[key]; ok {
			if item.Priority < previous.Priority {
				seen[key] = item
			}
			continue
		}
		seen[key] = item
		ordered = append(ordered, item)
	}
	out := make([]Notice, 0, len(ordered))
	for _, item := range ordered {
		key := item.DedupKey
		if key == "" {
			key = string(item.Kind) + ":" + item.Source + ":" + item.Content
		}
		out = append(out, seen[key])
	}
	return out
}
