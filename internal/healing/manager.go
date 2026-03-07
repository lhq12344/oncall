package healing

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go_agent/internal/concurrent"

	"github.com/cloudwego/eino/adk"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// HealingConfig 自愈配置
type HealingConfig struct {
	// 自动触发
	AutoTrigger     bool
	MonitorInterval time.Duration
	DetectionWindow time.Duration

	// 重试策略
	MaxRetries        int
	RetryDelay        time.Duration
	BackoffMultiplier float64

	// 审批策略
	RequireApproval bool
	ApprovalTimeout time.Duration

	// 学习策略
	EnableLearning bool
	MinConfidence  float64

	// Agents
	RCAAgent       adk.Agent
	StrategyAgent  adk.Agent
	ExecutionAgent adk.Agent
	KnowledgeAgent adk.Agent
	OpsAgent       adk.Agent
}

// HealingLoopManager 自愈循环管理器
type HealingLoopManager struct {
	config *HealingConfig

	// 组件
	monitor   *Monitor
	detector  *Detector
	diagnoser *Diagnoser
	decider   *Decider
	executor  *Executor
	verifier  *Verifier
	learner   *Learner

	// 状态管理
	activeSessions map[string]*HealingSession
	mutex          sync.RWMutex

	// 依赖
	logger    *zap.Logger
	cbManager *concurrent.CircuitBreakerManager

	// 控制
	ctx    context.Context
	cancel context.CancelFunc
}

// NewHealingLoopManager 创建自愈循环管理器
func NewHealingLoopManager(config *HealingConfig, logger *zap.Logger) (*HealingLoopManager, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// 设置默认值
	if config.MonitorInterval == 0 {
		config.MonitorInterval = 30 * time.Second
	}
	if config.DetectionWindow == 0 {
		config.DetectionWindow = 5 * time.Minute
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 30 * time.Second
	}
	if config.BackoffMultiplier == 0 {
		config.BackoffMultiplier = 2.0
	}
	if config.MinConfidence == 0 {
		config.MinConfidence = 0.7
	}

	cbManager := concurrent.NewCircuitBreakerManager(logger)

	manager := &HealingLoopManager{
		config:         config,
		activeSessions: make(map[string]*HealingSession),
		logger:         logger,
		cbManager:      cbManager,
	}

	// 初始化组件
	manager.monitor = NewMonitor(logger)
	manager.detector = NewDetector(logger)
	manager.diagnoser = NewDiagnoser(config.RCAAgent, config.OpsAgent, logger)
	manager.decider = NewDecider(config.StrategyAgent, logger)
	manager.executor = NewExecutor(config.ExecutionAgent, cbManager, logger)
	manager.verifier = NewVerifier(config.OpsAgent, logger)
	manager.learner = NewLearner(config.KnowledgeAgent, logger)

	return manager, nil
}

// Start 启动自愈循环
func (m *HealingLoopManager) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	m.logger.Info("healing loop manager started",
		zap.Bool("auto_trigger", m.config.AutoTrigger),
		zap.Duration("monitor_interval", m.config.MonitorInterval))

	if m.config.AutoTrigger {
		go m.monitorLoop()
	}

	return nil
}

// Stop 停止自愈循环
func (m *HealingLoopManager) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}

	m.logger.Info("healing loop manager stopped")
	return nil
}

// monitorLoop 监控循环
func (m *HealingLoopManager) monitorLoop() {
	ticker := time.NewTicker(m.config.MonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkAndTrigger()
		}
	}
}

// checkAndTrigger 检查并触发自愈
func (m *HealingLoopManager) checkAndTrigger() {
	// 1. 监控
	events, err := m.monitor.Collect(m.ctx)
	if err != nil {
		m.logger.Error("failed to collect monitor events", zap.Error(err))
		return
	}

	if len(events) == 0 {
		return
	}

	// 2. 检测
	incident, err := m.detector.Detect(m.ctx, events)
	if err != nil {
		m.logger.Error("failed to detect incident", zap.Error(err))
		return
	}

	if incident == nil {
		return
	}

	m.logger.Info("incident detected",
		zap.String("incident_id", incident.ID),
		zap.String("type", string(incident.Type)),
		zap.String("severity", string(incident.Severity)))

	// 3. 触发自愈
	_, err = m.TriggerHealing(m.ctx, incident)
	if err != nil {
		m.logger.Error("failed to trigger healing", zap.Error(err))
	}
}

// TriggerHealing 触发自愈流程
func (m *HealingLoopManager) TriggerHealing(ctx context.Context, incident *Incident) (*HealingSession, error) {
	sessionID := uuid.New().String()

	session := &HealingSession{
		ID:         sessionID,
		StartTime:  time.Now(),
		State:      StateDetecting,
		Incident:   incident,
		MaxRetries: m.config.MaxRetries,
		ResultChan: make(chan *HealingResult, 1),
	}

	m.mutex.Lock()
	m.activeSessions[sessionID] = session
	m.mutex.Unlock()

	m.logger.Info("healing session started",
		zap.String("session_id", sessionID),
		zap.String("incident_id", incident.ID))

	// 异步执行自愈流程
	go m.executeHealingLoop(ctx, session)

	return session, nil
}

