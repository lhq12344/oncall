package sse

import "go_agent/internal/events"

func ReplayAfter(all []events.RunEvent, lastEventID string) []events.RunEvent {
	if lastEventID == "" {
		out := make([]events.RunEvent, len(all))
		copy(out, all)
		return out
	}
	idx := -1
	for i, event := range all {
		if event.ID == lastEventID {
			idx = i
			break
		}
	}
	if idx < 0 || idx+1 >= len(all) {
		return nil
	}
	out := make([]events.RunEvent, len(all[idx+1:]))
	copy(out, all[idx+1:])
	return out
}
