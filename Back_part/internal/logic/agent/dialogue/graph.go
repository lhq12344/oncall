package dialogue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	dialoguetools "go_agent/internal/logic/agent/dialogue/tools"
	airetriever "go_agent/internal/logic/ai/retriever"
	appcontext "go_agent/internal/logic/session"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func init() {
	// 注册 OrchState 以支持 compose checkpoint 序列化。
	schema.RegisterName[*OrchState]("orch_state_v1")
}

type traceContextKey struct{}

type TraceMetadata struct {
	SessionID    string
	TurnID       string
	CheckpointID string
	Source       string
}

func WithTraceMetadata(ctx context.Context, meta TraceMetadata) context.Context {
	return context.WithValue(ctx, traceContextKey{}, meta)
}

func traceMetadataFromContext(ctx context.Context) TraceMetadata {
	meta, _ := ctx.Value(traceContextKey{}).(TraceMetadata)
	return meta
}

// OrchestrationResult 编排图构建结果。
type OrchestrationResult struct {
	// Graph 是编译后的 Runnable，供 Service 层调用。
	// 输入 []*schema.Message，流式输出 *schema.Message。
	Graph compose.Runnable[[]*schema.Message, *schema.Message]
}

// BuildOrchestrationGraph 构建三 Agent 协同对话编排图。
//
// 图拓扑（DAG）：
//
//	START → analysis_node → gate_node → [ragResultRouter] → answer_node → END
//	                                                   ↘ complex_node → END
//
// analysis_node 确定性运行意图/情绪工具并注入当前轮次内部分析上下文。
// gate_node 以批量模式运行 Gate Agent（RAG 检索 → 路由标记）。
// ragResultRouter 检查 Gate 输出中的 [RESOLVED]/[TO_COMPLEX] 标记决定路由。
// answer_node 以流式模式运行 Answer Agent（整理 RAG 结果，结合情绪调整语气）。
// complex_node 以流式模式运行 Complex Agent（全工具集 + 中断/恢复支持，结合情绪调整语气）。
//
// 入参 checkpointStore 同时用于外层图 checkpoint（中断时保存图状态）和
// complex_node 内部 complexRunner checkpoint（保存 Agent ReAct 状态）。
func BuildOrchestrationGraph(
	ctx context.Context,
	cfg *Config,
	checkpointStore compose.CheckPointStore,
) (*OrchestrationResult, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	retrieverCtx, cancel := context.WithTimeout(ctx, milvusRetrieverInitTimeout)
	defer cancel()
	knowledgeRetriever, err := airetriever.NewMilvusRetriever(retrieverCtx)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Warn("milvus retriever unavailable for orchestration graph, RAG degraded",
				zap.Error(err))
		}
		knowledgeRetriever = nil
	}

	gateAgent, err := newGateAgent(ctx, cfg, knowledgeRetriever)
	if err != nil {
		return nil, fmt.Errorf("failed to build gate agent: %w", err)
	}
	answerAgent, err := newAnswerAgent(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build answer agent: %w", err)
	}
	complexAgent, err := newComplexAgent(ctx, cfg, knowledgeRetriever)
	if err != nil {
		return nil, fmt.Errorf("failed to build complex agent: %w", err)
	}

	// 为 Complex Agent 创建专用 Runner，携带 checkpoint store 支持 interrupt/resume。
	complexRunner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           complexAgent,
		EnableStreaming: true,
		CheckPointStore: checkpointStore,
	})

	g := compose.NewGraph[[]*schema.Message, *schema.Message](
		compose.WithGenLocalState(func(ctx context.Context) *OrchState {
			return &OrchState{}
		}),
	)

	if err := g.AddLambdaNode("language_detector", compose.InvokableLambda(
		func(ctx context.Context, msgs []*schema.Message) ([]*schema.Message, error) {
			return detectAndStoreUserLanguage(ctx, cfg, msgs)
		},
	)); err != nil {
		return nil, fmt.Errorf("failed to add language_detector: %w", err)
	}

	// analysis_node：确定性运行意图/情绪分析，不依赖 LLM 自觉调用工具。
	if err := g.AddLambdaNode("analysis_node", compose.InvokableLambda(
		func(ctx context.Context, msgs []*schema.Message) ([]*schema.Message, error) {
			return appendAnalysisNodeMessage(ctx, cfg, msgs)
		},
	)); err != nil {
		return nil, fmt.Errorf("failed to add analysis_node: %w", err)
	}

	// gate_node：批量运行 Gate Agent（含知识库检索），收集输出消息供路由函数检查。
	// 同时解析 knowledge_search_expert 工具返回结果，写入 OrchState Blackboard。
	if err := g.AddLambdaNode("gate_node", compose.InvokableLambda(
		func(ctx context.Context, msgs []*schema.Message) ([]*schema.Message, error) {
			out, err := collectAgentMessages(withDialogueRuntimeContextFromState(ctx), gateAgent, msgs, cfg.Logger)
			if err == nil {
				recordGateTraceEvents(ctx, cfg, out)
				writeKnowledgeSpecialistResultToState(ctx, out, cfg.Logger)
			}
			return out, err
		},
	)); err != nil {
		return nil, fmt.Errorf("failed to add gate_node: %w", err)
	}

	// answer_node：流式运行 Answer Agent。
	if err := g.AddLambdaNode("answer_node", compose.StreamableLambda(
		func(ctx context.Context, msgs []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
			agentCtx := withDialogueRuntimeContextFromState(ctx)
			return streamAgentMessages(agentCtx, answerAgent, prepareAnswerAgentMessages(ctx, msgs), cfg.Logger)
		},
	)); err != nil {
		return nil, fmt.Errorf("failed to add answer_node: %w", err)
	}

	// complex_node：流式运行 Complex Agent，支持 interrupt/resume。
	if err := g.AddLambdaNode("complex_node", compose.StreamableLambda(
		buildComplexNodeLambda(complexRunner, cfg.Logger),
	)); err != nil {
		return nil, fmt.Errorf("failed to add complex_node: %w", err)
	}

	for _, edge := range [][2]string{
		{compose.START, "language_detector"},
		{"language_detector", "analysis_node"},
		{"analysis_node", "gate_node"},
		{"answer_node", compose.END},
		{"complex_node", compose.END},
	} {
		if err := g.AddEdge(edge[0], edge[1]); err != nil {
			return nil, fmt.Errorf("failed to add edge %s→%s: %w", edge[0], edge[1], err)
		}
	}

	branch := compose.NewGraphBranch(
		func(ctx context.Context, msgs []*schema.Message) (string, error) {
			route, err := ragResultRouter(ctx, msgs)
			recordTraceEvent(ctx, cfg, "gate_node", "route_decision", "gate_agent", "", statusFromError(err), route, errorSummary(err))
			return route, err
		},
		map[string]bool{"answer_node": true, "complex_node": true},
	)
	if err := g.AddBranch("gate_node", branch); err != nil {
		return nil, fmt.Errorf("failed to add router branch: %w", err)
	}

	compileOpts := []compose.GraphCompileOption{
		compose.WithGraphName("dialogue_orchestration"),
		compose.WithNodeTriggerMode(compose.AllPredecessor),
	}
	if checkpointStore != nil {
		compileOpts = append(compileOpts, compose.WithCheckPointStore(checkpointStore))
	}

	runnable, err := g.Compile(ctx, compileOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to compile orchestration graph: %w", err)
	}

	return &OrchestrationResult{Graph: runnable}, nil
}

