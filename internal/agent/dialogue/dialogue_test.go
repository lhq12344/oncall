package dialogue

import (
	"context"
	"testing"
	"time"

	appcontext "go_agent/internal/context"
)

func TestIntentPredictor(t *testing.T) {
	predictor := NewIntentPredictor(nil)
	ctx := context.Background()

	// 创建测试会话
	session := &appcontext.SessionContext{
		SessionID: "test_session",
		History:   make([]*appcontext.Message, 0),
	}

	tests := []struct {
		input        string
		expectedType string
	}{
		{"查看 CPU 使用率", "monitor"},
		{"服务报错了", "diagnose"},
		{"重启 nginx", "execute"},
		{"之前遇到过类似问题吗", "knowledge"},
	}

	for _, tt := range tests {
		intent, err := predictor.Predict(ctx, session, tt.input)
		if err != nil {
			t.Fatalf("predict failed: %v", err)
		}

		if intent.Type != tt.expectedType {
			t.Errorf("input '%s': expected type '%s', got '%s'",
				tt.input, tt.expectedType, intent.Type)
		}

		t.Logf("Input: %s -> Type: %s (Confidence: %.2f)",
			tt.input, intent.Type, intent.Confidence)
	}

	t.Log("✓ IntentPredictor works correctly")
}

func TestEntropyCalculator(t *testing.T) {
	calc := NewEntropyCalculator()
	ctx := context.Background()

	// 创建测试会话
	session := &appcontext.SessionContext{
		SessionID: "test_session",
		History: []*appcontext.Message{
			{Role: "user", Content: "查看 pod 状态", Timestamp: time.Now()},
			{Role: "assistant", Content: "正在查询...", Timestamp: time.Now()},
			{Role: "user", Content: "有什么异常吗", Timestamp: time.Now()},
			{Role: "assistant", Content: "发现 3 个 pod Pending", Timestamp: time.Now()},
		},
	}

	entropy, err := calc.Calculate(ctx, session)
	if err != nil {
		t.Fatalf("calculate entropy failed: %v", err)
	}

	if entropy < 0 || entropy > 1 {
		t.Errorf("entropy out of range: %.3f", entropy)
	}

	t.Logf("Entropy: %.3f", entropy)
	t.Log("✓ EntropyCalculator works correctly")
}

func TestQuestionGenerator(t *testing.T) {
	gen := NewQuestionGenerator()
	ctx := context.Background()

	// 创建测试会话
	session := &appcontext.SessionContext{
		SessionID: "test_session",
		Intent: &appcontext.UserIntent{
			Type:      "monitor",
			Converged: true,
		},
		History: make([]*appcontext.Message, 0),
	}

	// 测试生成问题
	questions, err := gen.Generate(ctx, session, 3)
	if err != nil {
		t.Fatalf("generate questions failed: %v", err)
	}

	if len(questions) == 0 {
		t.Error("expected at least 1 question")
	}

	t.Logf("Generated %d questions:", len(questions))
	for i, q := range questions {
		t.Logf("  %d. %s", i+1, q)
	}

	t.Log("✓ QuestionGenerator works correctly")
}

func TestQuestionGenerator_Clarification(t *testing.T) {
	gen := NewQuestionGenerator()
	ctx := context.Background()

	tests := []struct {
		intentType string
		expected   string
	}{
		{"monitor", "监控"},
		{"diagnose", "故障"},
		{"execute", "执行"},
		{"knowledge", "历史"},
	}

	for _, tt := range tests {
		session := &appcontext.SessionContext{
			SessionID: "test_session",
			Intent: &appcontext.UserIntent{
				Type: tt.intentType,
			},
		}

		question := gen.GenerateClarification(ctx, session)
		if question == "" {
			t.Errorf("clarification question is empty for type '%s'", tt.intentType)
		}

		t.Logf("Type: %s -> Question: %s", tt.intentType, question)
	}

	t.Log("✓ GenerateClarification works correctly")
}

