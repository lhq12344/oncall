package registry

import "strings"

type Descriptor struct {
	ID          string
	Version     string
	Capability  string
	Exposure    string
	Risk        string
	Description string
}

func Search(descriptors []Descriptor, query string, limit int) []Descriptor {
	query = strings.TrimSpace(strings.ToLower(query))
	if limit <= 0 {
		limit = 5
	}
	out := make([]Descriptor, 0, limit)
	for _, descriptor := range descriptors {
		blob := strings.ToLower(strings.Join([]string{descriptor.ID, descriptor.Capability, descriptor.Description}, " "))
		if query == "" || strings.Contains(blob, query) {
			out = append(out, descriptor)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}
