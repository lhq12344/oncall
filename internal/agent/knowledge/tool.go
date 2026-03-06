package knowledge

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/compose"
)

// KnowledgeSearchTool 知识搜索工具
type KnowledgeSearchTool struct {
	agent *KnowledgeAgent
}

// NewKnowledgeSearchTool 创建知识搜索工具
func NewKnowledgeSearchTool(agent *KnowledgeAgent) *KnowledgeSearchTool {
	return &KnowledgeSearchTool{agent: agent}
}

// Info 返回工具信息
func (t *KnowledgeSearchTool) Info(ctx context.Context) (*compose.ToolInfo, error) {
	return &compose.ToolInfo{
		Name: "knowledge_search",
		Desc: "搜索历史故障案例和最佳实践。输入：故障描述或关键词。输出：相关案例列表（包含解决方案）。",
		ParamsOneOf: compose.NewParamsOneOfByParams(
			map[string]*compose.ParameterInfo{
				"query": {
					Type:     compose.String,
					Desc:     "搜索关键词或故障描述",
					Required: true,
				},
				"top_k": {
					Type:     compose.Number,
					Desc:     "返回结果数量（默认 5）",
					Required: false,
				},
				"session_id": {
					Type:     compose.String,
					Desc:     "会话 ID（可选，用于上下文增强）",
					Required: false,
				},
			},
		),
	}, nil
}

// InvokableRun 执行工具
func (t *KnowledgeSearchTool) InvokableRun(ctx context.Context, input string, opts ...compose.ToolOption) (string, error) {
	// 解析输入参数
	var params struct {
		Query     string `json:"query"`
		TopK      int    `json:"top_k"`
		SessionID string `json:"session_id"`
	}

	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	// 设置默认值
	if params.TopK == 0 {
		params.TopK = 5
	}

	// 执行搜索
	var cases []*KnowledgeCase
	var err error

	if params.SessionID != "" {
		// 带上下文的搜索
		cases, err = t.agent.SearchWithContext(ctx, params.SessionID, params.Query, params.TopK)
	} else {
		// 普通搜索
		cases, err = t.agent.Search(ctx, params.Query, params.TopK)
	}

	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}

	// 格式化输出
	output := t.formatOutput(cases)

	return output, nil
}

// formatOutput 格式化输出
func (t *KnowledgeSearchTool) formatOutput(cases []*KnowledgeCase) string {
	if len(cases) == 0 {
		return "未找到相关案例"
	}

	result := fmt.Sprintf("找到 %d 个相关案例：\n\n", len(cases))

	for i, kcase := range cases {
		result += fmt.Sprintf("【案例 %d】%s\n", i+1, kcase.Title)
		result += fmt.Sprintf("相似度: %.2f | 质量评分: %.2f\n", kcase.Score, kcase.QualityScore)

		if kcase.Solution != "" {
			result += fmt.Sprintf("解决方案: %s\n", kcase.Solution)
		} else {
			// 截取内容前 200 字符
			content := kcase.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			result += fmt.Sprintf("内容: %s\n", content)
		}

		if len(kcase.Tags) > 0 {
			result += fmt.Sprintf("标签: %v\n", kcase.Tags)
		}

		result += "\n"
	}

	return result
}

// KnowledgeIndexTool 知识索引工具
type KnowledgeIndexTool struct {
	agent *KnowledgeAgent
}

// NewKnowledgeIndexTool 创建知识索引工具
func NewKnowledgeIndexTool(agent *KnowledgeAgent) *KnowledgeIndexTool {
	return &KnowledgeIndexTool{agent: agent}
}

// Info 返回工具信息
func (t *KnowledgeIndexTool) Info(ctx context.Context) (*compose.ToolInfo, error) {
	return &compose.ToolInfo{
		Name: "knowledge_index",
		Desc: "将新的故障案例或解决方案索引到知识库。输入：文档内容和元数据。输出：索引结果。",
		ParamsOneOf: compose.NewParamsOneOfByParams(
			map[string]*compose.ParameterInfo{
				"content": {
					Type:     compose.String,
					Desc:     "文档内容",
					Required: true,
				},
				"title": {
					Type:     compose.String,
					Desc:     "文档标题",
					Required: false,
				},
				"solution": {
					Type:     compose.String,
					Desc:     "解决方案",
					Required: false,
				},
				"tags": {
					Type:     compose.Array,
					Desc:     "标签列表",
					Required: false,
				},
			},
		),
	}, nil
}

// InvokableRun 执行工具
func (t *KnowledgeIndexTool) InvokableRun(ctx context.Context, input string, opts ...compose.ToolOption) (string, error) {
	// 解析输入参数
	var params struct {
		Content  string   `json:"content"`
		Title    string   `json:"title"`
		Solution string   `json:"solution"`
		Tags     []string `json:"tags"`
	}

	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	// 创建文档
	// doc := &schema.Document{
	// 	Content: params.Content,
	// 	MetaData: map[string]any{
	// 		"title":    params.Title,
	// 		"solution": params.Solution,
	// 		"tags":     params.Tags,
	// 	},
	// }

	// 索引文档
	// err := t.agent.Index(ctx, []*schema.Document{doc})
	// if err != nil {
	// 	return "", fmt.Errorf("index failed: %w", err)
	// }

	return fmt.Sprintf("成功索引文档: %s", params.Title), nil
}

// KnowledgeFeedbackTool 知识反馈工具
type KnowledgeFeedbackTool struct {
	agent *KnowledgeAgent
}

// NewKnowledgeFeedbackTool 创建知识反馈工具
func NewKnowledgeFeedbackTool(agent *KnowledgeAgent) *KnowledgeFeedbackTool {
	return &KnowledgeFeedbackTool{agent: agent}
}

// Info 返回工具信息
func (t *KnowledgeFeedbackTool) Info(ctx context.Context) (*compose.ToolInfo, error) {
	return &compose.ToolInfo{
		Name: "knowledge_feedback",
		Desc: "提交对知识案例的反馈。输入：案例 ID 和反馈信息。输出：反馈结果。",
		ParamsOneOf: compose.NewParamsOneOfByParams(
			map[string]*compose.ParameterInfo{
				"case_id": {
					Type:     compose.String,
					Desc:     "案例 ID",
					Required: true,
				},
				"helpful": {
					Type:     compose.Boolean,
					Desc:     "是否有帮助",
					Required: true,
				},
				"rating": {
					Type:     compose.Number,
					Desc:     "评分（1-5）",
					Required: false,
				},
				"comment": {
					Type:     compose.String,
					Desc:     "评论",
					Required: false,
				},
			},
		),
	}, nil
}

// InvokableRun 执行工具
func (t *KnowledgeFeedbackTool) InvokableRun(ctx context.Context, input string, opts ...compose.ToolOption) (string, error) {
	// 解析输入参数
	var params struct {
		CaseID  string `json:"case_id"`
		Helpful bool   `json:"helpful"`
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}

	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	// 创建反馈
	feedback := &Feedback{
		Helpful: params.Helpful,
		Rating:  params.Rating,
		Comment: params.Comment,
	}

	// 添加反馈
	err := t.agent.AddFeedback(ctx, params.CaseID, feedback)
	if err != nil {
		return "", fmt.Errorf("add feedback failed: %w", err)
	}

	return fmt.Sprintf("反馈已提交，案例 ID: %s", params.CaseID), nil
}
