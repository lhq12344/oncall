package toolregistry

import (
	"context"
	"fmt"
	"strings"

	"go_agent/internal/ai/models"
	dialoguetools "go_agent/internal/tools/dialogue"
	executiontools "go_agent/internal/tools/execution"
	opstools "go_agent/internal/tools/ops"
	"go_agent/internal/tools/policy/permissions"
	rcatools "go_agent/internal/tools/rca"
	toolruntime "go_agent/internal/tools/runtime"
	strategytools "go_agent/internal/tools/strategy"

	"github.com/cloudwego/eino/components/embedding"
	einoindexer "github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/components/retriever"
	einotool "github.com/cloudwego/eino/components/tool"
	"go.uber.org/zap"
)

type BaseTool = einotool.BaseTool

type ToolExposure string

const (
	ToolExposureAlways          ToolExposure = "always"
	ToolExposureDeferredGateway ToolExposure = "deferred_gateway"
)

type AgentKind string

const (
	AgentDialogue    AgentKind = "dialogue_agent"
	AgentOpsIncident AgentKind = "ops_incident_agent"
	AgentRCA         AgentKind = "rca_agent"
	AgentPlan        AgentKind = "plan_agent"
	AgentExecution   AgentKind = "execution_agent"
	AgentStrategy    AgentKind = "strategy_agent"
)

type Dependencies struct {
	ChatModel          *models.ChatModel
	Embedder           embedding.Embedder
	KnowledgeRetriever retriever.Retriever
	OpsCaseRetriever   retriever.Retriever
	OpsCaseIndexer     einoindexer.Indexer
	KubeConfig         string
	PrometheusURL      string
	EnableToolLLM      bool
	Logger             *zap.Logger
}

type factory func() (BaseTool, error)

type registration struct {
	name        string
	agents      map[AgentKind]struct{}
	optionalFor map[AgentKind]struct{}
	build       factory
}

type Registry struct {
	deps    Dependencies
	entries []registration
}

func NewRegistry(deps Dependencies) *Registry {
	r := &Registry{deps: deps}
	r.entries = r.defaultRegistrations()
	return r
}

func (r *Registry) ToolsForAgent(ctx context.Context, agent AgentKind) ([]BaseTool, error) {
	if !isKnownAgent(agent) {
		return nil, fmt.Errorf("unknown agent kind %q", agent)
	}

	tools := make([]BaseTool, 0)
	seen := make(map[string]struct{})
	for _, entry := range r.entries {
		if _, ok := entry.agents[agent]; !ok {
			continue
		}
		if _, duplicate := seen[entry.name]; duplicate {
			return nil, fmt.Errorf("tool %q is registered more than once for %s", entry.name, agent)
		}

		instance, err := entry.build()
		if err != nil {
			if _, optional := entry.optionalFor[agent]; optional {
				if r.deps.Logger != nil {
					r.deps.Logger.Warn("optional tool initialization failed",
						zap.String("agent", string(agent)),
						zap.String("tool", entry.name),
						zap.Error(err))
				}
				continue
			}
			return nil, fmt.Errorf("initialize tool %s for %s: %w", entry.name, agent, err)
		}
		if instance == nil {
			return nil, fmt.Errorf("tool %q factory returned nil for %s", entry.name, agent)
		}

		info, err := instance.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("load tool info for %s: %w", entry.name, err)
		}
		if info == nil || strings.TrimSpace(info.Name) == "" {
			return nil, fmt.Errorf("tool %q returned empty Eino ToolInfo", entry.name)
		}
		if info.Name != entry.name {
			return nil, fmt.Errorf("tool registration name %q does not match Eino ToolInfo name %q", entry.name, info.Name)
		}

		seen[entry.name] = struct{}{}
		tools = append(tools, instance)
	}
	return tools, nil
}

func (r *Registry) ExecutableToolsForAgent(ctx context.Context, agent AgentKind, exposure ToolExposure) ([]BaseTool, error) {
	deferredTools, err := r.ToolsForAgent(ctx, agent)
	if err != nil {
		return nil, err
	}

	checker := permissions.NewChecker(permissions.Options{})
	switch exposure {
	case ToolExposureAlways:
		return toolruntime.BuildAlwaysEinoTools(ctx, checker, deferredTools...), nil
	case ToolExposureDeferredGateway:
		return toolruntime.BuildDeferredGatewayEinoTools(ctx, checker, deferredTools...), nil
	default:
		return nil, fmt.Errorf("unknown tool exposure %q", exposure)
	}
}

