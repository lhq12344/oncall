package supervisor

import (
	"context"
	"fmt"
	"sync"

	"go_agent/internal/context"

	"github.com/cloudwego/eino/compose"
	"go.uber.org/zap"
)

// SupervisorAgent 总控代理
type SupervisorAgent struct {
	contextManager *context.ContextManager
	router         *AgentRouter
	aggregator     *ResultAggregator
	tools          []compose.Tool
	logger         *zap.Logger
	mu             sync.RWMutex
}

// Config Supervisor 配置
type Config struct {
	ContextManager *context.ContextManager
	Logger         *zap.Logger
}

// NewSupervisorAgent 创建 Supervisor Agent
func NewSupervisorAgent(cfg *Config) *SupervisorAgent {
	return &SupervisorAgent{
		contextManager: cfg.ContextManager,
		router:         NewAgentRouter(),
		aggregator:     NewResultAggregator(),
		tools:          make([]compose.Tool, 0),
		logger:         cfg.Logger,
	}
}

// RegisterTool 注册工具
func (s *SupervisorAgent) RegisterTool(tool compose.Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = append(s.tools, tool)
}

// RegisterTools 批量注册工具
func (s *SupervisorAgent) RegisterTools(tools []compose.Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = append(s.tools, tools...)
}

// GetTools 获取所有工具
func (s *SupervisorAgent) GetTools() []compose.Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tools
}

// HandleRequest 处理用户请求（主入口）
func (s *SupervisorAgent) HandleRequest(ctx context.Context, sessionID, userInput string) (string, error) {
	// 1. 获取会话上下文
	session, err := s.contextManager.GetSession(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to get session: %w", err)
	}

	// 2. 添加用户消息到历史
	if err := s.contextManager.AddMessage(ctx, sessionID, "user", userInput); err != nil {
		return "", fmt.Errorf("failed to add message: %w", err)
	}

	// 3. 分类意图
	intent, err := s.router.ClassifyIntent(ctx, session, userInput)
	if err != nil {
		return "", fmt.Errorf("failed to classify intent: %w", err)
	}

	s.logger.Info("intent classified",
		zap.String("session_id", sessionID),
		zap.String("intent_type", intent.Type),
		zap.Float64("confidence", intent.Confidence),
	)

	// 4. 根据意图路由到对应的处理流程
	var response string
	switch intent.Type {
	case "monitor":
		response, err = s.handleMonitorRequest(ctx, session, userInput)
	case "diagnose":
		response, err = s.handleDiagnoseRequest(ctx, session, userInput)
	case "execute":
		response, err = s.handleExecuteRequest(ctx, session, userInput)
	case "knowledge":
		response, err = s.handleKnowledgeRequest(ctx, session, userInput)
	default:
		response, err = s.handleGeneralRequest(ctx, session, userInput)
	}

	if err != nil {
		return "", fmt.Errorf("failed to handle request: %w", err)
	}

	// 5. 添加助手回复到历史
	if err := s.contextManager.AddMessage(ctx, sessionID, "assistant", response); err != nil {
		return "", fmt.Errorf("failed to add response: %w", err)
	}

	return response, nil
}

// handleMonitorRequest 处理监控查询请求
func (s *SupervisorAgent) handleMonitorRequest(ctx context.Context, session *context.SessionContext, input string) (string, error) {
	// TODO: 调用 Ops Agent 收集监控数据
	s.logger.Info("handling monitor request", zap.String("session_id", session.SessionID))
	return "监控查询功能开发中...", nil
}

// handleDiagnoseRequest 处理故障诊断请求（串行协作）
func (s *SupervisorAgent) handleDiagnoseRequest(ctx context.Context, session *context.SessionContext, input string) (string, error) {
	s.logger.Info("handling diagnose request", zap.String("session_id", session.SessionID))

	// 串行协作流程：Dialogue → Ops → RCA → Knowledge → Execution → Strategy
	// TODO: 实现完整的诊断流程

	return "故障诊断功能开发中...", nil
}

// handleExecuteRequest 处理执行操作请求
func (s *SupervisorAgent) handleExecuteRequest(ctx context.Context, session *context.SessionContext, input string) (string, error) {
	s.logger.Info("handling execute request", zap.String("session_id", session.SessionID))
	// TODO: 调用 Execution Agent 执行操作
	return "执行操作功能开发中...", nil
}

// handleKnowledgeRequest 处理知识检索请求
func (s *SupervisorAgent) handleKnowledgeRequest(ctx context.Context, session *context.SessionContext, input string) (string, error) {
	s.logger.Info("handling knowledge request", zap.String("session_id", session.SessionID))
	// TODO: 调用 Knowledge Agent 检索知识
	return "知识检索功能开发中...", nil
}

// handleGeneralRequest 处理通用请求
func (s *SupervisorAgent) handleGeneralRequest(ctx context.Context, session *context.SessionContext, input string) (string, error) {
	s.logger.Info("handling general request", zap.String("session_id", session.SessionID))
	// TODO: 使用 LLM 直接回答
	return "通用对话功能开发中...", nil
}

// HandleParallelCollection 并行信息收集
func (s *SupervisorAgent) HandleParallelCollection(ctx context.Context, sessionID, input string) (string, error) {
	// TODO: 实现并行协作模式
	return "", fmt.Errorf("not implemented")
}

// HandleSelfHealingLoop 自愈循环
func (s *SupervisorAgent) HandleSelfHealingLoop(ctx context.Context, executionID string) error {
	// TODO: 实现递归协作模式
	return fmt.Errorf("not implemented")
}