const analysisNodeTimeout = 5 * time.Second

func detectAndStoreUserLanguage(ctx context.Context, cfg *Config, msgs []*schema.Message) ([]*schema.Message, error) {
	question := lastUserQuestion(msgs)
	detectCtx, cancel := context.WithTimeout(ctx, languageDetectorTimeout)
	defer cancel()

	model := cfg.resolveModel(cfg.GateModel, cfg.ChatModel)
	language, detectErr := detectUserLanguage(detectCtx, model, question)
	language = normalizeLanguageCode(language)
	if language == "" {
		language = defaultUserLanguage
	}

	if err := compose.ProcessState[*OrchState](ctx, func(_ context.Context, s *OrchState) error {
		s.UserLanguage = language
		return nil
	}); err != nil && cfg.Logger != nil {
		cfg.Logger.Warn("failed to save user language to graph state", zap.Error(err))
	}

	status := "success"
	if detectErr != nil {
		status = "degraded"
	}
	recordTraceEvent(ctx, cfg, "language_detector", "language_detected", "language_detector", "", status, language, errorSummary(detectErr))
	return msgs, nil
}

func appendAnalysisNodeMessage(ctx context.Context, cfg *Config, msgs []*schema.Message) ([]*schema.Message, error) {
	question := lastUserQuestion(msgs)
	analysisCtx, cancel := context.WithTimeout(ctx, analysisNodeTimeout)
	defer cancel()

	intentJSON, intentErr := runAnalysisTool(
		analysisCtx,
		dialoguetools.NewIntentAnalysisTool(cfg.ChatModel, cfg.Embedder, cfg.Logger, cfg.EnableToolLLM),
		question,
		fallbackIntentAnalysis(),
	)
	emotionJSON, emotionErr := runAnalysisTool(
		analysisCtx,
		dialoguetools.NewPlayerEmotionAnalysisTool(cfg.ChatModel, cfg.Logger),
		question,
		fallbackEmotionAnalysis(),
	)

	analysis := turnAnalysisFromToolResults(intentJSON, emotionJSON, errors.Join(intentErr, emotionErr))
	if err := compose.ProcessState[*OrchState](ctx, func(_ context.Context, s *OrchState) error {
		s.Analysis = analysis
		return nil
	}); err != nil && cfg.Logger != nil {
		cfg.Logger.Warn("failed to save turn analysis to graph state", zap.Error(err))
	}

	message := buildAnalysisSystemMessage(intentJSON, emotionJSON, analysis, userLanguageFromState(ctx))
	out := make([]*schema.Message, 0, len(msgs)+1)
	out = append(out, msgs...)
	out = append(out, schema.SystemMessage(message))

	status := "success"
	if analysis.Degraded {
		status = "degraded"
	}
	recordTraceEvent(ctx, cfg, "analysis_node", "analysis", "analysis_node", "", status, message, analysis.ErrorSummary)
	return out, nil
}

