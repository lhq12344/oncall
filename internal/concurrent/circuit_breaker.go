package concurrent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

var (
	// ErrCircuitOpen 熔断器打开错误
	ErrCircuitOpen = errors.New("circuit breaker is open")
	// ErrTooManyRequests 请求过多错误
	ErrTooManyRequests = errors.New("too many requests")
)

// State 熔断器状态
type State int

const (
	// StateClosed 关闭状态（正常）
	StateClosed State = iota
	// StateOpen 打开状态（熔断）
	StateOpen
	// StateHalfOpen 半开状态（尝试恢复）
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig 熔断器配置
type CircuitBreakerConfig struct {
	Name              string        // 熔断器名称
	MaxRequests       uint32        // 半开状态下允许的最大请求数
	Interval          time.Duration // 统计周期
	Timeout           time.Duration // 打开状态持续时间
	FailureThreshold  uint32        // 失败阈值（失败次数）
	FailureRatio      float64       // 失败率阈值（0-1）
	MinRequestCount   uint32        // 最小请求数（达到此数量才开始统计失败率）
	OnStateChange     func(name string, from State, to State)
	Logger            *zap.Logger
}

// CircuitBreaker 熔断器
type CircuitBreaker struct {
	name              string
	maxRequests       uint32
	interval          time.Duration
	timeout           time.Duration
	failureThreshold  uint32
	failureRatio      float64
	minRequestCount   uint32
	onStateChange     func(name string, from State, to State)
	logger            *zap.Logger

	mutex           sync.Mutex
	state           State
	generation      uint64
	counts          *counts
	expiry          time.Time
}

type counts struct {
	Requests             uint32
	TotalSuccesses       uint32
	TotalFailures        uint32
	ConsecutiveSuccesses uint32
	ConsecutiveFailures  uint32
}

func (c *counts) onRequest() {
	c.Requests++
}

func (c *counts) onSuccess() {
	c.TotalSuccesses++
	c.ConsecutiveSuccesses++
	c.ConsecutiveFailures = 0
}

func (c *counts) onFailure() {
	c.TotalFailures++
	c.ConsecutiveFailures++
	c.ConsecutiveSuccesses = 0
}

func (c *counts) clear() {
	c.Requests = 0
	c.TotalSuccesses = 0
	c.TotalFailures = 0
	c.ConsecutiveSuccesses = 0
	c.ConsecutiveFailures = 0
}

// NewCircuitBreaker 创建熔断器
func NewCircuitBreaker(config *CircuitBreakerConfig) *CircuitBreaker {
	if config == nil {
		config = &CircuitBreakerConfig{}
	}

	if config.Name == "" {
		config.Name = "default"
	}

	if config.MaxRequests == 0 {
		config.MaxRequests = 1
	}

	if config.Interval <= 0 {
		config.Interval = 60 * time.Second
	}

	if config.Timeout <= 0 {
		config.Timeout = 60 * time.Second
	}

	if config.FailureThreshold == 0 {
		config.FailureThreshold = 5
	}

	if config.FailureRatio <= 0 || config.FailureRatio > 1 {
		config.FailureRatio = 0.5
	}

	if config.MinRequestCount == 0 {
		config.MinRequestCount = 10
	}

	cb := &CircuitBreaker{
		name:             config.Name,
		maxRequests:      config.MaxRequests,
		interval:         config.Interval,
		timeout:          config.Timeout,
		failureThreshold: config.FailureThreshold,
		failureRatio:     config.FailureRatio,
		minRequestCount:  config.MinRequestCount,
		onStateChange:    config.OnStateChange,
		logger:           config.Logger,
		state:            StateClosed,
		counts:           &counts{},
	}

	return cb
}

// Execute 执行函数（带熔断保护）
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func(context.Context) (interface{}, error)) (interface{}, error) {
	generation, err := cb.beforeRequest()
	if err != nil {
		return nil, err
	}

	result, err := fn(ctx)

	cb.afterRequest(generation, err == nil)

	return result, err
}

func (cb *CircuitBreaker) beforeRequest() (uint64, error) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, generation := cb.currentState(now)

	if state == StateOpen {
		return generation, ErrCircuitOpen
	} else if state == StateHalfOpen && cb.counts.Requests >= cb.maxRequests {
		return generation, ErrTooManyRequests
	}

	cb.counts.onRequest()
	return generation, nil
}

func (cb *CircuitBreaker) afterRequest(before uint64, success bool) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, generation := cb.currentState(now)
	if generation != before {
		return
	}

	if success {
		cb.onSuccess(state, now)
	} else {
		cb.onFailure(state, now)
	}
}