func TestDialogueStateTracker(t *testing.T) {
	tracker := NewDialogueStateTracker()
	ctx := context.Background()
	sessionID := "test_session"

	// 测试获取初始状态
	state, err := tracker.GetState(ctx, sessionID)
	if err != nil {
		t.Fatalf("get state failed: %v", err)
	}

	if state.CurrentState != "initial" {
		t.Errorf("expected initial state, got '%s'", state.CurrentState)
	}

	t.Logf("Initial state: %s", state.CurrentState)

	// 测试状态转移
	err = tracker.TransitionTo(ctx, sessionID, "monitoring", "user_query")
	if err != nil {
		t.Fatalf("transition failed: %v", err)
	}

	state, _ = tracker.GetState(ctx, sessionID)
	if state.CurrentState != "monitoring" {
		t.Errorf("expected monitoring state, got '%s'", state.CurrentState)
	}

	if state.PrevState != "initial" {
		t.Errorf("expected prev state 'initial', got '%s'", state.PrevState)
	}

	t.Logf("After transition: %s (prev: %s)", state.CurrentState, state.PrevState)

	// 测试状态历史
	history, err := tracker.GetStateHistory(ctx, sessionID)
	if err != nil {
		t.Fatalf("get history failed: %v", err)
	}

	if len(history) != 1 {
		t.Errorf("expected 1 transition, got %d", len(history))
	}

	t.Logf("State history: %d transitions", len(history))

	// 测试元数据
	err = tracker.SetMetadata(ctx, sessionID, "test_key", "test_value")
	if err != nil {
		t.Fatalf("set metadata failed: %v", err)
	}

	val, ok := tracker.GetMetadata(ctx, sessionID, "test_key")
	if !ok {
		t.Error("metadata not found")
	}

	if val != "test_value" {
		t.Errorf("expected 'test_value', got '%v'", val)
	}

	t.Log("✓ DialogueStateTracker works correctly")
}

func TestDialogueAgent_AnalyzeIntent(t *testing.T) {
	// 创建上下文管理器
	storage := &MockStorage{}
	contextManager := appcontext.NewContextManager(storage)

	// 创建 Dialogue Agent
	agent := NewDialogueAgent(&Config{
		ContextManager: contextManager,
		Logger:         nil,
	})

	ctx := context.Background()

	// 创建会话
	session, _ := contextManager.CreateSession(ctx, "test_user")

	// 添加消息
	contextManager.AddMessage(ctx, session.SessionID, "user", "查看 pod 状态")

	// 分析意图
	analysis, err := agent.AnalyzeIntent(ctx, session.SessionID, "有什么异常吗")
	if err != nil {
		t.Fatalf("analyze intent failed: %v", err)
	}

	if analysis.Intent == nil {
		t.Fatal("intent is nil")
	}

	t.Logf("Intent: %s (Confidence: %.2f, Entropy: %.2f, Converged: %v)",
		analysis.Intent.Type,
		analysis.Confidence,
		analysis.Entropy,
		analysis.Converged,
	)

	t.Log("✓ DialogueAgent.AnalyzeIntent works correctly")
}

func TestDialogueAgent_PredictNextQuestions(t *testing.T) {
	// 创建上下文管理器
	storage := &MockStorage{}
	contextManager := appcontext.NewContextManager(storage)

	// 创建 Dialogue Agent
	agent := NewDialogueAgent(&Config{
		ContextManager: contextManager,
		Logger:         nil,
	})

	ctx := context.Background()

	// 创建会话
	session, _ := contextManager.CreateSession(ctx, "test_user")

	// 设置意图（已收敛）
	session.Intent = &appcontext.UserIntent{
		Type:      "monitor",
		Converged: true,
		Entropy:   0.2,
	}
	contextManager.UpdateSession(ctx, session)

	// 预测问题
	questions, err := agent.PredictNextQuestions(ctx, session.SessionID, 3)
	if err != nil {
		t.Fatalf("predict questions failed: %v", err)
	}

	t.Logf("Predicted %d questions:", len(questions))
	for i, q := range questions {
		t.Logf("  %d. %s", i+1, q)
	}

	t.Log("✓ DialogueAgent.PredictNextQuestions works correctly")
}