func runAnalysisTool(ctx context.Context, analysisTool tool.BaseTool, userInput string, fallback string) (string, error) {
	invokable, ok := analysisTool.(interface {
		InvokableRun(context.Context, string, ...tool.Option) (string, error)
	})
	if !ok {
		return fallback, fmt.Errorf("analysis tool is not invokable")
	}
	args, err := json.Marshal(map[string]string{"user_input": strings.TrimSpace(userInput)})
	if err != nil {
		return fallback, err
	}
	out, err := invokable.InvokableRun(ctx, string(args))
	if err != nil {
		return fallback, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return fallback, fmt.Errorf("empty analysis result")
	}
	return out, nil
}

func fallbackIntentAnalysis() string {
	return `{"intent_type":"other","intent_label":"其他","confidence":0.2,"entropy":1,"converged":false,"missing_info":[],"routing_hint":"complex"}`
}

func fallbackEmotionAnalysis() string {
	return `{"emotion":"stable","emotion_label":"稳定","confidence":0.2,"intensity":0.28,"escalation_needed":false,"evidence_keywords":[]}`
}

func turnAnalysisFromToolResults(intentJSON, emotionJSON string, analysisErr error) *TurnAnalysis {
	intentPayload := decodeJSONMap(intentJSON)
	emotionPayload := decodeJSONMap(emotionJSON)
	analysis := &TurnAnalysis{
		IntentType:        stringValue(intentPayload, "intent_type", "other"),
		IntentLabel:       stringValue(intentPayload, "intent_label", "其他"),
		IntentConfidence:  floatValue(intentPayload, "confidence", 0.2),
		IntentEntropy:     floatValue(intentPayload, "entropy", 1),
		IntentConverged:   boolValue(intentPayload, "converged", false),
		MissingInfo:       stringSliceValue(intentPayload, "missing_info"),
		RoutingHint:       stringValue(intentPayload, "routing_hint", "complex"),
		Emotion:           stringValue(emotionPayload, "emotion", "stable"),
		EmotionLabel:      stringValue(emotionPayload, "emotion_label", "稳定"),
		EmotionConfidence: floatValue(emotionPayload, "confidence", 0.2),
		EmotionIntensity:  floatValue(emotionPayload, "intensity", 0.28),
		EscalationNeeded:  boolValue(emotionPayload, "escalation_needed", false),
	}
	if analysisErr != nil {
		analysis.Degraded = true
		analysis.ErrorSummary = appcontext.ClipTraceText(analysisErr.Error())
	}
	return analysis
}

func buildAnalysisSystemMessage(intentJSON, emotionJSON string, analysis *TurnAnalysis, userLanguage string) string {
	degraded := false
	errorSummary := ""
	if analysis != nil {
		degraded = analysis.Degraded
		errorSummary = analysis.ErrorSummary
	}
	return fmt.Sprintf("%s：user_language=%s; intent_analysis=%s; player_emotion_analysis=%s; analysis_degraded=%t; error_summary=%s",
		analysisMessageMarker,
		normalizeLanguageCode(userLanguage),
		strings.TrimSpace(intentJSON),
		strings.TrimSpace(emotionJSON),
		degraded,
		errorSummary,
	)
}

