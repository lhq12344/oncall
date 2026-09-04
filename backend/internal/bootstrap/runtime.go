package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	redisadapter "go_agent/internal/adapters/redis"
	"go_agent/internal/commands/slash"
	appcontext "go_agent/internal/context"
	"go_agent/internal/tools/policy"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
)

func buildRuntimeLayer(ctx context.Context, assembly *Assembly) error {
	if assembly == nil {
		return fmt.Errorf("assembly is required")
	}
	if assembly.App == nil {
		assembly.App = &Application{}
	}
	if assembly.Infra == nil {
		assembly.Infra = &Infrastructure{}
	}
	if assembly.Agents == nil {
		assembly.Agents = &AgentLayer{}
	}
	if assembly.Infra.ToolPolicy == nil {
		assembly.Infra.ToolPolicy = policy.NewEngine("")
	}

	checkpointStore := compose.CheckPointStore(newInMemoryCheckPointStore())
	if assembly.Infra.RedisClient != nil {
		checkpointStore = redisadapter.NewCheckPointStore(assembly.Infra.RedisClient, "oncall", 24*time.Hour)
	}

	workDir := defaultRuntimeWorkDir()
	runtime := &RuntimeLayer{
		CheckPointStore: checkpointStore,
		SessionMemory:   appcontext.NewSessionMemory(nil, assembly.Infra.Logger),
		SlashRegistry:   slash.CreateDefaultRegistry(workDir),
		RootAgentName:   runtimeAgentName(ctx, assembly.Agents.DialogueAgent, "dialogue_agent"),
		OpsRootName:     runtimeAgentName(ctx, assembly.Agents.OpsAgent, "ops_agent"),
		WorkDir:         workDir,
		ToolPolicy:      assembly.Infra.ToolPolicy,
	}

	if assembly.Agents.DialogueAgent != nil {
		runtime.ChatRunner = adk.NewRunner(ctx, adk.RunnerConfig{
			Agent:           assembly.Agents.DialogueAgent,
			EnableStreaming: true,
			CheckPointStore: checkpointStore,
		})
	}
	if assembly.Agents.OpsAgent != nil {
		runtime.OpsRunner = adk.NewRunner(ctx, adk.RunnerConfig{
			Agent:           assembly.Agents.OpsAgent,
			EnableStreaming: true,
			CheckPointStore: checkpointStore,
		})
	}

	assembly.Runtime = runtime
	assembly.App.Runtime = runtime
	return nil
}

func runtimeAgentName(ctx context.Context, agent adk.Agent, fallback string) string {
	if agent != nil {
		if name := strings.TrimSpace(agent.Name(ctx)); name != "" {
			return name
		}
	}
	return fallback
}

func defaultRuntimeWorkDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

type inMemoryCheckPointStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newInMemoryCheckPointStore() compose.CheckPointStore {
	return &inMemoryCheckPointStore{data: make(map[string][]byte)}
}

func (s *inMemoryCheckPointStore) Get(_ context.Context, checkpointID string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.data[checkpointID]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), data...), true, nil
}

func (s *inMemoryCheckPointStore) Set(_ context.Context, checkpointID string, checkpoint []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[checkpointID] = append([]byte(nil), checkpoint...)
	return nil
}
