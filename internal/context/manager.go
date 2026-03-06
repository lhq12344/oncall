package context

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ContextManager 上下文管理器
type ContextManager struct {
	global  *GlobalContext
	storage Storage // 存储层抽象
}

// NewContextManager 创建上下文管理器
func NewContextManager(storage Storage) *ContextManager {
	return &ContextManager{
		global:  NewGlobalContext(),
		storage: storage,
	}
}

// CreateSession 创建新会话
func (cm *ContextManager) CreateSession(ctx context.Context, userID string) (*SessionContext, error) {
	session := &SessionContext{
		SessionID:          uuid.New().String(),
		UserID:             userID,
		CreatedAt:          time.Now(),
		LastActive:         time.Now(),
		History:            make([]*Message, 0),
		Metadata:           make(map[string]interface{}),
		PredictedQuestions: make([]string, 0),
	}

	// 存入内存（L1）
	cm.global.sessions.Store(session.SessionID, session)

	return session, nil
}

// GetSession 获取会话
func (cm *ContextManager) GetSession(ctx context.Context, sessionID string) (*SessionContext, error) {
	// 先从内存（L1）查找
	if val, ok := cm.global.sessions.Load(sessionID); ok {
		session := val.(*SessionContext)
		session.mu.Lock()
		session.LastActive = time.Now()
		session.mu.Unlock()
		return session, nil
	}

	// 从存储层（L2）恢复
	session, err := cm.storage.LoadSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// 加载到内存
	cm.global.sessions.Store(sessionID, session)

	return session, nil
}

// UpdateSession 更新会话
func (cm *ContextManager) UpdateSession(ctx context.Context, session *SessionContext) error {
	session.mu.Lock()
	session.LastActive = time.Now()
	session.mu.Unlock()

	// 更新内存
	cm.global.sessions.Store(session.SessionID, session)

	return nil
}

// DeleteSession 删除会话
func (cm *ContextManager) DeleteSession(ctx context.Context, sessionID string) error {
	// 从内存删除
	cm.global.sessions.Delete(sessionID)

	// 从存储层删除
	return cm.storage.DeleteSession(ctx, sessionID)
}

// AddMessage 添加消息到会话
func (cm *ContextManager) AddMessage(ctx context.Context, sessionID string, role, content string) error {
	session, err := cm.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

	message := &Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	session.mu.Lock()
	session.History = append(session.History, message)
	session.LastActive = time.Now()
	session.mu.Unlock()

	return cm.UpdateSession(ctx, session)
}

// GetHistory 获取对话历史
func (cm *ContextManager) GetHistory(ctx context.Context, sessionID string, limit int) ([]*Message, error) {
	session, err := cm.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	session.mu.RLock()
	defer session.mu.RUnlock()

	history := session.History
	if limit > 0 && len(history) > limit {
		history = history[len(history)-limit:]
	}

	return history, nil
}

// CreateAgentContext 创建 Agent 上下文
func (cm *ContextManager) CreateAgentContext(ctx context.Context, agentType string) (*AgentContext, error) {
	agentCtx := &AgentContext{
		AgentID:             uuid.New().String(),
		AgentType:           agentType,
		State:               "idle",
		ToolCallHistory:     make([]*ToolCall, 0),
		IntermediateResults: make(map[string]interface{}),
		ErrorStack:          make([]error, 0),
		StartTime:           time.Now(),
	}

	cm.global.agents.Store(agentCtx.AgentID, agentCtx)

	return agentCtx, nil
}

// GetAgentContext 获取 Agent 上下文
func (cm *ContextManager) GetAgentContext(ctx context.Context, agentID string) (*AgentContext, error) {
	if val, ok := cm.global.agents.Load(agentID); ok {
		return val.(*AgentContext), nil
	}
	return nil, fmt.Errorf("agent context not found: %s", agentID)
}

// UpdateAgentState 更新 Agent 状态
func (cm *ContextManager) UpdateAgentState(ctx context.Context, agentID, state string) error {
	agentCtx, err := cm.GetAgentContext(ctx, agentID)
	if err != nil {
		return err
	}

	agentCtx.mu.Lock()
	agentCtx.State = state
	agentCtx.mu.Unlock()

	return nil
}

// AddToolCall 添加工具调用记录
func (cm *ContextManager) AddToolCall(ctx context.Context, agentID string, toolCall *ToolCall) error {
	agentCtx, err := cm.GetAgentContext(ctx, agentID)
	if err != nil {
		return err
	}

	agentCtx.mu.Lock()
	agentCtx.ToolCallHistory = append(agentCtx.ToolCallHistory, toolCall)
	agentCtx.mu.Unlock()

	return nil
}

// CreateExecutionContext 创建执行上下文
func (cm *ContextManager) CreateExecutionContext(ctx context.Context, planID string) (*ExecutionContext, error) {
	execCtx := &ExecutionContext{
		ExecutionID:       uuid.New().String(),
		PlanID:            planID,
		TaskQueue:         make([]*Task, 0),
		ExecutionLog:      make([]*LogEntry, 0),
		RollbackStack:     make([]*RollbackEntry, 0),
		ValidationResults: make(map[string]bool),
		Status:            "pending",
	}

	cm.global.executions.Store(execCtx.ExecutionID, execCtx)

	return execCtx, nil
}

// GetExecutionContext 获取执行上下文
func (cm *ContextManager) GetExecutionContext(ctx context.Context, executionID string) (*ExecutionContext, error) {
	if val, ok := cm.global.executions.Load(executionID); ok {
		return val.(*ExecutionContext), nil
	}
	return nil, fmt.Errorf("execution context not found: %s", executionID)
}

// AddExecutionLog 添加执行日志
func (cm *ContextManager) AddExecutionLog(ctx context.Context, executionID, level, message string, metadata map[string]interface{}) error {
	execCtx, err := cm.GetExecutionContext(ctx, executionID)
	if err != nil {
		return err
	}

	logEntry := &LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
		Metadata:  metadata,
	}

	execCtx.mu.Lock()
	execCtx.ExecutionLog = append(execCtx.ExecutionLog, logEntry)
	execCtx.mu.Unlock()

	return nil
}

// MigrateToL2 迁移不活跃会话到 L2（Redis）
func (cm *ContextManager) MigrateToL2(ctx context.Context) error {
	now := time.Now()
	threshold := 30 * time.Minute

	cm.global.sessions.Range(func(key, value interface{}) bool {
		session := value.(*SessionContext)
		session.mu.RLock()
		inactive := now.Sub(session.LastActive) > threshold
		session.mu.RUnlock()

		if inactive {
			// 序列化并存入 Redis
			data, err := json.Marshal(session)
			if err == nil {
				cm.storage.SaveSession(ctx, session.SessionID, data, 24*time.Hour)
			}

			// 从内存中删除
			cm.global.sessions.Delete(key)
		}

		return true
	})

	return nil
}

// ListActiveSessions 列出活跃会话
func (cm *ContextManager) ListActiveSessions(ctx context.Context) []*SessionContext {
	sessions := make([]*SessionContext, 0)

	cm.global.sessions.Range(func(key, value interface{}) bool {
		session := value.(*SessionContext)
		sessions = append(sessions, session)
		return true
	})

	return sessions
}