func decodeJSONMap(raw string) map[string]any {
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func stringValue(payload map[string]any, key string, fallback string) string {
	if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func floatValue(payload map[string]any, key string, fallback float64) float64 {
	if value, ok := payload[key]; ok {
		if n := numericValue(value); n != 0 {
			return n
		}
	}
	return fallback
}

func boolValue(payload map[string]any, key string, fallback bool) bool {
	if value, ok := payload[key].(bool); ok {
		return value
	}
	return fallback
}

func stringSliceValue(payload map[string]any, key string) []string {
	raw, ok := payload[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

// buildComplexNodeLambda 构建 complex_node 的 StreamableLambda 函数。
// 首次调用：生成 innerCPID，运行 complexRunner，检测中断并通过 compose.StatefulInterrupt 传播到外层图。
// 恢复调用：从图状态读取 innerCPID 和 ResumeData，调用 complexRunner.ResumeWithParams 继续。
func buildComplexNodeLambda(
	complexRunner *adk.Runner,
	logger *zap.Logger,
) func(ctx context.Context, msgs []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
	return func(ctx context.Context, msgs []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
		wasInterrupted, _, innerCPID := compose.GetInterruptState[string](ctx)

		sr, sw := schema.Pipe[*schema.Message](100)

		go func() {
			defer sw.Close()

			if wasInterrupted && strings.TrimSpace(innerCPID) != "" {
				// 恢复路径：从图共享状态读取审批数据和 interrupt IDs。
				var resumeData map[string]any
				var interruptIDs []string
				if err := compose.ProcessState[*OrchState](ctx, func(_ context.Context, s *OrchState) error {
					resumeData = s.ResumeData
					interruptIDs = s.ResumeInterruptIDs
					return nil
				}); err != nil {
					// 读取状态失败：无法获取审批数据，无法精确恢复，中止避免无限中断循环。
					sw.Send(nil, fmt.Errorf("failed to read resume state from graph: %w", err))
					return
				}
				runComplexResume(withDialogueRuntimeContextFromState(ctx), complexRunner, innerCPID, interruptIDs, resumeData, sw, logger)
			} else {
				// 首次运行路径：生成 innerCPID 并存入图状态。
				innerCPID = fmt.Sprintf("complex:%s", uuid.NewString())
				if err := compose.ProcessState[*OrchState](ctx, func(_ context.Context, s *OrchState) error {
					s.InnerCheckpointID = innerCPID
					return nil
				}); err != nil {
					// 写入失败不中止：innerCPID 已作为 StatefulInterrupt 本地状态保存，中断/恢复主路径不受影响。
					if logger != nil {
						logger.Warn("failed to save inner checkpoint ID to graph state",
							zap.String("inner_checkpoint_id", innerCPID),
							zap.Error(err))
					}
				}
				runComplexFresh(withDialogueRuntimeContextFromState(ctx), complexRunner, innerCPID, prepareComplexAgentMessages(ctx, msgs), sw, logger)
			}
		}()

		return sr, nil
	}
}

// runComplexFresh 首次运行 Complex Agent（无中断历史）。
func runComplexFresh(
	ctx context.Context,
	runner *adk.Runner,
	innerCPID string,
	msgs []*schema.Message,
	sw *schema.StreamWriter[*schema.Message],
	logger *zap.Logger,
) {
	iter := runner.Run(ctx, msgs, adk.WithCheckPointID(innerCPID))
	drainIterToStream(ctx, iter, innerCPID, sw, logger)
}

// runComplexResume 从 innerCPID 对应的检查点恢复 Complex Agent。
func runComplexResume(
	ctx context.Context,
	runner *adk.Runner,
	innerCPID string,
	interruptIDs []string,
	resumeData map[string]any,
	sw *schema.StreamWriter[*schema.Message],
	logger *zap.Logger,
) {
	targets := make(map[string]any, len(interruptIDs))
	for _, id := range interruptIDs {
		targets[id] = resumeData
	}

	var iter *adk.AsyncIterator[*adk.AgentEvent]
	var err error
	if len(targets) == 0 {
		iter, err = runner.Resume(ctx, innerCPID)
	} else {
		iter, err = runner.ResumeWithParams(ctx, innerCPID, &adk.ResumeParams{Targets: targets})
	}
	if err != nil {
		sw.Send(nil, fmt.Errorf("failed to resume complex agent: %w", err))
		return
	}
	drainIterToStream(ctx, iter, innerCPID, sw, logger)
}

// drainIterToStream 将 adk.AgentEvent 迭代器转为 schema.StreamWriter[*schema.Message] 输出。
// 检测 ADK interrupt 并通过 compose.StatefulInterrupt 向外层图传播。
func drainIterToStream(
	ctx context.Context,
	iter *adk.AsyncIterator[*adk.AgentEvent],
	innerCPID string,
	sw *schema.StreamWriter[*schema.Message],
	logger *zap.Logger,
) {
	emitCount := 0
	eventCount := 0

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		eventCount++

		if event.Err != nil {
			if logger != nil {
				logger.Error("complex_node event error",
					zap.String("inner_cp", innerCPID),
					zap.Error(event.Err))
			}
			sw.Send(nil, fmt.Errorf("complex agent error: %w", event.Err))
			return
		}

		// 将 ADK interrupt 转换为外层 compose.Graph 中断，并存储 innerCPID 为本地状态。
		if event.Action != nil && event.Action.Interrupted != nil {
			if logger != nil {
				logger.Info("complex_node interrupt detected, raising graph-level interrupt",
					zap.String("inner_checkpoint_id", innerCPID))
			}
			interruptErr := compose.StatefulInterrupt(ctx, event.Action.Interrupted, innerCPID)
			sw.Send(nil, interruptErr)
			return
		}

		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		mo := event.Output.MessageOutput
		if mo.Message != nil {
			emit := shouldEmitAssistantMessage(mo, mo.Message)
			if logger != nil {
				logger.Info("complex_node message",
					zap.String("role", string(mo.Message.Role)),
					zap.String("mo_role", string(mo.Role)),
					zap.Int("content_len", len(mo.Message.Content)),
					zap.Bool("has_tool_id", strings.TrimSpace(mo.Message.ToolCallID) != ""),
					zap.Int("tool_calls", len(mo.Message.ToolCalls)),
					zap.Bool("emit", emit))
			}
			if emit {
				emitCount++
				sw.Send(localizeAssistantMessage(ctx, mo.Message), nil)
			}
		} else if mo.MessageStream != nil {
			streamChunks := 0
			streamEmit := 0
			for {
				chunk, err := mo.MessageStream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					if logger != nil {
						logger.Error("complex_node stream error",
							zap.String("inner_cp", innerCPID),
							zap.Error(err))
					}
					sw.Send(nil, fmt.Errorf("complex agent stream error: %w", err))
					return
				}
				if chunk == nil {
					continue
				}
				streamChunks++
				emit := shouldEmitAssistantMessage(mo, chunk)
				if emit {
					streamEmit++
					emitCount++
					if logger != nil {
						logger.Info("complex_node emitting stream chunk",
							zap.Int("content_len", len(chunk.Content)),
							zap.String("preview", previewContent(chunk.Content)))
					}
					sw.Send(localizeAssistantMessage(ctx, chunk), nil)
				}
			}
			if logger != nil {
				logger.Info("complex_node stream done",
					zap.String("mo_role", string(mo.Role)),
					zap.Int("chunks_total", streamChunks),
					zap.Int("chunks_emitted", streamEmit))
			}
		}
	}

	if logger != nil {
		logger.Info("complex_node drain complete",
			zap.String("inner_cp", innerCPID),
			zap.Int("events", eventCount),
			zap.Int("emitted", emitCount))
	}
}

// collectAgentMessages 同步运行 Agent（批量模式），收集全部输出消息。
// 返回：输入消息 + Agent 新产生的消息（含工具调用消息，供路由函数检查）。
func collectAgentMessages(ctx context.Context, agent adk.Agent, inputMsgs []*schema.Message, logger *zap.Logger) ([]*schema.Message, error) {
	iter := agent.Run(ctx, &adk.AgentInput{
		Messages:        inputMsgs,
		EnableStreaming: false,
	})

	result := make([]*schema.Message, len(inputMsgs))
	copy(result, inputMsgs)

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			return nil, fmt.Errorf("gate agent error: %w", event.Err)
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		msg, err := event.Output.MessageOutput.GetMessage()
		if err == nil && msg != nil {
			result = append(result, msg)
		}
	}

	// 记录 gate_node 路由判断依据，帮助诊断路由方向。
	if logger != nil {
		for i := len(result) - 1; i >= 0; i-- {
			m := result[i]
			if m.Role == schema.Assistant && strings.TrimSpace(m.ToolCallID) == "" {
				logger.Info("gate_node routing decision",
					zap.Int("content_len", len(m.Content)),
					zap.Bool("resolved", strings.Contains(m.Content, "[RESOLVED]")),
					zap.Bool("to_complex", strings.Contains(m.Content, "[TO_COMPLEX]")),
					zap.String("preview", func() string {
						s := strings.TrimSpace(m.Content)
						if len(s) > 120 {
							return s[:120]
						}
						return s
					}()))
				break
			}
		}
	}

	return result, nil
}

func recordGateTraceEvents(ctx context.Context, cfg *Config, msgs []*schema.Message) {
	if cfg == nil || cfg.TraceRecorder == nil {
		return
	}
	// Build map from ToolCallID to tool name
	toolCallIDToName := make(map[string]string)
	for _, msg := range msgs {
		if msg != nil && msg.Role == schema.Assistant {
			for _, tc := range msg.ToolCalls {
				if strings.TrimSpace(tc.ID) != "" && strings.TrimSpace(tc.Function.Name) != "" {
					toolCallIDToName[tc.ID] = tc.Function.Name
				}
			}
		}
	}

	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		switch {
		case msg.Role == schema.Tool:
			toolName := toolCallIDToName[msg.ToolCallID]
			if toolName == "" {
				toolName = "unknown_tool"
			}
			recordTraceEvent(ctx, cfg, "gate_node", "tool_result", toolName, msg.ToolCallID, "success", msg.Content, "")
		case msg.Role == schema.Assistant && strings.TrimSpace(msg.ToolCallID) == "" && (strings.Contains(msg.Content, "[RESOLVED]") || strings.Contains(msg.Content, "[TO_COMPLEX]")):
			recordTraceEvent(ctx, cfg, "gate_node", "route_marker", "gate_agent", "", "success", msg.Content, "")
		}
	}
}

