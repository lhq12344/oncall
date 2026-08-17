package rag

import (
	"fmt"
	"sort"
	"strings"
)

func FuseRankedLists(lists [][]RetrievedResult, topK, rrfK int) []RetrievedResult {
	if rrfK <= 0 {
		rrfK = DefaultConfig().RRFK
	}
	if topK <= 0 {
		topK = DefaultConfig().FusionTopK
	}
	type aggregate struct {
		item RetrievedResult
		best float64
	}
	merged := map[string]*aggregate{}
	for _, list := range lists {
		for rank, item := range list {
			key := resultDedupeKey(item)
			if key == "" {
				continue
			}
			if len(item.RetrievalPath) == 0 {
				item.RetrievalPath = retrievalPathFromSource(item.Source)
			}
			score := 1.0 / float64(rrfK+rank+1)
			agg, ok := merged[key]
			if !ok {
				item.RetrievalPath = appendRetrievalPath(item.RetrievalPath, "rrf")
				item.RRFScore = score
				item.Score = score
				merged[key] = &aggregate{item: item, best: rawScore(item)}
				continue
			}
			agg.item.RRFScore += score
			agg.item.Score = agg.item.RRFScore
			if raw := rawScore(item); raw > agg.best {
				agg.best = raw
				agg.item.Content = firstNonEmpty(item.Content, agg.item.Content)
				agg.item.ID = firstNonEmpty(item.ID, agg.item.ID)
				agg.item.Meta = mergeMeta(agg.item.Meta, item.Meta)
			}
			if item.VectorScore > agg.item.VectorScore {
				agg.item.VectorScore = item.VectorScore
			}
			if item.BM25Score > agg.item.BM25Score {
				agg.item.BM25Score = item.BM25Score
			}
			agg.item.Source = mergeSource(agg.item.Source, item.Source)
			agg.item.RetrievalPath = appendRetrievalPath(agg.item.RetrievalPath, item.RetrievalPath...)
			agg.item.RetrievalPath = appendRetrievalPath(agg.item.RetrievalPath, "rrf")
		}
	}
	out := make([]RetrievedResult, 0, len(merged))
	for _, agg := range merged {
		agg.item.Score = agg.item.RRFScore
		out = append(out, agg.item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RRFScore == out[j].RRFScore {
			return out[i].ID < out[j].ID
		}
		return out[i].RRFScore > out[j].RRFScore
	})
	if len(out) > topK {
		out = out[:topK]
	}
	return out
}

func resultDedupeKey(item RetrievedResult) string {
	if item.Meta != nil {
		for _, key := range []string{"chunk_id", "content_hash"} {
			if value := strings.TrimSpace(fmt.Sprintf("%v", item.Meta[key])); value != "" && value != "<nil>" {
				return key + ":" + value
			}
		}
	}
	if value := strings.TrimSpace(item.ID); value != "" {
		return "id:" + value
	}
	if value := strings.TrimSpace(item.Content); value != "" {
		return "content:" + ContentHash(value)
	}
	return ""
}

func rawScore(item RetrievedResult) float64 {
	score := item.VectorScore
	if item.BM25Score > score {
		score = item.BM25Score
	}
	if item.Score > score {
		score = item.Score
	}
	return score
}

func BoostOpsCaseResults(items []RetrievedResult) []RetrievedResult {
	for i := range items {
		meta := items[i].Meta
		if meta == nil {
			continue
		}
		boost := 0.0
		for _, key := range []string{"root_cause", "target_node", "final_status", "service", "namespace"} {
			if value := strings.TrimSpace(fmt.Sprintf("%v", meta[key])); value != "" && value != "<nil>" {
				boost += 0.0005
			}
		}
		sourceType := strings.TrimSpace(fmt.Sprintf("%v", firstMeta(meta, "source_type", "type")))
		if sourceType == "ops_final_report" {
			boost += 0.001
		}
		items[i].Score += boost
		items[i].RRFScore += boost
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			return items[i].ID < items[j].ID
		}
		return items[i].Score > items[j].Score
	})
	return items
}

func firstMeta(meta map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := meta[key]; ok {
			return value
		}
	}
	return ""
}

func mergeSource(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" {
		return b
	}
	if b == "" || strings.Contains(","+a+",", ","+b+",") {
		return a
	}
	return a + "," + b
}

func retrievalPathFromSource(source string) []string {
	parts := strings.Split(source, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch {
		case strings.HasPrefix(part, "embedding"):
			out = appendRetrievalPath(out, "embedding")
		case strings.HasPrefix(part, "bm25"):
			out = appendRetrievalPath(out, "bm25")
		case strings.Contains(part, "local"):
			out = appendRetrievalPath(out, "local")
		default:
			out = appendRetrievalPath(out, part)
		}
	}
	return out
}

func appendRetrievalPath(base []string, parts ...string) []string {
	out := append([]string(nil), base...)
	seen := make(map[string]struct{}, len(out)+len(parts))
	for _, item := range out {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		seen[item] = struct{}{}
	}
	for _, part := range parts {
		for _, item := range strings.Split(part, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			out = append(out, item)
		}
	}
	return out
}
