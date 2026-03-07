package healing

import (
	"github.com/google/uuid"
)

// StrategyTemplate 策略模板
type StrategyTemplate struct {
	ID          string
	Name        string
	Type        StrategyType
	Description string
	Conditions  []Condition
	Actions     []Action
	Priority    int
	RiskLevel   RiskLevel
}

// GetDefaultStrategies 获取默认策略模板
func GetDefaultStrategies() []*StrategyTemplate {
	return []*StrategyTemplate{
		getPodRestartStrategy(),
		getPodScaleUpStrategy(),
		getPodScaleDownStrategy(),
		getDeploymentRollbackStrategy(),
		getNodeDrainStrategy(),
		getServiceRestartStrategy(),
		getDatabaseRestartStrategy(),
		getCacheFlushStrategy(),
		getConfigReloadStrategy(),
		getNetworkRepairStrategy(),
	}
}

// getPodRestartStrategy Pod 重启策略
func getPodRestartStrategy() *StrategyTemplate {
	return &StrategyTemplate{
		ID:          uuid.New().String(),
		Name:        "Pod Restart",
		Type:        StrategyTypeRestart,
		Description: "Restart the affected pod to recover from crash loop or unhealthy state",
		Conditions: []Condition{
			{
				Type:     "incident_type",
				Operator: "equals",
				Value:    string(IncidentTypePodCrashLoop),
			},
		},
		Actions: []Action{
			{
				Type: "delete_pod",
				Parameters: map[string]string{
					"graceful_period": "30",
				},
				Description: "Delete the pod to trigger restart",
			},
		},
		Priority:  1,
		RiskLevel: RiskLevelLow,
	}
}

// getPodScaleUpStrategy Pod 扩容策略
func getPodScaleUpStrategy() *StrategyTemplate {
	return &StrategyTemplate{
		ID:          uuid.New().String(),
		Name:        "Pod Scale Up",
		Type:        StrategyTypeScale,
		Description: "Scale up the deployment to handle high load",
		Conditions: []Condition{
			{
				Type:      "metric",
				Operator:  "greater_than",
				Threshold: 0.8,
			},
		},
		Actions: []Action{
			{
				Type: "scale_deployment",
				Parameters: map[string]string{
					"replicas": "+2",
					"max":      "10",
				},
				Description: "Increase replica count by 2",
			},
		},
		Priority:  2,
		RiskLevel: RiskLevelLow,
	}
}

// getPodScaleDownStrategy Pod 缩容策略
func getPodScaleDownStrategy() *StrategyTemplate {
	return &StrategyTemplate{
		ID:          uuid.New().String(),
		Name:        "Pod Scale Down",
		Type:        StrategyTypeScale,
		Description: "Scale down the deployment to save resources",
		Conditions: []Condition{
			{
				Type:      "metric",
				Operator:  "less_than",
				Threshold: 0.2,
			},
		},
		Actions: []Action{
			{
				Type: "scale_deployment",
				Parameters: map[string]string{
					"replicas": "-1",
					"min":      "1",
				},
				Description: "Decrease replica count by 1",
			},
		},
		Priority:  3,
		RiskLevel: RiskLevelLow,
	}
}

// getDeploymentRollbackStrategy Deployment 回滚策略
func getDeploymentRollbackStrategy() *StrategyTemplate {
	return &StrategyTemplate{
		ID:          uuid.New().String(),
		Name:        "Deployment Rollback",
		Type:        StrategyTypeRollback,
		Description: "Rollback deployment to previous stable version",
		Conditions: []Condition{
			{
				Type:     "incident_type",
				Operator: "equals",
				Value:    string(IncidentTypeHighErrorRate),
			},
		},
		Actions: []Action{
			{
				Type: "rollback_deployment",
				Parameters: map[string]string{
					"revision": "previous",
				},
				Description: "Rollback to previous revision",
			},
		},
		Priority:  1,
		RiskLevel: RiskLevelMedium,
	}
}

// getNodeDrainStrategy Node 驱逐策略
func getNodeDrainStrategy() *StrategyTemplate {
	return &StrategyTemplate{
		ID:          uuid.New().String(),
		Name:        "Node Drain",
		Type:        StrategyTypeCustom,
		Description: "Drain node to move pods to healthy nodes",
		Conditions: []Condition{
			{
				Type:     "resource_type",
				Operator: "equals",
				Value:    "node",
			},
		},
		Actions: []Action{
			{
				Type: "drain_node",
				Parameters: map[string]string{
					"grace_period":     "300",
					"ignore_daemonset": "true",
					"delete_local":     "true",
				},
				Description: "Drain the affected node",
			},
		},
		Priority:  2,
		RiskLevel: RiskLevelHigh,
	}
}

// getServiceRestartStrategy Service 重启策略
func getServiceRestartStrategy() *StrategyTemplate {
	return &StrategyTemplate{
		ID:          uuid.New().String(),
		Name:        "Service Restart",
		Type:        StrategyTypeRestart,
		Description: "Restart service by rolling restart of all pods",
		Conditions: []Condition{
			{
				Type:     "incident_type",
				Operator: "equals",
				Value:    string(IncidentTypeServiceDown),
			},
		},
		Actions: []Action{
			{
				Type: "rolling_restart",
				Parameters: map[string]string{
					"max_unavailable": "1",
					"wait_ready":      "true",
				},
				Description: "Perform rolling restart of service pods",
			},
		},
		Priority:  1,
		RiskLevel: RiskLevelMedium,
	}
}

