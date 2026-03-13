package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// GeneratePlanTool 执行计划生成工具
type GeneratePlanTool struct {
	chatModel *models.ChatModel
	logger    *zap.Logger
}

// ExecutionStep 执行步骤
type ExecutionStep struct {
	StepID          int      `json:"step_id"`
	Description     string   `json:"description"`
	Command         string   `json:"command"`
	Args            []string `json:"args"`
	ExpectedResult  string   `json:"expected_result"`
	RollbackCommand string   `json:"rollback_command"`
	RollbackArgs    []string `json:"rollback_args"`
	Timeout         int      `json:"timeout"`  // 秒
	Critical        bool     `json:"critical"` // 是否关键步骤
}

// ExecutionPlan 执行计划
type ExecutionPlan struct {
	PlanID        string          `json:"plan_id"`
	Description   string          `json:"description"`
	Steps         []ExecutionStep `json:"steps"`
	TotalSteps    int             `json:"total_steps"`
	EstimatedTime int             `json:"estimated_time"` // 秒
	RiskLevel     string          `json:"risk_level"`     // low/medium/high
}

func NewGeneratePlanTool(chatModel *models.ChatModel, logger *zap.Logger) tool.BaseTool {
	return &GeneratePlanTool{
		chatModel: chatModel,
		logger:    logger,
	}
}

func (t *GeneratePlanTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "generate_plan",
		Desc: "将用户意图转换为可执行的步骤序列。生成包含命令、参数、预期结果和回滚命令的执行计划。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"intent": {
				Type:     schema.String,
				Desc:     "用户意图描述（如：重启 nginx 服务）",
				Required: true,
			},
			"context": {
				Type:     schema.String,
				Desc:     "上下文信息（可选，如：Pod 名称、命名空间等）",
				Required: false,
			},
		}),
	}, nil
}

