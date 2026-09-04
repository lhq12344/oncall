package bootstrap

import (
	esadapter "go_agent/internal/adapters/elasticsearch"
	redisadapter "go_agent/internal/adapters/redis"
	"go_agent/internal/ai/models"
	"go_agent/internal/commands/slash"
	appcontext "go_agent/internal/context"
	hookpkg "go_agent/internal/hooks"
	"go_agent/internal/model"
	"go_agent/internal/telemetry"
	"go_agent/internal/tools/policy"
	"go_agent/internal/workflow/ops"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/compose"
	"go.uber.org/zap"
)

// Infrastructure contains process-level adapters and shared clients.
type Infrastructure struct {
	Logger           *zap.Logger
	HookEngine       *hookpkg.Engine
	RedisClient      *redisadapter.Client
	Elasticsearch    *esadapter.Client
	ChatModel        *models.ChatModel
	DialogueEmbedder embedding.Embedder
	ModelCatalog     *model.Catalog
	Telemetry        *telemetry.Recorder
	ToolPolicy       *policy.Engine
}

// StateLayer contains session state modules derived from infrastructure.
type StateLayer struct {
	ContextManager *appcontext.ContextManager
}

// AgentLayer contains domain agents and agent-facing integration modules.
type AgentLayer struct {
	DialogueAgent  adk.ResumableAgent
	KnowledgeAgent adk.Agent
	OpsIntegration *ops.IntegratedOpsExecutor
	OpsAgent       adk.Agent
}

// RuntimeLayer contains ADK runtime modules consumed by transport controllers.
type RuntimeLayer struct {
	CheckPointStore compose.CheckPointStore
	SessionMemory   *appcontext.SessionMemory
	SlashRegistry   *slash.Registry
	ChatRunner      *adk.Runner
	OpsRunner       *adk.Runner
	RootAgentName   string
	OpsRootName     string
	WorkDir         string
	ToolPolicy      *policy.Engine
}

// BackgroundLayer contains optional long-running background jobs.
type BackgroundLayer struct {
	PodLogShipper *ops.PodLogShipper
}
