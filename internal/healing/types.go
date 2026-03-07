package healing

import (
	"time"
)

// HealingState 自愈状态
type HealingState string

const (
	StateMonitoring  HealingState = "monitoring"
	StateDetecting   HealingState = "detecting"
	StateDiagnosing  HealingState = "diagnosing"
	StateDeciding    HealingState = "deciding"
	StateExecuting   HealingState = "executing"
	StateVerifying   HealingState = "verifying"
	StateLearning    HealingState = "learning"
	StateRollingBack HealingState = "rolling_back"
	StateCompleted   HealingState = "completed"
	StateFailed      HealingState = "failed"
)

// Severity 严重程度
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

// MonitorType 监控类型
type MonitorType string

const (
	MonitorTypeMetric MonitorType = "metric"
	MonitorTypeLog    MonitorType = "log"
	MonitorTypeEvent  MonitorType = "event"
)

// IncidentType 故障类型
type IncidentType string

const (
	IncidentTypePodCrashLoop     IncidentType = "pod_crash_loop"
	IncidentTypeHighCPU          IncidentType = "high_cpu"
	IncidentTypeHighMemory       IncidentType = "high_memory"
	IncidentTypeHighErrorRate    IncidentType = "high_error_rate"
	IncidentTypeServiceDown      IncidentType = "service_down"
	IncidentTypeDiskFull         IncidentType = "disk_full"
	IncidentTypeNetworkIssue     IncidentType = "network_issue"
	IncidentTypeDatabaseSlow     IncidentType = "database_slow"
)

// ExecutionStatus 执行状态
type ExecutionStatus string

const (
	ExecutionStatusPending   ExecutionStatus = "pending"
	ExecutionStatusRunning   ExecutionStatus = "running"
	ExecutionStatusCompleted ExecutionStatus = "completed"
	ExecutionStatusFailed    ExecutionStatus = "failed"
	ExecutionStatusRolledBack ExecutionStatus = "rolled_back"
)

// RiskLevel 风险等级
type RiskLevel string

const (
	RiskLevelLow    RiskLevel = "low"
	RiskLevelMedium RiskLevel = "medium"
	RiskLevelHigh   RiskLevel = "high"
)

// StrategyType 策略类型
type StrategyType string

const (
	StrategyTypeRestart  StrategyType = "restart"
	StrategyTypeScale    StrategyType = "scale"
	StrategyTypeRollback StrategyType = "rollback"
	StrategyTypeConfig   StrategyType = "config"
	StrategyTypeCustom   StrategyType = "custom"
)

// LessonType 经验类型
type LessonType string

const (
	LessonTypeRootCause LessonType = "root_cause"
	LessonTypeStrategy  LessonType = "strategy"
	LessonTypePattern   LessonType = "pattern"
)

// Resource 资源
type Resource struct {
	Type      string            `json:"type"`       // pod, service, deployment, etc.
	Namespace string            `json:"namespace"`
	Name      string            `json:"name"`
	Labels    map[string]string `json:"labels"`
}

// MonitorEvent 监控事件
type MonitorEvent struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Type      MonitorType       `json:"type"`
	Severity  Severity          `json:"severity"`
	Source    string            `json:"source"`
	Message   string            `json:"message"`
	Labels    map[string]string `json:"labels"`
	Value     float64           `json:"value"`
}

// Incident 故障事件
type Incident struct {
	ID                string           `json:"id"`
	Timestamp         time.Time        `json:"timestamp"`
	Severity          Severity         `json:"severity"`
	Type              IncidentType     `json:"type"`
	Title             string           `json:"title"`
	Description       string           `json:"description"`
	Events            []*MonitorEvent  `json:"events"`
	AffectedResources []Resource       `json:"affected_resources"`
}

// Evidence 证据
type Evidence struct {
	Type        string    `json:"type"`
	Source      string    `json:"source"`
	Description string    `json:"description"`
	Confidence  float64   `json:"confidence"`
	Timestamp   time.Time `json:"timestamp"`
}

// ImpactScope 影响范围
type ImpactScope struct {
	Services  []string `json:"services"`
	Users     int      `json:"users"`
	Regions   []string `json:"regions"`
	Severity  Severity `json:"severity"`
}

// DiagnosisResult 诊断结果
type DiagnosisResult struct {
	IncidentID   string             `json:"incident_id"`
	RootCause    string             `json:"root_cause"`
	Confidence   float64            `json:"confidence"`
	Evidence     []Evidence         `json:"evidence"`
	ImpactScope  ImpactScope        `json:"impact_scope"`
	SimilarCases []*HistoricalCase  `json:"similar_cases"`
}

// Condition 条件
type Condition struct {
	Type     string  `json:"type"`
	Operator string  `json:"operator"`
	Value    string  `json:"value"`
	Threshold float64 `json:"threshold"`
}

