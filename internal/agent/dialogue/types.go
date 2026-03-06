package dialogue

import (
	"time"
)

// IntentAnalysis 意图分析结果
type IntentAnalysis struct {
	Intent     *Intent   `json:"intent"`     // 意图
	Entropy    float64   `json:"entropy"`    // 语义熵
	Converged  bool      `json:"converged"`  // 是否收敛
	Confidence float64   `json:"confidence"` // 置信度
	Timestamp  time.Time `json:"timestamp"`  // 时间戳
}

// Intent 意图
type Intent struct {
	Type       string                 `json:"type"`       // 意图类型
	Confidence float64                `json:"confidence"` // 置信度
	Entities   map[string]interface{} `json:"entities"`   // 实体
	SubType    string                 `json:"sub_type"`   // 子类型（可选）
}

// DialogueState 对话状态
type DialogueState struct {
	SessionID    string                 `json:"session_id"`    // 会话 ID
	CurrentState string                 `json:"current_state"` // 当前状态
	PrevState    string                 `json:"prev_state"`    // 前一个状态
	Transitions  []StateTransition      `json:"transitions"`   // 状态转移历史
	Metadata     map[string]interface{} `json:"metadata"`      // 元数据
	UpdatedAt    time.Time              `json:"updated_at"`    // 更新时间
}

// StateTransition 状态转移
type StateTransition struct {
	FromState string    `json:"from_state"` // 源状态
	ToState   string    `json:"to_state"`   // 目标状态
	Trigger   string    `json:"trigger"`    // 触发条件
	Timestamp time.Time `json:"timestamp"`  // 时间戳
}

// PredictedQuestion 预测的问题
type PredictedQuestion struct {
	Question   string    `json:"question"`   // 问题文本
	Score      float64   `json:"score"`      // 评分
	Reason     string    `json:"reason"`     // 推荐理由
	Category   string    `json:"category"`   // 类别
	Timestamp  time.Time `json:"timestamp"`  // 时间戳
}

// ConversationVector 对话向量
type ConversationVector struct {
	SessionID string    `json:"session_id"` // 会话 ID
	Vector    []float32 `json:"vector"`     // 向量
	Timestamp time.Time `json:"timestamp"`  // 时间戳
}

// EntropyResult 熵计算结果
type EntropyResult struct {
	Entropy    float64   `json:"entropy"`    // 熵值
	Similarity float64   `json:"similarity"` // 相似度
	Converged  bool      `json:"converged"`  // 是否收敛
	Timestamp  time.Time `json:"timestamp"`  // 时间戳
}

// QuestionGenerationRequest 问题生成请求
type QuestionGenerationRequest struct {
	SessionID string `json:"session_id"` // 会话 ID
	Count     int    `json:"count"`      // 生成数量
	Category  string `json:"category"`   // 类别（可选）
}

// QuestionGenerationResponse 问题生成响应
type QuestionGenerationResponse struct {
	Questions []string      `json:"questions"` // 问题列表
	Duration  time.Duration `json:"duration"`  // 耗时
}

// EntityExtractionResult 实体提取结果
type EntityExtractionResult struct {
	Entities  map[string]interface{} `json:"entities"`  // 实体
	Timestamp time.Time              `json:"timestamp"` // 时间戳
}
