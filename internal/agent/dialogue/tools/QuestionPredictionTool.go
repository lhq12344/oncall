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

// QuestionPredictionTool 问题预测工具
type QuestionPredictionTool struct {
	chatModel *models.ChatModel
	logger    *zap.Logger
}

func NewQuestionPredictionTool(chatModel *models.ChatModel, logger *zap.Logger) tool.BaseTool {
	return &QuestionPredictionTool{
		chatModel: chatModel,
		logger:    logger,
	}
}

func (t *QuestionPredictionTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "question_prediction",
		Desc: "预测用户下一步可能提出的问题。基于当前对话上下文，生成 3-5 个候选问题。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"context": {
				Type:     schema.String,
				Desc:     "当前对话上下文",
				Required: true,
			},
			"count": {
				Type:     schema.Integer,
				Desc:     "生成问题数量（默认 3）",
				Required: false,
			},
		}),
	}, nil
}

func (t *QuestionPredictionTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Context string `json:"context"`
		Count   int    `json:"count"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	in.Context = strings.TrimSpace(in.Context)
	if in.Context == "" {
		return "", fmt.Errorf("context is required")
	}

	if in.Count <= 0 {
		in.Count = 3
	}
	if in.Count > 5 {
		in.Count = 5
	}

	// 1. 尝试使用 LLM 生成上下文相关的问题
	method := "llm"
	questions, err := t.llmBasedPrediction(ctx, in.Context, in.Count)
	if err != nil {
		if t.logger != nil {
			t.logger.Warn("LLM prediction failed, fallback to template",
				zap.Error(err))
		}
		// 降级到模板问题
		questions = t.templateBasedPrediction(in.Context, in.Count)
		method = "template"
	} else if len(questions) == 0 {
		// LLM 返回空结果时也降级，保证始终有可用问题建议
		questions = t.templateBasedPrediction(in.Context, in.Count)
		method = "template"
	}

	result := map[string]interface{}{
		"questions": questions,
		"count":     len(questions),
		"method":    method,
	}

	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("question prediction completed",
			zap.Int("count", len(questions)))
	}

	return string(out), nil
}

// llmBasedPrediction 使用 LLM 生成上下文相关的问题
func (t *QuestionPredictionTool) llmBasedPrediction(ctx context.Context, context string, count int) ([]string, error) {
	if t.chatModel == nil {
		return nil, fmt.Errorf("chat model not available")
	}

	prompt := fmt.Sprintf(`基于以下对话上下文，预测用户接下来可能提出的 %d 个问题。
							这些问题应该：
							1. 与当前上下文紧密相关
							2. 具有引导性，帮助用户快速定位问题
							3. 按照重要性排序

							对话上下文：
							%s

							请只返回 JSON 数组格式：["问题1", "问题2", "问题3"]`, count, context)

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

	// 尝试提取 JSON 数组
	start := strings.Index(content, "[")
	end := strings.LastIndex(content, "]")
	if start >= 0 && end > start {
		jsonStr := content[start : end+1]
		var questions []string
		if err := json.Unmarshal([]byte(jsonStr), &questions); err == nil {
			// 限制数量
			if len(questions) > count {
				questions = questions[:count]
			}
			return questions, nil
		}
	}

	return nil, fmt.Errorf("failed to parse LLM response")
}

// templateBasedPrediction 基于模板的问题预测（降级方案）
func (t *QuestionPredictionTool) templateBasedPrediction(context string, count int) []string {
	lower := strings.ToLower(context)

	// 根据上下文关键词选择问题模板
	var templates []string

	if strings.Contains(lower, "故障") || strings.Contains(lower, "异常") || strings.Contains(lower, "报错") {
		templates = []string{
			"故障是从什么时候开始的？",
			"有看到具体的错误信息吗？",
			"影响范围有多大？有多少用户受影响？",
			"最近是否有发布或配置变更？",
			"是否能提供相关日志或监控截图？",
		}
	} else if strings.Contains(lower, "监控") || strings.Contains(lower, "指标") || strings.Contains(lower, "cpu") || strings.Contains(lower, "内存") {
		templates = []string{
			"需要查看哪个服务或 Pod 的监控数据？",
			"关注哪些具体指标？CPU、内存还是网络？",
			"需要查看多长时间范围的数据？",
			"是否有告警触发？",
			"是否需要对比历史数据？",
		}
	} else if strings.Contains(lower, "重启") || strings.Contains(lower, "执行") || strings.Contains(lower, "修复") {
		templates = []string{
			"确认要执行这个操作吗？",
			"是否需要先备份当前配置？",
			"操作的目标是哪个服务或 Pod？",
			"是否需要通知相关人员？",
			"是否有回滚预案？",
		}
	} else if strings.Contains(lower, "案例") || strings.Contains(lower, "知识") || strings.Contains(lower, "文档") {
		templates = []string{
			"需要查找哪方面的案例？",
			"是否有具体的关键词或错误信息？",
			"希望了解解决方案还是预防措施？",
			"是否需要相关的最佳实践？",
		}
	} else {
		// 通用问题
		templates = []string{
			"能否提供更多详细信息？",
			"这个问题是什么时候发现的？",
			"是否影响了业务？",
			"需要我帮你做什么？",
			"是否需要查看相关监控或日志？",
		}
	}

	// 返回指定数量的问题
	if len(templates) > count {
		return templates[:count]
	}
	return templates
}