// Action 操作
type Action struct {
	Type        string            `json:"type"`
	Target      Resource          `json:"target"`
	Parameters  map[string]string `json:"parameters"`
	Description string            `json:"description"`
}

// ActionResult 操作结果
type ActionResult struct {
	Action    Action          `json:"action"`
	Status    ExecutionStatus `json:"status"`
	StartTime time.Time       `json:"start_time"`
	EndTime   time.Time       `json:"end_time"`
	Output    string          `json:"output"`
	Error     string          `json:"error,omitempty"`
}

// RollbackPlan 回滚计划
type RollbackPlan struct {
	Actions     []Action      `json:"actions"`
	Description string        `json:"description"`
	Timeout     time.Duration `json:"timeout"`
}

// HealingStrategy 自愈策略
type HealingStrategy struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Type        StrategyType `json:"type"`
	Actions     []Action     `json:"actions"`
	Conditions  []Condition  `json:"conditions"`
	Priority    int          `json:"priority"`
	Description string       `json:"description"`
}

// DecisionResult 决策结果
type DecisionResult struct {
	IncidentID       string           `json:"incident_id"`
	Strategy         *HealingStrategy `json:"strategy"`
	RiskLevel        RiskLevel        `json:"risk_level"`
	RollbackPlan     *RollbackPlan    `json:"rollback_plan"`
	EstimatedTime    time.Duration    `json:"estimated_time"`
	RequiresApproval bool             `json:"requires_approval"`
	Reasoning        string           `json:"reasoning"`
}

// ExecutionResult 执行结果
type ExecutionResult struct {
	IncidentID string            `json:"incident_id"`
	StartTime  time.Time         `json:"start_time"`
	EndTime    time.Time         `json:"end_time"`
	Status     ExecutionStatus   `json:"status"`
	Actions    []*ActionResult   `json:"actions"`
	Logs       []string          `json:"logs"`
	Errors     []string          `json:"errors"`
}

// HealthStatus 健康状态
type HealthStatus struct {
	Healthy     bool              `json:"healthy"`
	Checks      map[string]bool   `json:"checks"`
	Metrics     map[string]float64 `json:"metrics"`
	Description string            `json:"description"`
}

// VerificationResult 验证结果
type VerificationResult struct {
	IncidentID     string             `json:"incident_id"`
	Success        bool               `json:"success"`
	HealthStatus   HealthStatus       `json:"health_status"`
	MetricsBefore  map[string]float64 `json:"metrics_before"`
	MetricsAfter   map[string]float64 `json:"metrics_after"`
	Improvements   map[string]float64 `json:"improvements"`
	FailureReasons []string           `json:"failure_reasons"`
}

// Lesson 经验教训
type Lesson struct {
	Type          LessonType `json:"type"`
	Content       string     `json:"content"`
	Confidence    float64    `json:"confidence"`
	Applicability []string   `json:"applicability"`
}

// LearningResult 学习结果
type LearningResult struct {
	IncidentID        string             `json:"incident_id"`
	Success           bool               `json:"success"`
	Lessons           []Lesson           `json:"lessons"`
	UpdatedStrategies []*HealingStrategy `json:"updated_strategies"`
	KnowledgeUpdated  bool               `json:"knowledge_updated"`
}

// HistoricalCase 历史案例
type HistoricalCase struct {
	ID           string               `json:"id"`
	Timestamp    time.Time            `json:"timestamp"`
	Incident     *Incident            `json:"incident"`
	Diagnosis    *DiagnosisResult     `json:"diagnosis"`
	Decision     *DecisionResult      `json:"decision"`
	Execution    *ExecutionResult     `json:"execution"`
	Verification *VerificationResult  `json:"verification"`
	Success      bool                 `json:"success"`
	Duration     time.Duration        `json:"duration"`
	Embedding    []float32            `json:"embedding,omitempty"`
}

// HealingSession 自愈会话
type HealingSession struct {
	ID           string               `json:"id"`
	StartTime    time.Time            `json:"start_time"`
	State        HealingState         `json:"state"`
	Incident     *Incident            `json:"incident"`
	Diagnosis    *DiagnosisResult     `json:"diagnosis,omitempty"`
	Decision     *DecisionResult      `json:"decision,omitempty"`
	Execution    *ExecutionResult     `json:"execution,omitempty"`
	Verification *VerificationResult  `json:"verification,omitempty"`
	Learning     *LearningResult      `json:"learning,omitempty"`
	RetryCount   int                  `json:"retry_count"`
	MaxRetries   int                  `json:"max_retries"`
	ResultChan   chan *HealingResult  `json:"-"`
}

// HealingResult 自愈结果
type HealingResult struct {
	SessionID string `json:"session_id"`
	Success   bool   `json:"success"`
	Error     error  `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
}
