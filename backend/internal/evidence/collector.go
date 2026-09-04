package evidence

import (
	"context"
	"sync"
	"time"
)

type Source interface {
	Collect(context.Context, Query) Evidence
}

type Collector struct {
	Sources []Source
	Now     func() time.Time
}

func (c Collector) Collect(ctx context.Context, query Query) []Evidence {
	now := time.Now().UTC()
	if c.Now != nil {
		now = c.Now()
	}
	if len(c.Sources) == 0 {
		return []Evidence{PermissionEvidence("evidence", query.Scope, "no evidence sources configured", now)}
	}
	out := make([]Evidence, len(c.Sources))
	var wg sync.WaitGroup
	for i, source := range c.Sources {
		wg.Add(1)
		go func(i int, source Source) {
			defer wg.Done()
			if source == nil {
				out[i] = PermissionEvidence("unknown", query.Scope, "nil evidence source", now)
				return
			}
			out[i] = source.Collect(ctx, query)
		}(i, source)
	}
	wg.Wait()
	for i := range out {
		if out[i].Timestamp.IsZero() {
			out[i].Timestamp = now
		}
		if out[i].ArtifactRef.ID == "" {
			out[i].ArtifactRef = ArtifactRef{ID: out[i].Source + ":inline", Kind: "inline"}
		}
	}
	return out
}
