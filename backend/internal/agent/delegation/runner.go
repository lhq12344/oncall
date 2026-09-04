package delegation

import (
	"context"
	"fmt"
)

type Handler func(context.Context, Task) Result

type Runner struct {
	ParentTools []string
	Handler     Handler
}

func (r Runner) Run(ctx context.Context, task Task) Result {
	if !AllowsTools(r.ParentTools, task.ToolAllowlist) {
		return Result{TaskID: task.ID, Status: StatusFailed, Error: "delegated task requested tools outside parent allowlist"}
	}
	if task.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, task.Timeout)
		defer cancel()
	}
	if r.Handler == nil {
		return Result{TaskID: task.ID, Status: StatusFailed, Error: "delegation handler is required"}
	}
	if task.Timeout <= 0 {
		return r.Handler(ctx, task)
	}
	done := make(chan Result, 1)
	go func() { done <- r.Handler(ctx, task) }()
	select {
	case <-ctx.Done():
		status := StatusCancelled
		if task.Timeout > 0 && ctx.Err() == context.DeadlineExceeded {
			status = StatusTimeout
		}
		return Result{TaskID: task.ID, Status: status, Error: fmt.Sprint(ctx.Err())}
	case result := <-done:
		return result
	}
}