func (cb *CircuitBreaker) onSuccess(state State, now time.Time) {
	cb.counts.onSuccess()

	if state == StateHalfOpen {
		// 半开状态下连续成功，转为关闭状态
		if cb.counts.ConsecutiveSuccesses >= cb.maxRequests {
			cb.setState(StateClosed, now)
		}
	}
}

func (cb *CircuitBreaker) onFailure(state State, now time.Time) {
	cb.counts.onFailure()

	switch state {
	case StateClosed:
		// 检查是否需要打开熔断器
		if cb.shouldOpen() {
			cb.setState(StateOpen, now)
		}
	case StateHalfOpen:
		// 半开状态下失败，立即打开熔断器
		cb.setState(StateOpen, now)
	}
}

func (cb *CircuitBreaker) shouldOpen() bool {
	counts := cb.counts

	// 请求数未达到最小阈值
	if counts.Requests < cb.minRequestCount {
		return false
	}

	// 连续失败次数超过阈值
	if counts.ConsecutiveFailures >= cb.failureThreshold {
		return true
	}

	// 失败率超过阈值
	failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
	return failureRatio >= cb.failureRatio
}

func (cb *CircuitBreaker) currentState(now time.Time) (State, uint64) {
	switch cb.state {
	case StateClosed:
		if !cb.expiry.IsZero() && cb.expiry.Before(now) {
			cb.toNewGeneration(now)
		}
	case StateOpen:
		if cb.expiry.Before(now) {
			cb.setState(StateHalfOpen, now)
		}
	}
	return cb.state, cb.generation
}

func (cb *CircuitBreaker) setState(state State, now time.Time) {
	if cb.state == state {
		return
	}

	prev := cb.state
	cb.state = state

	cb.toNewGeneration(now)

	if cb.onStateChange != nil {
		cb.onStateChange(cb.name, prev, state)
	}

	if cb.logger != nil {
		cb.logger.Info("circuit breaker state changed",
			zap.String("name", cb.name),
			zap.String("from", prev.String()),
			zap.String("to", state.String()))
	}
}

func (cb *CircuitBreaker) toNewGeneration(now time.Time) {
	cb.generation++
	cb.counts.clear()

	var zero time.Time
	switch cb.state {
	case StateClosed:
		if cb.interval == 0 {
			cb.expiry = zero
		} else {
			cb.expiry = now.Add(cb.interval)
		}
	case StateOpen:
		cb.expiry = now.Add(cb.timeout)
	default: // StateHalfOpen
		cb.expiry = zero
	}
}

// State 获取当前状态
func (cb *CircuitBreaker) State() State {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, _ := cb.currentState(now)
	return state
}

// Counts 获取统计信息
func (cb *CircuitBreaker) Counts() (uint32, uint32, uint32) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	return cb.counts.Requests, cb.counts.TotalSuccesses, cb.counts.TotalFailures
}

// Reset 重置熔断器
func (cb *CircuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.toNewGeneration(time.Now())
	cb.setState(StateClosed, time.Now())
}

// CircuitBreakerManager 熔断器管理器
type CircuitBreakerManager struct {
	breakers map[string]*CircuitBreaker
	mutex    sync.RWMutex
	logger   *zap.Logger
}

// NewCircuitBreakerManager 创建熔断器管理器
func NewCircuitBreakerManager(logger *zap.Logger) *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers: make(map[string]*CircuitBreaker),
		logger:   logger,
	}
}

// GetOrCreate 获取或创建熔断器
func (m *CircuitBreakerManager) GetOrCreate(name string, config *CircuitBreakerConfig) *CircuitBreaker {
	m.mutex.RLock()
	cb, exists := m.breakers[name]
	m.mutex.RUnlock()

	if exists {
		return cb
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 双重检查
	if cb, exists := m.breakers[name]; exists {
		return cb
	}

	if config == nil {
		config = &CircuitBreakerConfig{}
	}
	config.Name = name
	config.Logger = m.logger

	cb = NewCircuitBreaker(config)
	m.breakers[name] = cb

	return cb
}

// Get 获取熔断器
func (m *CircuitBreakerManager) Get(name string) (*CircuitBreaker, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	cb, exists := m.breakers[name]
	if !exists {
		return nil, fmt.Errorf("circuit breaker %s not found", name)
	}

	return cb, nil
}

// Remove 移除熔断器
func (m *CircuitBreakerManager) Remove(name string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	delete(m.breakers, name)
}

// List 列出所有熔断器
func (m *CircuitBreakerManager) List() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	names := make([]string, 0, len(m.breakers))
	for name := range m.breakers {
		names = append(names, name)
	}

	return names
}
