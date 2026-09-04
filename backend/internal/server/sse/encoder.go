package sse

import (
	"bytes"
	"fmt"

	"go_agent/internal/events"
)

type Encoder struct{}

func (Encoder) Encode(event events.RunEvent) ([]byte, error) {
	line, err := event.MarshalJSONLine()
	if err != nil {
		return nil, err
	}
	line = bytes.TrimSuffix(line, []byte("\n"))
	return []byte(fmt.Sprintf("id: %s\nevent: %s\ndata: %s\n\n", event.ID, event.Type, line)), nil
}
