package dialogue

import (
	"context"
	"sync"
	"time"
)

// DialogueStateTracker 对话状态跟踪器
type DialogueStateTracker struct {
	states sync.Map // sessionID -> *DialogueState
}

// NewDialogueStateTracker 创建对话状态跟踪器
func NewDialogueStateTracker() *DialogueStateTracker {
	return &DialogueStateTracker{}
}

// UpdateState 更新对话状态
func (t *DialogueStateTracker) UpdateState(ctx context.Context, sessionID string, state *DialogueState) error {
	// 获取当前状态
	currentState, _ := t.GetState(ctx, sessionID)

	// 记录状态转移
	if currentState != nil && currentState.CurrentState != state.CurrentState {
		transition := StateTransition{
			FromState: currentState.CurrentState,
			ToState:   state.CurrentState,
			Trigger:   "user_input", // 可以根据实际情况设置
			Timestamp: time.Now(),
		}
		state.Transitions = append(currentState.Transitions, transition)
		state.PrevState = currentState.CurrentState
	}

	state.SessionID = sessionID
	state.UpdatedAt = time.Now()

	// 保存状态
	t.states.Store(sessionID, state)

	return nil
}

// GetState 获取对话状态
func (t *DialogueStateTracker) GetState(ctx context.Context, sessionID string) (*DialogueState, error) {
	if val, ok := t.states.Load(sessionID); ok {
		return val.(*DialogueState), nil
	}

	// 返回初始状态
	return &DialogueState{
		SessionID:    sessionID,
		CurrentState: "initial",
		PrevState:    "",
		Transitions:  make([]StateTransition, 0),
		Metadata:     make(map[string]interface{}),
		UpdatedAt:    time.Now(),
	}, nil
}

// DeleteState 删除对话状态
func (t *DialogueStateTracker) DeleteState(ctx context.Context, sessionID string) error {
	t.states.Delete(sessionID)
	return nil
}

// ListStates 列出所有状态
func (t *DialogueStateTracker) ListStates(ctx context.Context) []*DialogueState {
	states := make([]*DialogueState, 0)

	t.states.Range(func(key, value interface{}) bool {
		state := value.(*DialogueState)
		states = append(states, state)
		return true
	})

	return states
}

// GetStateHistory 获取状态转移历史
func (t *DialogueStateTracker) GetStateHistory(ctx context.Context, sessionID string) ([]StateTransition, error) {
	state, err := t.GetState(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return state.Transitions, nil
}

// IsInState 检查是否处于特定状态
func (t *DialogueStateTracker) IsInState(ctx context.Context, sessionID string, stateName string) (bool, error) {
	state, err := t.GetState(ctx, sessionID)
	if err != nil {
		return false, err
	}

	return state.CurrentState == stateName, nil
}

// TransitionTo 转移到新状态
func (t *DialogueStateTracker) TransitionTo(ctx context.Context, sessionID string, newState string, trigger string) error {
	state, err := t.GetState(ctx, sessionID)
	if err != nil {
		return err
	}

	// 记录转移
	transition := StateTransition{
		FromState: state.CurrentState,
		ToState:   newState,
		Trigger:   trigger,
		Timestamp: time.Now(),
	}

	state.PrevState = state.CurrentState
	state.CurrentState = newState
	state.Transitions = append(state.Transitions, transition)
	state.UpdatedAt = time.Now()

	// 保存状态
	t.states.Store(sessionID, state)

	return nil
}

// GetMetadata 获取元数据
func (t *DialogueStateTracker) GetMetadata(ctx context.Context, sessionID string, key string) (interface{}, bool) {
	state, err := t.GetState(ctx, sessionID)
	if err != nil {
		return nil, false
	}

	val, ok := state.Metadata[key]
	return val, ok
}

// SetMetadata 设置元数据
func (t *DialogueStateTracker) SetMetadata(ctx context.Context, sessionID string, key string, value interface{}) error {
	state, err := t.GetState(ctx, sessionID)
	if err != nil {
		return err
	}

	state.Metadata[key] = value
	state.UpdatedAt = time.Now()

	t.states.Store(sessionID, state)

	return nil
}

// CleanupOldStates 清理旧状态
func (t *DialogueStateTracker) CleanupOldStates(ctx context.Context, olderThan time.Duration) int {
	count := 0
	now := time.Now()

	t.states.Range(func(key, value interface{}) bool {
		state := value.(*DialogueState)
		if now.Sub(state.UpdatedAt) > olderThan {
			t.states.Delete(key)
			count++
		}
		return true
	})

	return count
}
