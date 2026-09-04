package config

import "fmt"

type Observability struct {
	Trace Trace
}

type Trace struct {
	Required        bool
	Exporter        string
	Endpoint        string
	LocalBufferSize int
}

func (o Observability) Validate() error {
	if o.Trace.Required && o.Trace.Exporter == "" {
		return fmt.Errorf("trace exporter is required when trace.required=true")
	}
	if o.Trace.LocalBufferSize < 0 {
		return fmt.Errorf("trace local buffer size must not be negative")
	}
	return nil
}
