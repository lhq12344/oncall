package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// RollbackTool 回滚工具
type RollbackTool struct {
	logger *zap.Logger
}

// RollbackResult 回滚结果
type RollbackResult struct {
	StepID     int      `json:"step_id"`
	Success    bool     `json:"success"`
	Message    string   `json:"message"`
	RolledBack []string `json:"rolled_back"` // 已回滚的步骤
	Failed     []string `json:"failed"`      // 回滚失败的步骤
	Duration   int      `json:"duration"`    // 毫秒
}

func NewRollbackTool(logger *zap.Logger) tool.BaseTool {
	return &RollbackTool{
		logger: logger,
	}
}

func (t *RollbackTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "rollback",
		Desc: "执行回滚操作。按照相反顺序执行回滚命令，恢复到执行前的状态。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"step_id": {
				Type:     schema.Integer,
				Desc:     "失败的步骤 ID",
				Required: true,
			},
			"rollback_steps": {
				Type:     schema.Array,
				ElemInfo: &schema.ParameterInfo{Type: schema.Object},
				Desc:     "回滚步骤列表（JSON 数组）",
				Required: true,
			},
			"reason": {
				Type:     schema.String,
				Desc:     "回滚原因",
				Required: false,
			},
		}),
	}, nil
}

func (t *RollbackTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type rollbackStep struct {
		StepID  int      `json:"step_id"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}

	type args struct {
		StepID        int            `json:"step_id"`
		RollbackSteps []rollbackStep `json:"rollback_steps"`
		Reason        string         `json:"reason"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if len(in.RollbackSteps) == 0 {
		return "", fmt.Errorf("no rollback steps provided")
	}

	start := time.Now()

	// 按相反顺序执行回滚
	rolledBack := []string{}
	failed := []string{}

	for i := len(in.RollbackSteps) - 1; i >= 0; i-- {
		step := in.RollbackSteps[i]

		if step.Command == "" {
			// 跳过没有回滚命令的步骤
			continue
		}

		stepDesc := fmt.Sprintf("Step %d: %s %s", step.StepID, step.Command, strings.Join(step.Args, " "))

		if t.logger != nil {
			t.logger.Info("executing rollback",
				zap.Int("step_id", step.StepID),
				zap.String("command", step.Command))
		}

		// 执行回滚命令
		cmd := exec.CommandContext(ctx, step.Command, step.Args...)
		output, err := cmd.CombinedOutput()

		if err != nil {
			failed = append(failed, stepDesc)
			if t.logger != nil {
				t.logger.Error("rollback failed",
					zap.Int("step_id", step.StepID),
					zap.Error(err),
					zap.String("output", string(output)))
			}
		} else {
			rolledBack = append(rolledBack, stepDesc)
			if t.logger != nil {
				t.logger.Info("rollback succeeded",
					zap.Int("step_id", step.StepID))
			}
		}
	}

	duration := time.Since(start).Milliseconds()

	result := &RollbackResult{
		StepID:     in.StepID,
		Success:    len(failed) == 0,
		RolledBack: rolledBack,
		Failed:     failed,
		Duration:   int(duration),
	}

	if result.Success {
		result.Message = fmt.Sprintf("Successfully rolled back %d steps", len(rolledBack))
	} else {
		result.Message = fmt.Sprintf("Rolled back %d steps, %d failed", len(rolledBack), len(failed))
	}

	if in.Reason != "" {
		result.Message += fmt.Sprintf(". Reason: %s", in.Reason)
	}

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("rollback completed",
			zap.Int("failed_step_id", in.StepID),
			zap.Bool("success", result.Success),
			zap.Int("rolled_back", len(rolledBack)),
			zap.Int("failed", len(failed)))
	}

	return string(output), nil
}
