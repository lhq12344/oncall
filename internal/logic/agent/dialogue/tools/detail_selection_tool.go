package tools

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

const (
	minDetailSelectionOptions = 2
	maxDetailSelectionOptions = 6
)

func init() {
	gob.Register(&DetailSelectionInterruptInfo{})
}

// DetailSelectionTool 为 dialogue_agent 提供基于单选卡片的细节补充能力。
type DetailSelectionTool struct {
	logger *zap.Logger
}

type DetailSelectionOption struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// DetailSelectionInterruptInfo 定义中断时返回给前端的补充细节信息。
type DetailSelectionInterruptInfo struct {
	Field    string                  `json:"field"`
	Question string                  `json:"question"`
	Reason   string                  `json:"reason,omitempty"`
	Options  []DetailSelectionOption `json:"options"`
}

func (i *DetailSelectionInterruptInfo) String() string {
	if i == nil {
		return "需要补充细节信息，请完成选择后继续。"
	}
	question := strings.TrimSpace(i.Question)
	if question == "" {
		question = "请补充必要细节"
	}
	if reason := strings.TrimSpace(i.Reason); reason != "" {
		return fmt.Sprintf("%s（原因：%s）", question, reason)
	}
	return question
}

type DetailSelectionResult struct {
	Field         string `json:"field"`
	Question      string `json:"question"`
	SelectedValue string `json:"selected_value"`
	SelectedLabel string `json:"selected_label"`
}

// NewDetailSelectionTool 创建单选型细节补充工具。
func NewDetailSelectionTool(logger *zap.Logger) tool.BaseTool {
	return &DetailSelectionTool{logger: logger}
}

func (t *DetailSelectionTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "request_detail_selection",
		Desc: "当缺少关键上下文且候选项有限时，请求用户从给定选项中单选补充细节，例如 namespace、环境、资源类型。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"field": {
				Type:     schema.String,
				Desc:     "缺失字段标识，例如 namespace、environment、resource_type",
				Required: true,
			},
			"question": {
				Type:     schema.String,
				Desc:     "展示给用户的提问，例如：请选择要检查的命名空间",
				Required: true,
			},
			"reason": {
				Type:     schema.String,
				Desc:     "可选，说明为什么需要补充这个细节",
				Required: false,
			},
			"options": {
				Type:     schema.Array,
				Desc:     "候选项列表，必须是 2-6 个单选项",
				Required: true,
				ElemInfo: &schema.ParameterInfo{
					Type: schema.Object,
					SubParams: map[string]*schema.ParameterInfo{
						"label": {
							Type:     schema.String,
							Desc:     "展示给用户的名称",
							Required: true,
						},
						"value": {
							Type:     schema.String,
							Desc:     "回传给工具和模型使用的 canonical 值",
							Required: true,
						},
						"description": {
							Type:     schema.String,
							Desc:     "可选，对该选项的补充说明",
							Required: false,
						},
					},
				},
			},
		}),
	}, nil
}

func (t *DetailSelectionTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Field    string                  `json:"field"`
		Question string                  `json:"question"`
		Reason   string                  `json:"reason"`
		Options  []DetailSelectionOption `json:"options"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	in.Field = strings.TrimSpace(in.Field)
	in.Question = strings.TrimSpace(in.Question)
	in.Reason = strings.TrimSpace(in.Reason)
	normalizedOptions, err := normalizeDetailSelectionOptions(in.Options)
	if err != nil {
		return "", err
	}
	in.Options = normalizedOptions

	if in.Field == "" {
		return "", fmt.Errorf("field is required")
	}
	if in.Question == "" {
		return "", fmt.Errorf("question is required")
	}

	info := &DetailSelectionInterruptInfo{
		Field:    in.Field,
		Question: in.Question,
		Reason:   in.Reason,
		Options:  in.Options,
	}

	wasInterrupted, _, _ := tool.GetInterruptState[any](ctx)
	if !wasInterrupted {
		return "", tool.Interrupt(ctx, info)
	}

	isResumeTarget, hasData, resumeData := tool.GetResumeContext[map[string]any](ctx)
	if !isResumeTarget || !hasData {
		return "", tool.Interrupt(ctx, info)
	}

	selectionValue := parseDetailSelectionValue(resumeData)
	selectedOption, ok := findDetailSelectionOption(in.Options, selectionValue)
	if !ok {
		if t.logger != nil {
			t.logger.Warn("detail selection resume missing or invalid option",
				zap.String("field", in.Field),
				zap.String("selection_value", selectionValue))
		}
		return "", tool.Interrupt(ctx, info)
	}

	result := DetailSelectionResult{
		Field:         in.Field,
		Question:      in.Question,
		SelectedValue: selectedOption.Value,
		SelectedLabel: selectedOption.Label,
	}
	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal detail selection result: %w", err)
	}
	return string(out), nil
}

func normalizeDetailSelectionOptions(options []DetailSelectionOption) ([]DetailSelectionOption, error) {
	if len(options) < minDetailSelectionOptions || len(options) > maxDetailSelectionOptions {
		return nil, fmt.Errorf("options count must be between %d and %d", minDetailSelectionOptions, maxDetailSelectionOptions)
	}

	seen := make(map[string]struct{}, len(options))
	normalized := make([]DetailSelectionOption, 0, len(options))
	for _, option := range options {
		label := strings.TrimSpace(option.Label)
		value := strings.TrimSpace(option.Value)
		description := strings.TrimSpace(option.Description)
		if label == "" {
			return nil, fmt.Errorf("option label is required")
		}
		if value == "" {
			return nil, fmt.Errorf("option value is required")
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("duplicate option value: %s", value)
		}
		seen[value] = struct{}{}
		normalized = append(normalized, DetailSelectionOption{
			Label:       label,
			Value:       value,
			Description: description,
		})
	}
	return normalized, nil
}

func parseDetailSelectionValue(data map[string]any) string {
	if data == nil {
		return ""
	}
	if value, ok := data["selection_value"].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func findDetailSelectionOption(options []DetailSelectionOption, selectionValue string) (DetailSelectionOption, bool) {
	selectionValue = strings.TrimSpace(selectionValue)
	if selectionValue == "" {
		return DetailSelectionOption{}, false
	}
	for _, option := range options {
		if strings.TrimSpace(option.Value) == selectionValue {
			return option, true
		}
	}
	return DetailSelectionOption{}, false
}
