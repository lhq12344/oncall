package rag

import "strings"

type EvalCase struct {
	ID          string           `json:"id"`
	Profile     RetrievalProfile `json:"profile"`
	Query       string           `json:"query"`
	ExpectedIDs []string         `json:"expected_ids,omitempty"`
	Notes       string           `json:"notes,omitempty"`
}

type EvalResult struct {
	ID        string  `json:"id"`
	Scored    bool    `json:"scored"`
	Hit       bool    `json:"hit"`
	Rank      int     `json:"rank,omitempty"`
	RecallAtK float64 `json:"recall_at_k"`
}

type EvalSummary struct {
	Total       int          `json:"total"`
	Scored      int          `json:"scored"`
	Unscored    int          `json:"unscored"`
	Hits        int          `json:"hits"`
	RecallAtK   float64      `json:"recall_at_20"`
	MRRAt3      float64      `json:"mrr_at_3"`
	Top3HitRate float64      `json:"top3_hit_rate"`
	Results     []EvalResult `json:"results"`
}

func EvaluateRetrievedContexts(cases []EvalCase, contexts map[string]*RetrievedContext) EvalSummary {
	summary := EvalSummary{Total: len(cases), Results: make([]EvalResult, 0, len(cases))}
	for _, item := range cases {
		result := EvalResult{ID: item.ID}
		expected := make(map[string]struct{}, len(item.ExpectedIDs))
		for _, id := range item.ExpectedIDs {
			if id = strings.TrimSpace(id); id != "" {
				expected[id] = struct{}{}
			}
		}
		if len(expected) == 0 {
			summary.Unscored++
			summary.Results = append(summary.Results, result)
			continue
		}
		result.Scored = true
		summary.Scored++
		ctx := contexts[item.ID]
		if ctx != nil {
			for rank, got := range ctx.Results {
				if _, ok := expected[strings.TrimSpace(got.ID)]; ok {
					result.Hit = true
					result.Rank = rank + 1
					break
				}
			}
		}
		if result.Hit {
			result.RecallAtK = 1
			summary.Hits++
			if result.Rank <= 3 {
				summary.MRRAt3 += 1.0 / float64(result.Rank)
				summary.Top3HitRate++
			}
		}
		summary.Results = append(summary.Results, result)
	}
	if summary.Scored > 0 {
		summary.RecallAtK = float64(summary.Hits) / float64(summary.Scored)
		summary.MRRAt3 /= float64(summary.Scored)
		summary.Top3HitRate /= float64(summary.Scored)
	}
	return summary
}
