package knowledge

import (
	"time"

	"github.com/cloudwego/eino/schema"
)

// KnowledgeCase 知识案例
type KnowledgeCase struct {
	ID          string                 `json:"id"`           // 案例 ID
	Title       string                 `json:"title"`        // 标题
	Content     string                 `json:"content"`      // 内容
	Solution    string                 `json:"solution"`     // 解决方案
	Score       float64                `json:"score"`        // 相似度评分
	QualityScore float64               `json:"quality_score"` // 质量评分
	Tags        []string               `json:"tags"`         // 标签
	Metadata    map[string]interface{} `json:"metadata"`     // 元数据
	RetrievedAt time.Time              `json:"retrieved_at"` // 检索时间
	UsageCount  int                    `json:"usage_count"`  // 使用次数
	SuccessRate float64                `json:"success_rate"` // 成功率
}

// Feedback 用户反馈
type Feedback struct {
	CaseID    string    `json:"case_id"`    // 案例 ID
	UserID    string    `json:"user_id"`    // 用户 ID
	Helpful   bool      `json:"helpful"`    // 是否有帮助
	Rating    int       `json:"rating"`     // 评分（1-5）
	Comment   string    `json:"comment"`    // 评论
	Timestamp time.Time `json:"timestamp"`  // 时间戳
}

// ExecutionLog 执行日志（用于提取成功路径）
type ExecutionLog struct {
	ExecutionID string        `json:"execution_id"` // 执行 ID
	Problem     string        `json:"problem"`      // 问题描述
	Solution    string        `json:"solution"`     // 解决方案
	Steps       []string      `json:"steps"`        // 执行步骤
	Duration    time.Duration `json:"duration"`     // 执行时长
	Success     bool          `json:"success"`      // 是否成功
	SuccessRate float64       `json:"success_rate"` // 成功率
	Tags        []string      `json:"tags"`         // 标签
	Timestamp   time.Time     `json:"timestamp"`    // 时间戳
}

// SearchRequest 搜索请求
type SearchRequest struct {
	Query     string            `json:"query"`      // 查询文本
	TopK      int               `json:"top_k"`      // 返回数量
	Filters   map[string]string `json:"filters"`    // 过滤条件
	SessionID string            `json:"session_id"` // 会话 ID（可选）
}

// SearchResponse 搜索响应
type SearchResponse struct {
	Cases      []*KnowledgeCase `json:"cases"`       // 案例列表
	TotalCount int              `json:"total_count"` // 总数
	QueryTime  time.Duration    `json:"query_time"`  // 查询耗时
}

// IndexRequest 索引请求
type IndexRequest struct {
	Documents []*schema.Document `json:"documents"` // 文档列表
}

// IndexResponse 索引响应
type IndexResponse struct {
	Success      bool          `json:"success"`       // 是否成功
	IndexedCount int           `json:"indexed_count"` // 索引数量
	Duration     time.Duration `json:"duration"`      // 耗时
}
