package healing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestHealingLoopManager_Creation(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := &HealingConfig{
		AutoTrigger:     false,
		MonitorInterval: 30 * time.Second,
		MaxRetries:      3,
		EnableLearning:  true,
	}

	manager, err := NewHealingLoopManager(config, logger)
	assert.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, config.MaxRetries, manager.config.MaxRetries)
}

func TestHealingLoopManager_TriggerHealing(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := &HealingConfig{
		AutoTrigger:    false,
		MaxRetries:     2,
		EnableLearning: false,
	}

	manager, err := NewHealingLoopManager(config, logger)
	assert.NoError(t, err)

	ctx := context.Background()
	err = manager.Start(ctx)
	assert.NoError(t, err)
	defer manager.Stop()

	// 创建模拟故障
	incident := &Incident{
		ID:          "test-incident-1",
		Timestamp:   time.Now(),
		Severity:    SeverityHigh,
		Type:        IncidentTypePodCrashLoop,
		Title:       "Pod Crash Loop Detected",
		Description: "Pod api-service-xxx is in CrashLoopBackOff state",
		AffectedResources: []Resource{
			{
				Type:      "pod",
				Namespace: "default",
				Name:      "api-service-xxx",
			},
		},
	}

	// 触发自愈
	session, err := manager.TriggerHealing(ctx, incident)
	assert.NoError(t, err)
	assert.NotNil(t, session)
	assert.Equal(t, incident.ID, session.Incident.ID)

	// 等待结果（带超时）
	select {
	case result := <-session.ResultChan:
		assert.NotNil(t, result)
		assert.Equal(t, session.ID, result.SessionID)
		t.Logf("Healing result: success=%v, duration=%v", result.Success, result.Duration)
	case <-time.After(10 * time.Second):
		t.Fatal("Healing timeout")
	}
}

func TestHealingLoopManager_GetActiveSessions(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	config := &HealingConfig{
		AutoTrigger: false,
		MaxRetries:  1,
	}

	manager, err := NewHealingLoopManager(config, logger)
	assert.NoError(t, err)

	ctx := context.Background()
	err = manager.Start(ctx)
	assert.NoError(t, err)
	defer manager.Stop()

	// 初始应该没有活跃会话
	sessions := manager.GetActiveSessions()
	assert.Empty(t, sessions)

	// 触发一个自愈流程
	incident := &Incident{
		ID:       "test-incident-2",
		Severity: SeverityMedium,
		Type:     IncidentTypeHighCPU,
	}

	session, err := manager.TriggerHealing(ctx, incident)
	assert.NoError(t, err)

	// 应该有一个活跃会话
	sessions = manager.GetActiveSessions()
	assert.Len(t, sessions, 1)
	assert.Equal(t, session.ID, sessions[0].ID)

	// 等待完成
	<-session.ResultChan

	// 完成后应该没有活跃会话
	time.Sleep(100 * time.Millisecond)
	sessions = manager.GetActiveSessions()
	assert.Empty(t, sessions)
}