func recordTraceEvent(ctx context.Context, cfg *Config, node string, eventType string, agentOrTool string, toolCallID string, status string, payload string, errSummary string) {
	if cfg == nil || cfg.TraceRecorder == nil {
		return
	}
	meta := traceMetadataFromContext(ctx)
	if strings.TrimSpace(meta.SessionID) == "" || strings.TrimSpace(meta.TurnID) == "" {
		return
	}
	recordCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := cfg.TraceRecorder.RecordEvent(recordCtx, appcontext.OrchestrationTraceEvent{
		SessionID:      meta.SessionID,
		TurnID:         meta.TurnID,
		CheckpointID:   meta.CheckpointID,
		Source:         meta.Source,
		Node:           node,
		EventType:      eventType,
		AgentOrTool:    agentOrTool,
		ToolCallID:     strings.TrimSpace(toolCallID),
		Status:         strings.TrimSpace(status),
		Tags:           buildTraceTags(ctx),
		CompactPayload: payload,
		ErrorSummary:   errSummary,
	}); err != nil && cfg.Logger != nil {
		cfg.Logger.Warn("failed to record orchestration trace event",
			zap.String("node", node),
			zap.String("event_type", eventType),
			zap.Error(err))
	}
}

func statusFromError(err error) string {
	if err != nil {
		return "error"
	}
	return "success"
}