func (t *GeneratePlanTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Intent  string `json:"intent"`
		Context string `json:"context"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	in.Intent = strings.TrimSpace(in.Intent)
	if in.Intent == "" {
		return "", fmt.Errorf("intent is required")
	}

	// 1. 使用 LLM 生成执行计划
	plan, err := t.generatePlanWithLLM(ctx, in.Intent, in.Context)
	if err != nil {
		if t.logger != nil {
			t.logger.Warn("LLM plan generation failed, using template",
				zap.Error(err))
		}
		// 降级到模板方案
		plan = t.generatePlanWithTemplate(in.Intent, in.Context)
	}

	// 2. 验证计划安全性
	if err := t.validatePlanSafety(plan); err != nil {
		return "", fmt.Errorf("plan validation failed: %w", err)
	}

	// 3. 评估风险等级
	plan.RiskLevel = t.assessRiskLevel(plan)
	markExecutionPlanPrepared(ctx, plan.PlanID)

	output, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("failed to marshal plan: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("execution plan generated",
			zap.String("plan_id", plan.PlanID),
			zap.Int("steps", plan.TotalSteps),
			zap.String("risk_level", plan.RiskLevel))
	}

	return string(output), nil
}

// generatePlanWithLLM 使用 LLM 生成执行计划
func (t *GeneratePlanTool) generatePlanWithLLM(ctx context.Context, intent, context string) (*ExecutionPlan, error) {
	if t.chatModel == nil {
		return nil, fmt.Errorf("chat model not available")
	}

	prompt := fmt.Sprintf(`根据以下用户意图生成执行计划：

意图：%s
上下文：%s

请生成一个详细的执行计划，包含以下信息：
1. 每个步骤的描述
2. 要执行的命令和参数
3. 预期结果
4. 回滚命令
5. 超时时间（秒）
6. 是否为关键步骤

返回 JSON 格式：
{
  "description": "计划描述",
  "steps": [
    {
      "step_id": 1,
      "description": "步骤描述",
      "command": "命令",
      "args": ["参数1", "参数2"],
      "expected_result": "预期结果",
      "rollback_command": "回滚命令",
      "rollback_args": ["回滚参数"],
      "timeout": 30,
      "critical": true
    }
  ]
}`, intent, context)

	resp, err := t.chatModel.Client.Generate(ctx, []*schema.Message{
		schema.UserMessage(prompt),
	})
	if err != nil {
		return nil, err
	}

	content := resp.Content
	if content == "" {
		return nil, fmt.Errorf("empty response from LLM")
	}

	// 提取 JSON
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		jsonStr := content[start : end+1]

		var planData struct {
			Description string          `json:"description"`
			Steps       []ExecutionStep `json:"steps"`
		}

		if err := json.Unmarshal([]byte(jsonStr), &planData); err == nil {
			// 生成计划 ID
			planID := fmt.Sprintf("plan_%d", len(intent))

			// 计算总时间
			totalTime := 0
			for _, step := range planData.Steps {
				totalTime += step.Timeout
			}

			return &ExecutionPlan{
				PlanID:        planID,
				Description:   planData.Description,
				Steps:         planData.Steps,
				TotalSteps:    len(planData.Steps),
				EstimatedTime: totalTime,
			}, nil
		}
	}

	return nil, fmt.Errorf("failed to parse LLM response")
}

// generatePlanWithTemplate 使用模板生成执行计划（降级方案）
func (t *GeneratePlanTool) generatePlanWithTemplate(intent, context string) *ExecutionPlan {
	lower := strings.ToLower(intent)

	var steps []ExecutionStep

	// 根据意图关键词生成模板计划
	if strings.Contains(lower, "重启") || strings.Contains(lower, "restart") {
		if strings.Contains(lower, "pod") || strings.Contains(lower, "容器") {
			steps = []ExecutionStep{
				{
					StepID:          1,
					Description:     "获取 Pod 信息",
					Command:         "kubectl",
					Args:            []string{"get", "pod", "-n", "default"},
					ExpectedResult:  "Pod 列表",
					RollbackCommand: "",
					Timeout:         10,
					Critical:        false,
				},
				{
					StepID:          2,
					Description:     "删除 Pod 触发重启",
					Command:         "kubectl",
					Args:            []string{"delete", "pod", "<pod-name>", "-n", "default"},
					ExpectedResult:  "Pod 删除成功",
					RollbackCommand: "",
					Timeout:         30,
					Critical:        true,
				},
				{
					StepID:          3,
					Description:     "验证新 Pod 启动",
					Command:         "kubectl",
					Args:            []string{"wait", "--for=condition=Ready", "pod", "-l", "app=<app>", "-n", "default", "--timeout=60s"},
					ExpectedResult:  "Pod Ready",
					RollbackCommand: "",
					Timeout:         60,
					Critical:        true,
				},
			}
		} else {
			steps = []ExecutionStep{
				{
					StepID:          1,
					Description:     "检查服务状态",
					Command:         "systemctl",
					Args:            []string{"status", "nginx"},
					ExpectedResult:  "服务状态信息",
					RollbackCommand: "",
					Timeout:         5,
					Critical:        false,
				},
				{
					StepID:          2,
					Description:     "重启服务",
					Command:         "systemctl",
					Args:            []string{"restart", "nginx"},
					ExpectedResult:  "服务重启成功",
					RollbackCommand: "systemctl",
					RollbackArgs:    []string{"start", "nginx"},
					Timeout:         30,
					Critical:        true,
				},
				{
					StepID:          3,
					Description:     "验证服务运行",
					Command:         "systemctl",
					Args:            []string{"is-active", "nginx"},
					ExpectedResult:  "active",
					RollbackCommand: "",
					Timeout:         5,
					Critical:        true,
				},
			}
		}
	} else if strings.Contains(lower, "扩容") || strings.Contains(lower, "scale") {
		steps = []ExecutionStep{
			{
				StepID:          1,
				Description:     "获取当前副本数",
				Command:         "kubectl",
				Args:            []string{"get", "deployment", "<deployment>", "-n", "default", "-o", "jsonpath='{.spec.replicas}'"},
				ExpectedResult:  "当前副本数",
				RollbackCommand: "",
				Timeout:         10,
				Critical:        false,
			},
			{
				StepID:          2,
				Description:     "扩容到指定副本数",
				Command:         "kubectl",
				Args:            []string{"scale", "deployment", "<deployment>", "-n", "default", "--replicas=<count>"},
				ExpectedResult:  "扩容成功",
				RollbackCommand: "kubectl",
				RollbackArgs:    []string{"scale", "deployment", "<deployment>", "-n", "default", "--replicas=<original>"},
				Timeout:         30,
				Critical:        true,
			},
			{
				StepID:          3,
				Description:     "验证副本就绪",
				Command:         "kubectl",
				Args:            []string{"rollout", "status", "deployment", "<deployment>", "-n", "default"},
				ExpectedResult:  "所有副本就绪",
				RollbackCommand: "",
				Timeout:         60,
				Critical:        true,
			},
		}
	} else {
		// 通用模板
		steps = []ExecutionStep{
			{
				StepID:          1,
				Description:     "执行操作",
				Command:         "echo",
				Args:            []string{"Executing:", intent},
				ExpectedResult:  "操作完成",
				RollbackCommand: "",
				Timeout:         10,
				Critical:        false,
			},
		}
	}

	totalTime := 0
	for _, step := range steps {
		totalTime += step.Timeout
	}

	return &ExecutionPlan{
		PlanID:        fmt.Sprintf("plan_template_%d", len(intent)),
		Description:   fmt.Sprintf("执行：%s", intent),
		Steps:         steps,
		TotalSteps:    len(steps),
		EstimatedTime: totalTime,
	}
}

// validatePlanSafety 验证计划安全性
func (t *GeneratePlanTool) validatePlanSafety(plan *ExecutionPlan) error {
	// 危险命令黑名单
	dangerousCommands := []string{
		"rm -rf /",
		"dd if=/dev/zero",
		"mkfs",
		":(){ :|:& };:",
		"chmod -R 777 /",
	}

	for _, step := range plan.Steps {
		fullCmd := step.Command + " " + strings.Join(step.Args, " ")

		// 检查黑名单
		for _, dangerous := range dangerousCommands {
			if strings.Contains(fullCmd, dangerous) {
				return fmt.Errorf("dangerous command detected in step %d: %s", step.StepID, dangerous)
			}
		}

		// 检查必要字段
		if step.Command == "" {
			return fmt.Errorf("step %d missing command", step.StepID)
		}
		if step.Description == "" {
			return fmt.Errorf("step %d missing description", step.StepID)
		}
	}

	return nil
}

// assessRiskLevel 评估风险等级
func (t *GeneratePlanTool) assessRiskLevel(plan *ExecutionPlan) string {
	riskScore := 0

	for _, step := range plan.Steps {
		// 关键步骤增加风险
		if step.Critical {
			riskScore += 2
		}

		// 没有回滚命令增加风险
		if step.RollbackCommand == "" && step.Critical {
			riskScore += 1
		}

		// 危险命令增加风险
		lower := strings.ToLower(step.Command)
		if strings.Contains(lower, "delete") || strings.Contains(lower, "rm") {
			riskScore += 2
		}
		if strings.Contains(lower, "restart") || strings.Contains(lower, "stop") {
			riskScore += 1
		}
	}

	// 步骤数量多也增加风险
	if plan.TotalSteps > 5 {
		riskScore += 1
	}

	if riskScore >= 5 {
		return "high"
	} else if riskScore >= 3 {
		return "medium"
	}
	return "low"
}