// getDatabaseRestartStrategy 数据库重启策略
func getDatabaseRestartStrategy() *StrategyTemplate {
	return &StrategyTemplate{
		ID:          uuid.New().String(),
		Name:        "Database Restart",
		Type:        StrategyTypeRestart,
		Description: "Restart database service with connection draining",
		Conditions: []Condition{
			{
				Type:     "incident_type",
				Operator: "equals",
				Value:    string(IncidentTypeDatabaseSlow),
			},
		},
		Actions: []Action{
			{
				Type: "drain_connections",
				Parameters: map[string]string{
					"timeout": "60",
				},
				Description: "Drain existing database connections",
			},
			{
				Type: "restart_database",
				Parameters: map[string]string{
					"wait_ready": "true",
					"timeout":    "300",
				},
				Description: "Restart database service",
			},
		},
		Priority:  1,
		RiskLevel: RiskLevelHigh,
	}
}

// getCacheFlushStrategy 缓存清理策略
func getCacheFlushStrategy() *StrategyTemplate {
	return &StrategyTemplate{
		ID:          uuid.New().String(),
		Name:        "Cache Flush",
		Type:        StrategyTypeCustom,
		Description: "Flush cache to resolve stale data issues",
		Conditions: []Condition{
			{
				Type:     "service_type",
				Operator: "equals",
				Value:    "cache",
			},
		},
		Actions: []Action{
			{
				Type: "flush_cache",
				Parameters: map[string]string{
					"pattern": "*",
					"async":   "false",
				},
				Description: "Flush all cache entries",
			},
		},
		Priority:  2,
		RiskLevel: RiskLevelMedium,
	}
}

// getConfigReloadStrategy 配置重载策略
func getConfigReloadStrategy() *StrategyTemplate {
	return &StrategyTemplate{
		ID:          uuid.New().String(),
		Name:        "Config Reload",
		Type:        StrategyTypeConfig,
		Description: "Reload configuration without restarting service",
		Conditions: []Condition{
			{
				Type:     "config_changed",
				Operator: "equals",
				Value:    "true",
			},
		},
		Actions: []Action{
			{
				Type: "reload_config",
				Parameters: map[string]string{
					"signal": "SIGHUP",
				},
				Description: "Send reload signal to service",
			},
		},
		Priority:  3,
		RiskLevel: RiskLevelLow,
	}
}

// getNetworkRepairStrategy 网络修复策略
func getNetworkRepairStrategy() *StrategyTemplate {
	return &StrategyTemplate{
		ID:          uuid.New().String(),
		Name:        "Network Repair",
		Type:        StrategyTypeCustom,
		Description: "Repair network connectivity issues",
		Conditions: []Condition{
			{
				Type:     "incident_type",
				Operator: "equals",
				Value:    string(IncidentTypeNetworkIssue),
			},
		},
		Actions: []Action{
			{
				Type: "restart_network",
				Parameters: map[string]string{
					"interface": "eth0",
				},
				Description: "Restart network interface",
			},
			{
				Type: "flush_dns",
				Parameters: map[string]string{
					"cache": "all",
				},
				Description: "Flush DNS cache",
			},
		},
		Priority:  2,
		RiskLevel: RiskLevelMedium,
	}
}

// StrategyMatcher 策略匹配器
type StrategyMatcher struct {
	strategies []*StrategyTemplate
}

// NewStrategyMatcher 创建策略匹配器
func NewStrategyMatcher() *StrategyMatcher {
	return &StrategyMatcher{
		strategies: GetDefaultStrategies(),
	}
}

// Match 匹配策略
func (m *StrategyMatcher) Match(incident *Incident) *StrategyTemplate {
	var bestMatch *StrategyTemplate
	highestPriority := 999

	for _, strategy := range m.strategies {
		if m.matchConditions(strategy, incident) {
			if strategy.Priority < highestPriority {
				bestMatch = strategy
				highestPriority = strategy.Priority
			}
		}
	}

	return bestMatch
}

// matchConditions 匹配条件
func (m *StrategyMatcher) matchConditions(strategy *StrategyTemplate, incident *Incident) bool {
	for _, condition := range strategy.Conditions {
		if !m.matchCondition(condition, incident) {
			return false
		}
	}
	return true
}

// matchCondition 匹配单个条件
func (m *StrategyMatcher) matchCondition(condition Condition, incident *Incident) bool {
	switch condition.Type {
	case "incident_type":
		return string(incident.Type) == condition.Value
	case "severity":
		return string(incident.Severity) == condition.Value
	case "resource_type":
		if len(incident.AffectedResources) > 0 {
			return incident.AffectedResources[0].Type == condition.Value
		}
	}
	return false
}

// AddStrategy 添加策略
func (m *StrategyMatcher) AddStrategy(strategy *StrategyTemplate) {
	m.strategies = append(m.strategies, strategy)
}

// GetStrategies 获取所有策略
func (m *StrategyMatcher) GetStrategies() []*StrategyTemplate {
	return m.strategies
}

// ConvertToHealingStrategy 转换为 HealingStrategy
func (t *StrategyTemplate) ConvertToHealingStrategy(incident *Incident) *HealingStrategy {
	// 填充实际的资源信息
	actions := make([]Action, len(t.Actions))
	for i, action := range t.Actions {
		actions[i] = action
		if len(incident.AffectedResources) > 0 {
			actions[i].Target = incident.AffectedResources[0]
		}
	}

	return &HealingStrategy{
		ID:          t.ID,
		Name:        t.Name,
		Type:        t.Type,
		Actions:     actions,
		Conditions:  t.Conditions,
		Priority:    t.Priority,
		Description: t.Description,
	}
}