func errorSummary(err error) string {
	if err == nil {
		return ""
	}
	return appcontext.ClipTraceText(err.Error())
}

// streamAgentMessages 流式运行 Agent，通过 schema.Pipe 推送 *schema.Message token 块。
// 用于 answer_node（无工具，无 interrupt 风险）。
func streamAgentMessages(ctx context.Context, agent adk.Agent, msgs []*schema.Message, logger *zap.Logger) (*schema.StreamReader[*schema.Message], error) {
	sr, sw := schema.Pipe[*schema.Message](100)

	go func() {
		defer sw.Close()
		iter := agent.Run(ctx, &adk.AgentInput{
			Messages:        msgs,
			EnableStreaming: true,
		})
		emitCount := 0
		for {
			event, ok := iter.Next()
			if !ok {
				break
			}
			if event == nil {
				continue
			}
			if event.Err != nil {
				if logger != nil {
					logger.Warn("answer_node agent error", zap.Error(event.Err))
				}
				sw.Send(nil, event.Err)
				return
			}
			if event.Output == nil || event.Output.MessageOutput == nil {
				continue
			}
			mo := event.Output.MessageOutput
			if mo.Message != nil {
				if shouldEmitAssistantMessage(mo, mo.Message) {
					emitCount++
					sw.Send(localizeAssistantMessage(ctx, mo.Message), nil)
				}
			} else if mo.MessageStream != nil {
				for {
					chunk, err := mo.MessageStream.Recv()
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						if logger != nil {
							logger.Warn("answer_node stream error", zap.Error(err))
						}
						sw.Send(nil, err)
						return
					}
					if shouldEmitAssistantMessage(mo, chunk) {
						emitCount++
						sw.Send(localizeAssistantMessage(ctx, chunk), nil)
					}
				}
			}
		}
		if logger != nil {
			logger.Info("answer_node stream done", zap.Int("emitted", emitCount))
		}
	}()

	return sr, nil
}

