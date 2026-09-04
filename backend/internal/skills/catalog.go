package skills

import (
	"fmt"
	"sort"
	"strings"
)

type Catalog struct {
	items map[string]Metadata
}

func NewCatalog(items []Metadata) (*Catalog, error) {
	c := &Catalog{items: map[string]Metadata{}}
	for _, item := range items {
		name := strings.ToLower(strings.TrimSpace(item.Name))
		if name == "" {
			return nil, fmt.Errorf("skill name is required")
		}
		if _, exists := c.items[name]; exists {
			return nil, fmt.Errorf("duplicate skill %q", name)
		}
		item.Name = name
		c.items[name] = item
	}
	return c, nil
}

func (c *Catalog) Match(input string) []Metadata {
	if c == nil {
		return nil
	}
	lower := strings.ToLower(input)
	out := make([]Metadata, 0)
	for _, item := range c.items {
		for _, trigger := range item.Triggers {
			if trigger != "" && strings.Contains(lower, strings.ToLower(trigger)) {
				out = append(out, item)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (c *Catalog) Get(name string) (Metadata, bool) {
	if c == nil {
		return Metadata{}, false
	}
	item, ok := c.items[strings.ToLower(strings.TrimSpace(name))]
	return item, ok
}
