package context

import (
	"sync"
	"time"
)

// SessionContext 会话上下文
type SessionContext struct {
	SessionID   string                 `json:"session_id"`   // 会话唯一标识
	UserID      string                 `json:"user_id"`      // 用户标识
	CreatedAt   time.Time              `json:"created_at"`   // 创建时间
	LastActive  time.Time              `json:"last_active"`  // 最后活跃时间
	History     []*Message             `json:"history"`      // 对话历史
	Intent      *UserIntent            `json:"intent"`       // 用户意图
	Metadata    map[string]interface{} `json:"metadata"`     // 会话元数据
	PredictedQuestions []string        `json:"predicted_questions"` // 预测的候选问题
	mu          sync.RWMutex           // 并发保护
}

// Message 消息结构
type Message struct {
	Role      string    `json:"role"`      // user/assistant/system
	Content   string    `json:"content"`   // 消息内容
	Timestamp time.Time `json:"timestamp"` // 时间戳
	Metadata  map[string]interface{} `json:"metadata,omitempty"` // 元数据
}

// UserIntent 用户意图
type UserIntent struct {
	Type       string                 `json:"type"`        // 意图类型：monitor/diagnose/execute/knowledge
	Confidence float64                `json:"confidence"`  // 置信度
	Entities   map[string]interface{} `json:"entities"`    // 提取的实体
	Converged  bool                   `json:"converged"`   // 意图是否收敛
	Entropy    float64                `json:"entropy"`     // 语义熵
}

// AgentContext Agent 上下文
type AgentContext struct {
	AgentID          string                 `json:"agent_id"`           // Agent 唯一标识
	AgentType        string                 `json:"agent_type"`         // Agent 类型
	State            string                 `json:"state"`              // 状态：idle/running/waiting/error
	ToolCallHistory  []*ToolCall            `json:"tool_call_history"`  // 工具调用历史
	IntermediateResults map[string]interface{} `json:"intermediate_results"` // 中间结果
	ErrorStack       []error                `json:"error_stack"`        // 错误栈
	StartTime        time.Time              `json:"start_time"`         // 开始时间
	mu               sync.RWMutex           // 并发保护
}

// ToolCall 工具调用记录
type ToolCall struct {
	ToolName  string                 `json:"tool_name"`  // 工具名称
	Input     string                 `json:"input"`      // 输入参数
	Output    string                 `json:"output"`     // 输出结果
	Error     string                 `json:"error"`      // 错误信息
	StartTime time.Time              `json:"start_time"` // 开始时间
	Duration  time.Duration          `json:"duration"`   // 执行时长
	Metadata  map[string]interface{} `json:"metadata"`   // 元数据
}

// ExecutionContext 执行上下文
type ExecutionContext struct {
	ExecutionID      string                 `json:"execution_id"`       // 执行唯一标识
	PlanID           string                 `json:"plan_id"`            // 执行计划 ID
	TaskQueue        []*Task                `json:"task_queue"`         // 任务队列
	ExecutionLog     []*LogEntry            `json:"execution_log"`      // 执行日志
	RollbackStack    []*RollbackEntry       `json:"rollback_stack"`     // 回滚栈
	ValidationResults map[string]bool       `json:"validation_results"` // 验证结果
	Status           string                 `json:"status"`             // 状态：pending/running/success/failed
	mu               sync.RWMutex           // 并发保护
}

// Task 任务
type Task struct {
	TaskID      string                 `json:"task_id"`      // 任务 ID
	Type        string                 `json:"type"`         // 任务类型：check/execute/validate
	Action      string                 `json:"action"`       // 具体操作
	Command     string                 `json:"command"`      // 执行命令
	Expected    string                 `json:"expected"`     // 预期结果
	Status      string                 `json:"status"`       // 状态：pending/running/success/failed
	Result      string                 `json:"result"`       // 执行结果
	Error       string                 `json:"error"`        // 错误信息
	StartTime   time.Time              `json:"start_time"`   // 开始时间
	Duration    time.Duration          `json:"duration"`     // 执行时长
	Metadata    map[string]interface{} `json:"metadata"`     // 元数据
}

// LogEntry 日志条目
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"` // 时间戳
	Level     string                 `json:"level"`     // 日志级别：info/warn/error
	Message   string                 `json:"message"`   // 日志消息
	Metadata  map[string]interface{} `json:"metadata"`  // 元数据
}

// RollbackEntry 回滚条目
type RollbackEntry struct {
	TaskID      string    `json:"task_id"`      // 关联的任务 ID
	Action      string    `json:"action"`       // 回滚操作
	Command     string    `json:"command"`      // 回滚命令
	Description string    `json:"description"`  // 描述
	Timestamp   time.Time `json:"timestamp"`    // 时间戳
}

// GlobalContext 全局上下文
type GlobalContext struct {
	sessions   sync.Map // SessionID -> *SessionContext
	agents     sync.Map // AgentID -> *AgentContext
	executions sync.Map // ExecutionID -> *ExecutionContext
	mu         sync.RWMutex
}

// NewGlobalContext 创建全局上下文
func NewGlobalContext() *GlobalContext {
	return &GlobalContext{}
}