func localizeAssistantMessage(ctx context.Context, msg *schema.Message) *schema.Message {
	if msg == nil || strings.TrimSpace(msg.Content) == "" {
		return msg
	}
	msg.Content = applyTerminology(msg.Content, userLanguageFromState(ctx))
	return msg
}

func buildTraceTags(ctx context.Context) map[string]string {
	language := userLanguageFromState(ctx)
	if language == "" {
		return nil
	}
	return map[string]string{"user_language": language}
}

func prepareAnswerAgentMessages(ctx context.Context, msgs []*schema.Message) []*schema.Message {
	var solvedCtxs []string
	_ = compose.ProcessState[*OrchState](ctx, func(_ context.Context, s *OrchState) error {
		solvedCtxs = s.SolvedContexts
		return nil
	})
	return appendHandoffUserMessage(msgs, "answer_node", solvedCtxs)
}

func prepareComplexAgentMessages(ctx context.Context, msgs []*schema.Message) []*schema.Message {
	var pendingQs []string
	_ = compose.ProcessState[*OrchState](ctx, func(_ context.Context, s *OrchState) error {
		pendingQs = s.PendingQuestions
		return nil
	})
	return appendHandoffUserMessage(msgs, "complex_node", pendingQs)
}

func appendHandoffUserMessage(msgs []*schema.Message, target string, extraCtx []string) []*schema.Message {
	prepared := make([]*schema.Message, 0, len(msgs)+1)
	prepared = append(prepared, msgs...)

	question := lastUserQuestion(msgs)
	var content string
	switch target {
	case "answer_node":
		ctxBlock := ""
		if len(extraCtx) > 0 {
			ctxBlock = "\n\n已解决的知识背景（可直接引用）：\n" + strings.Join(extraCtx, "\n---\n")
		}
		content = fmt.Sprintf(
			"请基于上文中的 knowledge_search_expert 工具结果，直接回答用户原始问题。\n\n原始问题：%s%s\n\n要求：不要输出 [RESOLVED] 或 [TO_COMPLEX] 路由标记；如果知识库结果不足，请明确说明不足之处。",
			question, ctxBlock,
		)
	default:
		pendingBlock := ""
		if len(extraCtx) > 0 {
			pendingBlock = "\n\n知识库未能解决的遗留子问题（请重点攻克）：\n- " + strings.Join(extraCtx, "\n- ")
		}
		content = fmt.Sprintf(
			"Gate Agent 已判断知识库结果不足或检索异常。请忽略内部路由标记，继续处理并直接回答用户原始问题。\n\n原始问题：%s%s\n\n要求：不要输出 [RESOLVED] 或 [TO_COMPLEX] 路由标记；必要时使用可用 Skill 工具或 web_search 攻克遗留问题。",
			question, pendingBlock,
		)
	}
	prepared = append(prepared, schema.UserMessage(content))
	return prepared
}

func lastUserQuestion(msgs []*schema.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i] == nil || msgs[i].Role != schema.User {
			continue
		}
		if content := strings.TrimSpace(msgs[i].Content); content != "" {
			return content
		}
	}
	return "（未找到原始问题）"
}

func previewContent(content string) string {
	content = strings.TrimSpace(content)
	runes := []rune(content)
	if len(runes) > 120 {
		return string(runes[:120])
	}
	return content
}

func shouldEmitAssistantMessage(mo *adk.MessageVariant, msg *schema.Message) bool {
	if msg == nil {
		return false
	}
	role := msg.Role
	if role == "" && mo != nil {
		role = mo.Role
	}
	if role != schema.Assistant {
		return false
	}
	if strings.TrimSpace(msg.ToolCallID) != "" || len(msg.ToolCalls) > 0 {
		return false
	}
	return true
}