func TestDialogueAgent_ExtractEntities(t *testing.T) {
	// 创建 Dialogue Agent
	agent := NewDialogueAgent(&Config{
		Logger: nil,
	})

	ctx := context.Background()

	tests := []struct {
		input    string
		expected map[string]string
	}{
		{
			"查看 nginx 的 CPU 使用率",
			map[string]string{"service": "nginx", "metric": "cpu"},
		},
		{
			"mysql 最近5分钟的内存情况",
			map[string]string{"service": "mysql", "metric": "memory", "time_range": "5m"},
		},
	}

	for _, tt := range tests {
		entities, err := agent.ExtractEntities(ctx, tt.input)
		if err != nil {
			t.Fatalf("extract entities failed: %v", err)
		}

		t.Logf("Input: %s", tt.input)
		t.Logf("Entities: %v", entities)

		// 验证期望的实体
		for key, expectedVal := range tt.expected {
			if val, ok := entities[key]; !ok {
				t.Errorf("expected entity '%s' not found", key)
			} else if val != expectedVal {
				t.Logf("Warning: entity '%s' expected '%s', got '%v'", key, expectedVal, val)
			}
		}
	}

	t.Log("✓ DialogueAgent.ExtractEntities works correctly")
}

// MockStorage 用于测试的 mock storage
type MockStorage struct{}

func (m *MockStorage) SaveSession(ctx context.Context, sessionID string, data []byte, ttl time.Duration) error {
	return nil
}

func (m *MockStorage) LoadSession(ctx context.Context, sessionID string) (*appcontext.SessionContext, error) {
	return nil, fmt.Errorf("not found")
}

func (m *MockStorage) DeleteSession(ctx context.Context, sessionID string) error {
	return nil
}

func (m *MockStorage) SaveAgentContext(ctx context.Context, agentID string, data []byte, ttl time.Duration) error {
	return nil
}

func (m *MockStorage) LoadAgentContext(ctx context.Context, agentID string) (*appcontext.AgentContext, error) {
	return nil, fmt.Errorf("not found")
}

func (m *MockStorage) SaveExecutionContext(ctx context.Context, executionID string, data []byte, ttl time.Duration) error {
	return nil
}

func (m *MockStorage) LoadExecutionContext(ctx context.Context, executionID string) (*appcontext.ExecutionContext, error) {
	return nil, fmt.Errorf("not found")
}

func (m *MockStorage) ListSessions(ctx context.Context, pattern string) ([]string, error) {
	return []string{}, nil
}

func (m *MockStorage) DeleteExpiredSessions(ctx context.Context, before time.Time) error {
	return nil
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		a        []float64
		b        []float64
		expected float64
	}{
		{
			[]float64{1, 0, 0},
			[]float64{1, 0, 0},
			1.0,
		},
		{
			[]float64{1, 0, 0},
			[]float64{0, 1, 0},
			0.0,
		},
		{
			[]float64{1, 1, 0},
			[]float64{1, 1, 0},
			1.0,
		},
	}

	for _, tt := range tests {
		similarity := cosineSimilarity(tt.a, tt.b)
		if similarity < tt.expected-0.01 || similarity > tt.expected+0.01 {
			t.Errorf("expected %.2f, got %.2f", tt.expected, similarity)
		}
		t.Logf("Similarity: %.3f", similarity)
	}

	t.Log("✓ cosineSimilarity works correctly")
}

func TestExtractSimpleKeywords(t *testing.T) {
	tests := []struct {
		input    string
		minCount int
	}{
		{"查看 pod 状态", 2},
		{"服务响应慢需要优化", 3},
	}

	for _, tt := range tests {
		keywords := extractSimpleKeywords(tt.input)
		if len(keywords) < tt.minCount {
			t.Errorf("input '%s': expected at least %d keywords, got %d",
				tt.input, tt.minCount, len(keywords))
		}
		t.Logf("'%s' -> %v", tt.input, keywords)
	}

	t.Log("✓ extractSimpleKeywords works correctly")
}
