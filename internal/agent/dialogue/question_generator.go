package dialogue

import (
	"context"
	"fmt"
	"strings"

	appcontext "go_agent/internal/context"
)

// QuestionGenerator 问题生成器
type QuestionGenerator struct {
	// 预定义的问题模板
	templates map[string][]string
}

// NewQuestionGenerator 创建问题生成器
func NewQuestionGenerator() *QuestionGenerator {
	return &QuestionGenerator{
		templates: map[string][]string{
			"monitor": {
				"需要查看哪个服务的监控数据？",
				"想了解哪个指标的情况？（CPU、内存、磁盘等）",
				"需要查看多长时间范围的数据？",
			},
			"diagnose": {
				"故障是从什么时候开始的？",
				"有看到具体的错误信息吗？",
				"影响范围有多大？有多少用户受影响？",
				"最近有做过什么变更吗？",
			},
			"execute": {
				"确定要执行这个操作吗？",
				"需要在哪个环境执行？（生产/测试）",
				"是否需要先备份数据？",
			},
			"knowledge": {
				"想了解哪方面的历史案例？",
				"遇到的问题和之前的案例有什么不同？",
				"需要更详细的解决步骤吗？",
			},
		},
	}
}

// Generate 生成候选问题
func (q *QuestionGenerator) Generate(ctx context.Context, session *appcontext.SessionContext, count int) ([]string, error) {
	session.mu.RLock()
	defer session.mu.RUnlock()

	if session.Intent == nil {
		return []string{}, nil
	}

	intentType := session.Intent.Type

	// 获取该意图类型的问题模板
	templates, ok := q.templates[intentType]
	if !ok {
		templates = q.templates["monitor"] // 默认使用监控类问题
	}

	// 根据对话历史过滤已问过的问题
	askedQuestions := make(map[string]bool)
	for _, msg := range session.History {
		if msg.Role == "assistant" {
			askedQuestions[msg.Content] = true
		}
	}

	// 选择未问过的问题
	questions := make([]string, 0, count)
	for _, template := range templates {
		if !askedQuestions[template] && len(questions) < count {
			questions = append(questions, template)
		}
	}

	// 如果不够，生成基于上下文的问题
	if len(questions) < count {
		contextQuestions := q.generateContextualQuestions(session, count-len(questions))
		questions = append(questions, contextQuestions...)
	}

	return questions, nil
}

// generateContextualQuestions 生成基于上下文的问题
func (q *QuestionGenerator) generateContextualQuestions(session *appcontext.SessionContext, count int) []string {
	questions := make([]string, 0, count)

	// 分析最近的对话，生成相关问题
	if len(session.History) > 0 {
		lastMessage := session.History[len(session.History)-1]

		// 如果提到了服务名，询问更多细节
		if containsServiceName(lastMessage.Content) {
			questions = append(questions, "这个服务的日志有什么异常吗？")
			if len(questions) >= count {
				return questions
			}
			questions = append(questions, "需要查看这个服务的依赖关系吗？")
			if len(questions) >= count {
				return questions
			}
		}

		// 如果提到了错误，询问更多信息
		if containsErrorKeywords(lastMessage.Content) {
			questions = append(questions, "错误是偶发的还是持续的？")
			if len(questions) >= count {
				return questions
			}
			questions = append(questions, "有尝试过什么解决方法吗？")
			if len(questions) >= count {
				return questions
			}
		}
	}

	// 通用问题
	if len(questions) < count {
		questions = append(questions, "还需要了解其他信息吗？")
	}

	return questions
}

// GenerateClarification 生成澄清性问题
func (q *QuestionGenerator) GenerateClarification(ctx context.Context, session *appcontext.SessionContext) string {
	session.mu.RLock()
	defer session.mu.RUnlock()

	if session.Intent == nil {
		return "能否详细描述一下您遇到的问题？"
	}

	// 根据意图类型生成澄清问题
	switch session.Intent.Type {
	case "monitor":
		return "您想查看哪个服务或指标的监控数据？"
	case "diagnose":
		return "能否提供更多关于故障的信息？比如错误信息、发生时间等。"
	case "execute":
		return "请确认要执行的具体操作和目标环境。"
	case "knowledge":
		return "您想了解哪方面的历史案例或最佳实践？"
	default:
		return "能否更具体地说明您的需求？"
	}
}

// GenerateFollowUp 生成跟进问题
func (q *QuestionGenerator) GenerateFollowUp(ctx context.Context, session *appcontext.SessionContext, lastResponse string) []string {
	questions := make([]string, 0, 3)

	// 根据最后的回复生成跟进问题
	if strings.Contains(lastResponse, "成功") {
		questions = append(questions, "需要验证一下结果吗？")
		questions = append(questions, "还有其他需要处理的吗？")
	} else if strings.Contains(lastResponse, "失败") || strings.Contains(lastResponse, "错误") {
		questions = append(questions, "需要查看详细的错误日志吗？")
		questions = append(questions, "要尝试其他解决方案吗？")
	} else {
		questions = append(questions, "这个信息对您有帮助吗？")
		questions = append(questions, "需要更详细的说明吗？")
	}

	return questions
}

// containsServiceName 检查是否包含服务名
func containsServiceName(text string) bool {
	serviceNames := []string{"nginx", "mysql", "redis", "kafka", "etcd", "kubernetes", "docker"}
	text = strings.ToLower(text)
	for _, service := range serviceNames {
		if strings.Contains(text, service) {
			return true
		}
	}
	return false
}

// containsErrorKeywords 检查是否包含错误关键词
func containsErrorKeywords(text string) bool {
	errorKeywords := []string{"错误", "异常", "失败", "超时", "崩溃", "宕机"}
	text = strings.ToLower(text)
	for _, keyword := range errorKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

// GenerateByCategory 按类别生成问题
func (q *QuestionGenerator) GenerateByCategory(ctx context.Context, category string, count int) []string {
	templates, ok := q.templates[category]
	if !ok {
		return []string{}
	}

	if len(templates) <= count {
		return templates
	}

	return templates[:count]
}

// AddTemplate 添加问题模板
func (q *QuestionGenerator) AddTemplate(category string, template string) {
	if _, ok := q.templates[category]; !ok {
		q.templates[category] = make([]string, 0)
	}
	q.templates[category] = append(q.templates[category], template)
}

// GetTemplates 获取所有模板
func (q *QuestionGenerator) GetTemplates() map[string][]string {
	return q.templates
}

// GenerateWithLLM 使用 LLM 生成问题（TODO）
func (q *QuestionGenerator) GenerateWithLLM(ctx context.Context, session *appcontext.SessionContext, count int) ([]string, error) {
	// TODO: 集成 LLM 生成更智能的问题
	// 1. 构建 prompt（包含对话历史和当前意图）
	// 2. 调用 LLM API
	// 3. 解析返回的问题列表

	return nil, fmt.Errorf("LLM generation not implemented yet")
}