// ragResultRouter 路由函数：根据 Gate Agent 输出决定分流到 answer_node 或 complex_node。
//
// 优先级（高到低）：
// 1. OrchState 结构化状态（knowledge_search_expert 写入）：
//   - len(SolvedContexts)>0 && len(PendingQuestions)==0 → answer_node
//   - len(PendingQuestions)>0 → complex_node
//     （工具本轮被调用时，结构化状态优先于 LLM 文本标记，防止 LLM 误判）
//
// 2. Gate Agent 最终助手消息含 [RESOLVED] → answer_node（工具未调用时的 LLM 判断）
// 3. Gate Agent 最终助手消息含 [TO_COMPLEX] → complex_node
// 4. 回退：最近一条 Tool 消息内容有效 → answer_node；无效或空 → complex_node
// 5. 无 Tool 消息（未执行 RAG）→ complex_node
func ragResultRouter(ctx context.Context, msgs []*schema.Message) (string, error) {
	// 优先级 1：读取 OrchState 结构化状态（由 writeKnowledgeSpecialistResultToState 写入）。
	// 仅当本轮确实调用了 knowledge_search_expert（SolvedContexts 或 PendingQuestions 非空）时生效。
	var solvedCtxs, pendingQs []string
	_ = compose.ProcessState[*OrchState](ctx, func(_ context.Context, s *OrchState) error {
		solvedCtxs = s.SolvedContexts
		pendingQs = s.PendingQuestions
		return nil
	})
	if len(pendingQs) > 0 {
		// 有未解决子问题，无论 LLM 标记如何，必须走 complex_node。
		return "complex_node", nil
	}
	if len(solvedCtxs) > 0 {
		// 所有子问题已解决，走 answer_node。
		return "answer_node", nil
	}

	// 优先级 2-3：工具本轮未被调用，回退到 LLM 文本标记。
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		if msg.Role == schema.Assistant && strings.TrimSpace(msg.ToolCallID) == "" {
			content := msg.Content
			if strings.Contains(content, "[RESOLVED]") {
				return "answer_node", nil
			}
			if strings.Contains(content, "[TO_COMPLEX]") {
				return "complex_node", nil
			}
			break
		}
	}

	// 优先级 4：检查最近一条 Tool 消息内容。
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == schema.Tool {
			content := strings.TrimSpace(msgs[i].Content)
			if content == "" || isEmptyRAGResult(content) {
				return "complex_node", nil
			}
			return "answer_node", nil
		}
	}

	// 优先级 5：无 Tool 消息，走 complex_node。
	return "complex_node", nil
}

// isEmptyRAGResult 判断 RAG 工具返回内容是否为空/无效。
func isEmptyRAGResult(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return true
	}
	if isStructuredEmptyRAGResult(content) {
		return true
	}
	lower := strings.ToLower(content)
	return strings.Contains(lower, "未找到") ||
		strings.Contains(lower, "no results") ||
		strings.Contains(lower, "没有找到") ||
		strings.Contains(lower, "检索结果为空") ||
		len([]rune(content)) < 10
}

func isStructuredEmptyRAGResult(content string) bool {
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return false
	}

	status, _ := payload["status"].(string)
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "error", "degraded":
		return true
	}

	if count, ok := payload["count"]; ok && numericValue(count) <= 0 {
		return true
	}
	if results, ok := payload["results"].([]any); ok && len(results) == 0 {
		return true
	}
	return false
}

func numericValue(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	case json.Number:
		n, _ := v.Float64()
		return n
	default:
		return 0
	}
}

// CheckPointStore 类型别名，供 Service 层使用时无需引入 compose 包。
type CheckPointStore = compose.CheckPointStore

// RunCheckpointTTL 是 Redis checkpoint 的默认 TTL。
const RunCheckpointTTL = 24 * time.Hour

// writeKnowledgeSpecialistResultToState 扫描消息列表，找到 knowledge_search_expert 工具返回的
// JSON 结果，解析后写入 OrchState Blackboard（SolvedContexts / PendingQuestions）。
func writeKnowledgeSpecialistResultToState(ctx context.Context, msgs []*schema.Message, logger *zap.Logger) {
	for _, msg := range msgs {
		if msg == nil || msg.Role != schema.Tool {
			continue
		}
		var ksResult KnowledgeSpecialistResult
		if err := json.Unmarshal([]byte(msg.Content), &ksResult); err != nil {
			continue
		}
		if len(ksResult.SolvedContexts)+len(ksResult.PendingQuestions) == 0 {
			continue
		}
		if stateErr := compose.ProcessState[*OrchState](ctx, func(_ context.Context, s *OrchState) error {
			s.SolvedContexts = ksResult.SolvedContexts
			s.PendingQuestions = ksResult.PendingQuestions
			return nil
		}); stateErr != nil && logger != nil {
			logger.Warn("gate_node: failed to write KnowledgeSpecialistResult to OrchState",
				zap.Error(stateErr))
		}
		return // 仅处理第一条匹配的工具消息
	}
}
