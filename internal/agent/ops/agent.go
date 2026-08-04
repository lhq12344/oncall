package ops

import (
	"context"
	"fmt"
	"strings"

	"go_agent/internal/ai/models"
	"go_agent/internal/prompt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Config Ops Agent 配置
type Config struct {
	ChatModel     *models.ChatModel
	KubeConfig    string // 保留配置结构，避免上层初始化联动修改
	PrometheusURL string
	Logger        *zap.Logger
}

// NewOpsAgent 创建 Ops Agent（修复策略规划）。
// 输入：ctx、配置。
// 输出：仅负责输出结构化 RemediationProposal 的 Agent。
func NewOpsAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	env := prompt.DetectEnvironment("")
	instruction := prompt.BuildAgentPrompt(prompt.RoleOps, env, prompt.BuildOptions{})

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "ops_agent",
		Description:   "基于 RCA 与执行反馈生成修复策略提案的运维代理",
		Model:         cfg.ChatModel.Client,
		GenModelInput: noFormatGenModelInput,
		Instruction:   instruction,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ops agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("ops agent initialized as remediation planner")
	}
	return agent, nil
}

// noFormatGenModelInput 构建模型输入消息，不对 instruction 执行 FString 变量替换。
// 输入：instruction 系统提示词，input 用户/历史消息。
// 输出：拼接后的模型消息列表（system + input.Messages）。
func noFormatGenModelInput(_ context.Context, instruction string, input *adk.AgentInput) ([]adk.Message, error) {
	msgs := make([]adk.Message, 0, 1)
	if strings.TrimSpace(instruction) != "" {
		msgs = append(msgs, schema.SystemMessage(instruction))
	}
	if input != nil && len(input.Messages) > 0 {
		msgs = append(msgs, input.Messages...)
	}
	return msgs, nil
}
