package supervisor

import (
	"context"
	"strings"

	"go_agent/internal/context"
)

// AgentRouter Agent 路由器
type AgentRouter struct {
	// 意图关键词映射
	intentKeywords map[string][]string
}

// NewAgentRouter 创建路由器
func NewAgentRouter() *AgentRouter {
	return &AgentRouter{
		intentKeywords: map[string][]string{
			"monitor": {
				"监控", "查看", "状态", "指标", "cpu", "内存", "磁盘",
				"pod", "容器", "服务", "健康", "运行",
			},
			"diagnose": {
				"故障", "问题", "错误", "异常", "报错", "失败", "超时",
				"慢", "卡", "不可用", "宕机", "崩溃", "诊断", "分析",
			},
			"execute": {
				"重启", "扩容", "缩容", "部署", "回滚", "执行", "操作",
				"修复", "处理", "解决",
			},
			"knowledge": {
				"历史", "案例", "文档", "经验", "之前", "类似", "搜索",
				"查询", "怎么", "如何",
			},
		},
	}
}

// ClassifyIntent 分类用户意图
func (r *AgentRouter) ClassifyIntent(ctx context.Context, session *context.SessionContext, input string) (*context.UserIntent, error) {
	input = strings.ToLower(input)

	// 简单的关键词匹配（后续可以用 LLM 增强）
	scores := make(map[string]int)

	for intentType, keywords := range r.intentKeywords {
		score := 0
		for _, keyword := range keywords {
			if strings.Contains(input, keyword) {
				score++
			}
		}
		scores[intentType] = score
	}

	// 找出得分最高的意图
	maxScore := 0
	intentType := "general"
	for t, score := range scores {
		if score > maxScore {
			maxScore = score
			intentType = t
		}
	}

	// 计算置信度
	confidence := 0.5
	if maxScore > 0 {
		confidence = float64(maxScore) / 10.0
		if confidence > 1.0 {
			confidence = 1.0
		}
	}

	intent := &context.UserIntent{
		Type:       intentType,
		Confidence: confidence,
		Entities:   make(map[string]interface{}),
		Converged:  confidence > 0.7,
		Entropy:    1.0 - confidence,
	}

	// 更新会话意图
	session.Intent = intent

	return intent, nil
}

// RouteToAgent 根据意图路由到对应的 Agent
func (r *AgentRouter) RouteToAgent(intent *context.UserIntent) string {
	switch intent.Type {
	case "monitor":
		return "ops"
	case "diagnose":
		return "rca"
	case "execute":
		return "execution"
	case "knowledge":
		return "knowledge"
	default:
		return "dialogue"
	}
}
