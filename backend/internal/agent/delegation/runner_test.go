package delegation

import (
	"context"
	"testing"
	"time"
)

func TestRunnerCannotExpandParentTools(t *testing.T) {
	result := Runner{ParentTools: []string{"k8s_monitor"}, Handler: func(context.Context, Task) Result { return Result{Status: StatusSucceeded} }}.Run(context.Background(), Task{ID: "t1", ToolAllowlist: []string{"execute_step"}})
	if result.Status != StatusFailed {
		t.Fatalf("expected failed delegation: %+v", result)
	}
}

func TestRunnerTimeoutDoesNotLoseTaskIdentity(t *testing.T) {
	result := Runner{ParentTools: []string{"k8s_monitor"}, Handler: func(ctx context.Context, task Task) Result {
		<-ctx.Done()
		return Result{TaskID: task.ID, Status: StatusFailed}
	}}.Run(context.Background(), Task{ID: "t1", ToolAllowlist: []string{"k8s_monitor"}, Timeout: time.Millisecond})
	if result.TaskID != "t1" || result.Status != StatusTimeout {
		t.Fatalf("unexpected timeout result: %+v", result)
	}
}