func (r *Registry) defaultRegistrations() []registration {
	agents := func(values ...AgentKind) map[AgentKind]struct{} {
		out := make(map[AgentKind]struct{}, len(values))
		for _, value := range values {
			out[value] = struct{}{}
		}
		return out
	}

	return []registration{
		{
			name:   "intent_analysis",
			agents: agents(AgentDialogue),
			build: func() (BaseTool, error) {
				return dialoguetools.NewIntentAnalysisTool(r.deps.ChatModel, r.deps.Embedder, r.deps.Logger, r.deps.EnableToolLLM), nil
			},
		},
		{
			name:   "request_detail_selection",
			agents: agents(AgentDialogue),
			build: func() (BaseTool, error) {
				return dialoguetools.NewDetailSelectionTool(r.deps.Logger), nil
			},
		},
		{
			name:   "knowledge_retrieve",
			agents: agents(AgentDialogue),
			build: func() (BaseTool, error) {
				return dialoguetools.NewKnowledgeRetrieveTool(r.deps.KnowledgeRetriever, r.deps.Logger), nil
			},
		},
		{
			name:   "ops_case_retrieve",
			agents: agents(AgentDialogue),
			build: func() (BaseTool, error) {
				return dialoguetools.NewOpsCaseRetrieveTool(r.deps.OpsCaseRetriever, r.deps.Logger), nil
			},
		},
		{
			name:   "bash_execute_with_approval",
			agents: agents(AgentDialogue),
			build: func() (BaseTool, error) {
				return dialoguetools.NewBashApprovalTool(r.deps.Logger), nil
			},
		},
		{
			name:   "web_search",
			agents: agents(AgentDialogue),
			build: func() (BaseTool, error) {
				return dialoguetools.NewWebSearchTool(r.deps.Logger), nil
			},
		},
		{
			name:        "k8s_monitor",
			agents:      agents(AgentDialogue, AgentOpsIncident, AgentRCA),
			optionalFor: agents(AgentDialogue),
			build: func() (BaseTool, error) {
				return opstools.NewK8sMonitorTool(r.deps.KubeConfig, r.deps.Logger)
			},
		},
		{
			name:        "metrics_collector",
			agents:      agents(AgentDialogue, AgentOpsIncident, AgentRCA),
			optionalFor: agents(AgentDialogue),
			build: func() (BaseTool, error) {
				return opstools.NewMetricsCollectorTool(r.deps.PrometheusURL, r.deps.Logger)
			},
		},
		{
			name:   "es_log_query",
			agents: agents(AgentOpsIncident),
			build: func() (BaseTool, error) {
				return opstools.NewESLogQueryTool(r.deps.Logger)
			},
		},
		{
			name:   "time_query",
			agents: agents(AgentOpsIncident, AgentRCA),
			build: func() (BaseTool, error) {
				return rcatools.NewTimeQueryTool(r.deps.Logger), nil
			},
		},
		{
			name:   "build_dependency_graph",
			agents: agents(AgentOpsIncident, AgentRCA),
			build: func() (BaseTool, error) {
				return rcatools.NewBuildDependencyGraphTool(r.deps.KubeConfig, r.deps.Logger), nil
			},
		},
		{
			name:   "correlate_signals",
			agents: agents(AgentOpsIncident, AgentRCA),
			build: func() (BaseTool, error) {
				return rcatools.NewCorrelateSignalsTool(r.deps.Logger), nil
			},
		},
		{
			name:   "infer_root_cause",
			agents: agents(AgentOpsIncident, AgentRCA),
			build: func() (BaseTool, error) {
				return rcatools.NewInferRootCauseTool(r.deps.ChatModel, r.deps.Logger), nil
			},
		},
		{
			name:   "analyze_impact",
			agents: agents(AgentOpsIncident, AgentRCA),
			build: func() (BaseTool, error) {
				return rcatools.NewAnalyzeImpactTool(r.deps.Logger), nil
			},
		},
		{
			name:   "normalize_plan",
			agents: agents(AgentPlan),
			build: func() (BaseTool, error) {
				return executiontools.NewNormalizePlanTool(r.deps.ChatModel, r.deps.Logger), nil
			},
		},
		{
			name:   "generate_plan",
			agents: agents(AgentPlan),
			build: func() (BaseTool, error) {
				return executiontools.NewGeneratePlanTool(r.deps.ChatModel, r.deps.Logger), nil
			},
		},
		{
			name:   "validate_plan",
			agents: agents(AgentPlan),
			build: func() (BaseTool, error) {
				return executiontools.NewValidatePlanTool(r.deps.Logger), nil
			},
		},
		{
			name:   "execute_step",
			agents: agents(AgentExecution),
			build: func() (BaseTool, error) {
				return executiontools.NewExecuteStepTool(r.deps.Logger), nil
			},
		},
		{
			name:   "validate_result",
			agents: agents(AgentExecution),
			build: func() (BaseTool, error) {
				return executiontools.NewValidateResultTool(r.deps.Logger), nil
			},
		},
		{
			name:   "rollback",
			agents: agents(AgentExecution),
			build: func() (BaseTool, error) {
				return executiontools.NewRollbackTool(r.deps.Logger), nil
			},
		},
		{
			name:   "evaluate_strategy",
			agents: agents(AgentStrategy),
			build: func() (BaseTool, error) {
				return strategytools.NewEvaluateStrategyTool(r.deps.Logger), nil
			},
		},
		{
			name:   "optimize_strategy",
			agents: agents(AgentStrategy),
			build: func() (BaseTool, error) {
				return strategytools.NewOptimizeStrategyTool(r.deps.ChatModel, r.deps.Logger), nil
			},
		},
		{
			name:   "update_knowledge",
			agents: agents(AgentStrategy),
			build: func() (BaseTool, error) {
				return strategytools.NewUpdateKnowledgeTool(r.deps.OpsCaseIndexer, r.deps.Logger), nil
			},
		},
		{
			name:   "prune_knowledge",
			agents: agents(AgentStrategy),
			build: func() (BaseTool, error) {
				return strategytools.NewPruneKnowledgeTool(r.deps.Logger), nil
			},
		},
	}
}

func isKnownAgent(agent AgentKind) bool {
	switch agent {
	case AgentDialogue, AgentOpsIncident, AgentRCA, AgentPlan, AgentExecution, AgentStrategy:
		return true
	default:
		return false
	}
}
