package catalog

import "fmt"

type Catalog struct {
	definitions map[WorkflowID]Definition
}

func New(defs ...Definition) *Catalog {
	c := &Catalog{definitions: map[WorkflowID]Definition{}}
	for _, def := range defs {
		if def.ID != "" {
			c.definitions[def.ID] = def
		}
	}
	return c
}

func Default() *Catalog { return New(IncidentDefinition()) }

func (c *Catalog) Resolve(id WorkflowID, version string) (Definition, error) {
	if c == nil {
		c = Default()
	}
	def, ok := c.definitions[id]
	if !ok {
		return Definition{}, fmt.Errorf("workflow %s not found", id)
	}
	if version != "" && def.Version != version {
		return Definition{}, fmt.Errorf("workflow %s version %s not found", id, version)
	}
	return def, nil
}
