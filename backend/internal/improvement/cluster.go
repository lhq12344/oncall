package improvement

import "strings"

type Cluster struct {
	Key   string
	Cases []Case
}

func ClusterCases(cases []Case) []Cluster {
	byKey := map[string][]Case{}
	order := []string{}
	for _, item := range cases {
		key := strings.ToLower(strings.TrimSpace(item.NormalizedQuestion))
		if key == "" {
			key = item.ID
		}
		if _, ok := byKey[key]; !ok {
			order = append(order, key)
		}
		byKey[key] = append(byKey[key], item)
	}
	out := make([]Cluster, 0, len(order))
	for _, key := range order {
		out = append(out, Cluster{Key: key, Cases: byKey[key]})
	}
	return out
}