// executeHealingLoop 执行自愈循环
func (m *HealingLoopManager) executeHealingLoop(ctx context.Context, session *HealingSession) {
	startTime := time.Now()
	var err error

	defer func() {
		duration := time.Since(startTime)
		result := &HealingResult{
			SessionID: session.ID,
			Success:   session.State == StateCompleted,
			Error:     err,
			Duration:  duration,
		}

		session.ResultChan <- result
		close(session.ResultChan)

		m.mutex.Lock()
		delete(m.activeSessions, session.ID)
		m.mutex.Unlock()

		m.logger.Info("healing session completed",
			zap.String("session_id", session.ID),
			zap.String("state", string(session.State)),
			zap.Duration("duration", duration),
			zap.Bool("success", result.Success))
	}()

	// 3. 诊断
	session.State = StateDiagnosing
	diagnosis, err := m.diagnoser.Diagnose(ctx, session.Incident)
	if err != nil {
		m.logger.Error("diagnosis failed", zap.Error(err))
		session.State = StateFailed
		return
	}
	session.Diagnosis = diagnosis

	m.logger.Info("diagnosis completed",
		zap.String("session_id", session.ID),
		zap.String("root_cause", diagnosis.RootCause),
		zap.Float64("confidence", diagnosis.Confidence))

	// 4. 决策
	session.State = StateDeciding
	decision, err := m.decider.Decide(ctx, session.Incident, diagnosis)
	if err != nil {
		m.logger.Error("decision failed", zap.Error(err))
		session.State = StateFailed
		return
	}
	session.Decision = decision

	m.logger.Info("decision completed",
		zap.String("session_id", session.ID),
		zap.String("strategy", decision.Strategy.Name),
		zap.String("risk_level", string(decision.RiskLevel)))

	// 检查是否需要审批
	if decision.RequiresApproval && m.config.RequireApproval {
		m.logger.Warn("healing requires approval",
			zap.String("session_id", session.ID))
		session.State = StateFailed
		err = fmt.Errorf("healing requires manual approval")
		return
	}

	// 5. 执行（带重试）
	for attempt := 0; attempt <= session.MaxRetries; attempt++ {
		session.RetryCount = attempt

		session.State = StateExecuting
		execution, execErr := m.executor.Execute(ctx, decision)
		session.Execution = execution

		if execErr != nil {
			m.logger.Error("execution failed",
				zap.String("session_id", session.ID),
				zap.Int("attempt", attempt),
				zap.Error(execErr))

			if attempt < session.MaxRetries {
				delay := time.Duration(float64(m.config.RetryDelay) * float64(attempt+1) * m.config.BackoffMultiplier)
				m.logger.Info("retrying execution",
					zap.String("session_id", session.ID),
					zap.Duration("delay", delay))
				time.Sleep(delay)
				continue
			}

			session.State = StateFailed
			err = execErr
			return
		}

		// 6. 验证
		session.State = StateVerifying
		verification, verifyErr := m.verifier.Verify(ctx, session.Incident, execution)
		session.Verification = verification

		if verifyErr != nil {
			m.logger.Error("verification failed",
				zap.String("session_id", session.ID),
				zap.Error(verifyErr))

			if attempt < session.MaxRetries {
				m.logger.Info("verification failed, retrying",
					zap.String("session_id", session.ID))
				continue
			}

			// 回滚
			session.State = StateRollingBack
			if rollbackErr := m.executor.Rollback(ctx, decision.RollbackPlan); rollbackErr != nil {
				m.logger.Error("rollback failed",
					zap.String("session_id", session.ID),
					zap.Error(rollbackErr))
			}

			session.State = StateFailed
			err = verifyErr
			return
		}

		if !verification.Success {
			m.logger.Warn("verification unsuccessful",
				zap.String("session_id", session.ID),
				zap.Strings("reasons", verification.FailureReasons))

			if attempt < session.MaxRetries {
				continue
			}

			// 回滚
			session.State = StateRollingBack
			if rollbackErr := m.executor.Rollback(ctx, decision.RollbackPlan); rollbackErr != nil {
				m.logger.Error("rollback failed",
					zap.String("session_id", session.ID),
					zap.Error(rollbackErr))
			}

			session.State = StateFailed
			err = fmt.Errorf("verification failed after %d attempts", session.MaxRetries+1)
			return
		}

		// 验证成功，跳出重试循环
		break
	}

	// 7. 学习
	if m.config.EnableLearning {
		session.State = StateLearning
		learning, learnErr := m.learner.Learn(ctx, session)
		session.Learning = learning

		if learnErr != nil {
			m.logger.Warn("learning failed", zap.Error(learnErr))
			// 学习失败不影响整体流程
		} else {
			m.logger.Info("learning completed",
				zap.String("session_id", session.ID),
				zap.Int("lessons_count", len(learning.Lessons)))
		}
	}

	session.State = StateCompleted
}

// GetActiveSessions 获取活跃会话
func (m *HealingLoopManager) GetActiveSessions() []*HealingSession {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	sessions := make([]*HealingSession, 0, len(m.activeSessions))
	for _, session := range m.activeSessions {
		sessions = append(sessions, session)
	}

	return sessions
}

// GetSession 获取会话
func (m *HealingLoopManager) GetSession(sessionID string) (*HealingSession, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	session, exists := m.activeSessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	return session, nil
}
