package app

import (
	"context"
	"fmt"

	"go_agent/internal/events"
)

type RunContext struct {
	RunID   string
	TraceID string
	Events  *events.Emitter
}

func (a *Application) NewRun(ctx context.Context, runID, traceID string) (*RunContext, error) {
	if a == nil {
		return nil, fmt.Errorf("application is nil")
	}
	emitter, err := events.NewEmitter(runID, traceID, a.EventSink)
	if err != nil {
		return nil, err
	}
	if _, err := emitter.Emit(ctx, events.EventRunStarted, nil); err != nil {
		return nil, err
	}
	return &RunContext{RunID: runID, TraceID: traceID, Events: emitter}, nil
}
