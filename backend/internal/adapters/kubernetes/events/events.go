package events

import "go_agent/internal/evidence"

type Query struct {
	Scope evidence.Scope
}

type Result struct {
	Items []string
}
