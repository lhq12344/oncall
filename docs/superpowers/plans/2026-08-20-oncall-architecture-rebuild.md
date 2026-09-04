# OnCall Architecture Rebuild Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` for implementation, or execute task-by-task with verification gates. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 OnCall 后端重构成参考 `D:\Code\project\mewcode-golang` 的清晰 agent 项目形态：入口薄、模块深、工具注册集中、外部中间件全部通过可选适配器接入，不再让 Redis、MySQL、Elasticsearch、Milvus 侵入核心 agent 流程。

**Architecture:** 采用“核心 agent runtime + domain workflows + adapters”的模块结构。`cmd/oncall` 只负责装配配置和启动 HTTP/SSE；`internal/app` 负责组合；`internal/agent`, `internal/tools`, `internal/workflows`, `internal/session`, `internal/rag`, `internal/observability` 分别持有稳定 seam；Redis/Milvus/Elasticsearch/K8s/Prometheus 只能在 `internal/adapters/*` 中出现。

**Tech Stack:** Go 1.25.5, Eino ADK, standard `net/http` SSE, zap, file/in-memory default state, optional Redis, optional Milvus, optional Elasticsearch, optional K8s/Prometheus adapters.

---

## Evidence Summary

- OnCall 现状入口较厚：`backend/main.go` 直接读取 `.env`、GoFrame config、Redis/Prometheus/K8s/log-sync 配置并绑定 controller，`main` 不是纯启动壳。
- OnCall bootstrap 当前把 logger、hook、Redis、MySQL、Elasticsearch、chat model、embedder 全部放在 `buildInfrastructureLayer`，导致应用启动被 Redis/MySQL 这类非核心依赖强绑定。
- Redis 当前有两个职责：ADK checkpoint store 和 session memory；`buildRuntimeLayer` 已有 in-memory checkpoint fallback，但 `buildStateLayer` 仍强依赖 `RedisClient`。
- MySQL 当前只有 `utility/mysql`、bootstrap 初始化和关闭路径，没有发现业务读写调用；这是删除优先级最高的中间件。
- Elasticsearch 当前是 ops 日志查询和可选 pod log shipper 的适配器依赖，不应该作为核心启动依赖。
- Milvus 当前是知识库/ops case 向量检索和索引依赖；它是 RAG 生产能力，不应删除，但必须改成可选 adapter，默认允许 BM25/file-only 降级。
- 参考项目 `mewcode-golang` 结构清晰的关键不是目录名，而是 seam 清楚：`internal/tools` 统一工具接口/注册，`internal/agent` 持有 agent 执行，`internal/teams`、`internal/memory`、`internal/skills`、`internal/permissions` 独立演进，没有数据库型全局初始化。

## Coding and Encapsulation Rules from `mewcode-golang`

- Prefer struct-owned runtime state over package globals. Follow the `Agent`, `Team`, and `Registry` shape in `mewcode-golang`: constructors fully initialize owned state, methods mutate only that owner, and callers do not reach into unrelated modules.
- Keep constructors as the seam. Each deep module exposes a small constructor such as `New(...)`, `NewRegistry(...)`, or `NewTeam(...)`; setup details, caches, locks, and adapter wiring stay inside the implementation.
- Keep interfaces small and behavior-oriented. Core workflows should accept interfaces such as `LogSearcher`, `MetricQuerier`, `SessionStore`, or `ToolRegistry`; they must not know Redis, MySQL, Milvus, Elasticsearch, K8s, or Prometheus SDK types.
- Keep dependency direction one-way. Runtime/app composition may import adapters; domain modules must not import app/bootstrap/server/adapter packages.
- Use registries for extensibility. Tool, command, slash, and workflow registration should follow the reference pattern: a registry owns available entries; agent/workflow constructors select by role or capability instead of hardcoding concrete packages.
- Avoid pass-through modules. A module earns its place only if deleting it would force duplicated rules across callers. Thin wrappers around external clients belong in adapters, not in core packages.
- Keep tests at the seam. Tests should exercise constructors, registries, stores, and workflow interfaces with fake adapters instead of standing up Redis/MySQL/Milvus/ES.

## Target Architecture

```text
backend/
  cmd/oncall/                 # HTTP/SSE 服务入口，薄 main
  cmd/ragctl/                 # RAG 离线/运维 CLI，保留
  internal/app/               # 应用组合根：config -> adapters -> runtime -> routes
  internal/config/            # typed config，替代散落 g.Cfg()/env 读取
  internal/server/            # net/http routes, SSE, request DTO adapter
  internal/agent/             # agent runner、模型装配、agent catalog
  internal/workflows/         # dialogue, incident, knowledge, execution workflow
  internal/tools/             # 统一 Tool interface, registry, deferred tools, permission wrapper
  internal/session/           # session memory, checkpoint, transcript storage
  internal/rag/               # retrieval domain: BM25/hybrid/fusion/rerank interfaces
  internal/observability/     # incident evidence domain: metrics/logs/k8s snapshots
  internal/adapters/          # redis, milvus, elasticsearch, k8s, prometheus, llm, file
  internal/platform/          # logger, lifecycle, background jobs, clock, ids
  api/chat/v1/                # external DTO only, no runtime logic
```

The intended dependency rule is one-way:

```text
cmd -> app -> server -> workflows -> agent/tools/session/rag/observability interfaces
                              adapters -> external systems
```

No package under `internal/workflows`, `internal/agent`, `internal/tools`, `internal/session`, or `internal/rag` may import Redis, MySQL, Elasticsearch, Milvus SDK, GoFrame, Kubernetes client-go, or Prometheus client directly. Those imports belong only under `internal/adapters/*`.

## Middleware Decision Matrix

| Middleware | Current evidence | Decision | Action |
| --- | --- | --- | --- |
| MySQL | `utility/mysql` has global `*gorm.DB`; only bootstrap init/close references were found, no domain reads/writes. | Delete. | Remove `utility/mysql`, remove `gorm.io/*` and `go-sql-driver/mysql` from `go.mod`, remove `mysql` config block, remove bootstrap MySQL init/close. |
| Redis | Used by session/checkpoint/memory; startup currently fails if Redis ping fails. | Keep as optional adapter, not core dependency. | Add file/in-memory default stores; move Redis implementation to `internal/adapters/redis`; startup logs degraded mode instead of failing. |
| Elasticsearch | Used by ops log query and optional pod log shipper. | Keep optional adapter. | Move to `internal/adapters/elasticsearch`; ops flow consumes `LogSearcher` interface; log sync disabled by default. |
| Milvus | Used by knowledge upload, dialogue retrieval, strategy/ops case indexing. | Keep optional production adapter. | Move SDK code to `internal/adapters/milvus`; RAG core depends on `VectorStore` interface; BM25/file retriever is default fallback. |
| K8s | Required for real incident observation/execution. | Keep optional ops adapter. | Move client creation to `internal/adapters/kubernetes`; workflows depend on `ClusterObserver`/`ClusterExecutor`. |
| Prometheus | Required for metrics evidence. | Keep optional ops adapter. | Move client code to `internal/adapters/prometheus`; workflows depend on `MetricQuerier`. |
| GoFrame | Currently provides HTTP server/config wrappers and leaks into controllers/api. | Remove from core, phase out from HTTP/config. | Replace runtime server with `net/http`; move config to typed loader; keep API structs but remove `g.Meta` dependency when regenerating controllers is no longer needed. |

## Deepening Opportunities

1. **Application composition module**

   **Files:** `backend/main.go`, `backend/internal/bootstrap/*`, new `backend/internal/app/*`, new `backend/cmd/oncall/main.go`.

   **Problem:** current bootstrap has shallow layers that still share one large infrastructure bag. Understanding startup requires bouncing through `main`, `bootstrap`, `utility/*`, controllers, and background jobs.

   **Solution:** create a deep `app.Application` module with one public constructor: `app.New(ctx, config.Config) (*app.Application, error)`. Hide adapter wiring, lifecycle, background jobs, and degraded-mode decisions behind that interface.

   **Benefits:** startup decisions gain locality; tests can build app with fake adapters without Redis/MySQL/ES/Milvus running.

2. **Unified tool system**

   **Files:** `backend/internal/tool/*`, `backend/internal/toolkit/*`, workflow-local tool references, new `backend/internal/tools/*`.

   **Problem:** tools are split across workflow packages and toolkit wrappers, while reference project keeps tool interface, registry, discovery/defer logic, and execution shape in one module.

   **Solution:** make `internal/tools` the only external tool seam. Workflows request `registry.ToolsFor(agent.Kind)`; concrete tools live by domain (`dialogue`, `ops`, `execution`, `rca`, `strategy`) but register through one interface.

   **Benefits:** adding/removing a tool no longer requires editing agent constructors; permission and tool-call result handling become testable through one interface.

3. **Session/checkpoint storage module**

   **Files:** `backend/internal/context/*`, `backend/utility/mem/*`, `backend/internal/bootstrap/runtime.go`, new `backend/internal/session/*`, new `backend/internal/adapters/redis/*`.

   **Problem:** session memory, context manager, checkpoint store, Redis Lua memory, and controller logic are coupled. Redis failure currently changes app startup behavior.

   **Solution:** define `session.Store`, `session.CheckpointStore`, and `session.TranscriptStore`. Implement `memory`, `file`, and `redis` adapters. Default local dev/test uses file or in-memory; Redis is opt-in.

   **Benefits:** Redis becomes an adapter, not architecture. Session tests run without network state.

4. **RAG retrieval module**

   **Files:** `backend/internal/rag/*`, `backend/internal/ai/indexer/*`, `backend/internal/ai/retriever/*`, `backend/internal/knowledge/*`, `backend/cmd/ragctl/*`, new `backend/internal/adapters/milvus/*`.

   **Problem:** RAG domain logic and Milvus SDK setup are mixed. Offline BM25 support exists, but production startup still tries vector infrastructure from agent constructors.

   **Solution:** keep `internal/rag` as pure retrieval domain. Introduce `VectorStore`, `KeywordIndex`, `HybridRetriever`, and `Indexer` interfaces. Move Milvus client/indexer/retriever into adapter package. `ragctl inspect/eval/rebuild-bm25` stays file-backed by default.

   **Benefits:** retrieval algorithms become testable without Milvus; Milvus schema/version migrations are contained.

5. **Observability and incident evidence module**

   **Files:** `backend/internal/workflow/ops/*`, `backend/internal/tool/ops/*`, `backend/utility/kubernetes/*`, `backend/utility/elasticsearch/*`, new `backend/internal/observability/*`, new `backend/internal/adapters/{kubernetes,prometheus,elasticsearch}/*`.

   **Problem:** incident workflow, tools, K8s, Prometheus, and Elasticsearch evidence collection are interleaved.

   **Solution:** move evidence collection behind `observability.Snapshotter`, `MetricQuerier`, `LogSearcher`, and `ClusterReader`. Ops workflow receives one `EvidenceCollector` interface.

   **Benefits:** ops workflow tests can use deterministic fake evidence; ES/K8s/Prometheus outages become degraded evidence, not agent construction failures.

6. **HTTP/SSE server module**

   **Files:** `backend/main.go`, `backend/internal/controller/chat/*`, `backend/api/chat/v1/*`, new `backend/internal/server/*`.

   **Problem:** GoFrame controller, request parsing, SSE streaming, session memory, slash commands, and agent runners are all close together.

   **Solution:** create `server.Router` and `server.ChatHandler` on standard `net/http`. Handler depends on a narrow `ChatService` interface, not concrete agents or Redis.

   **Benefits:** external HTTP shape is preserved while runtime internals can change; SSE tests use `httptest`.

## Implementation Phases

### Phase 0: Freeze behavior before moving seams

- [x] Run current baseline from `backend`: `go test ./...`.
- [x] Add architecture guard test under `backend/internal/arch/architecture_test.go` that fails if forbidden imports appear outside adapters.
- [ ] Add startup test that builds app with no Redis/MySQL/Elasticsearch/Milvus configured and expects success.
- [ ] Add HTTP smoke test for `/api/v1/chat` route construction using fake `ChatService`; do not call external LLM.
- [ ] Commit only tests and guardrails before large moves.

### Phase 1: Delete MySQL cleanly

- [x] Remove MySQL from `backend/internal/bootstrap/application_layers.go` and `backend/internal/bootstrap/app.go` close path.
- [x] Delete `backend/utility/mysql/mysql.go`.
- [x] Remove `mysql` block from `backend/manifest/config/config.yaml` and `backend/hack/config.yaml` if only used as generated scaffold.
- [x] Run `go mod tidy` from `backend`.
- [x] Verify `rg -n "mysql|gorm|MYSQL_DSN|GlobalMySQL|InitMySQL" backend -g "*.go" -g "*.yaml"` returns only domain text/examples, not code dependencies.
- [x] Run `go test ./...`.

### Phase 2: Make Redis optional

- [ ] Create `backend/internal/session/store.go` with `Store`, `CheckpointStore`, and `TranscriptStore` interfaces.
- [ ] Create `backend/internal/session/memory_store.go` and `backend/internal/session/file_store.go`.
- [x] Move Redis implementations from `backend/internal/context/redis_storage.go`, `backend/internal/context/checkpoint_store.go`, and `backend/utility/mem/mem.go` into `backend/internal/adapters/redis`.
- [x] Change app composition so missing/failed Redis selects file/in-memory store and logs degraded mode.
- [x] Update `SessionMemory` to depend on `TranscriptStore`, not package-level Redis utility state.
- [ ] Verify `go test ./internal/session ./internal/adapters/redis ./internal/controller/chat ./internal/bootstrap`.

### Phase 3: Centralize tools like the reference project

- [ ] Create `backend/internal/tools/tool.go` modelled on the reference project's `internal/tools/tool.go`: `Tool`, `Registry`, `Category`, `DeferrableTool`, `SystemTool`.
- [ ] Fold `backend/internal/toolkit` permission wrapper into `internal/tools/gateway.go`.
- [x] Move concrete tool constructors from agent/workflow-local paths into `backend/internal/tools/{dialogue,ops,execution,rca,strategy}`.
- [x] Keep agent constructors dependent on `tools.Registry`, not concrete tool packages.
- [x] Verify with existing tool tests plus targeted workflow/agent tests under `backend/internal/workflow` and `backend/internal/tools`.

### Phase 4: Split workflows from adapters

- [ ] Rename `backend/internal/workflow` to `backend/internal/workflows` after imports are stable.
- [ ] Create `backend/internal/observability` interfaces and fake implementations.
- [x] Move K8s, Prometheus, and Elasticsearch client construction into `backend/internal/adapters/*`.
- [x] Move Elasticsearch SDK client, log query behavior, and bulk indexing behavior behind `backend/internal/adapters/elasticsearch.Client`.
- [x] Move `k8s_monitor` SDK DTO formatting and discovery behavior behind `backend/internal/adapters/kubernetes.Monitor`.
- [x] Move PodLogShipper K8s log streaming behind `backend/internal/adapters/kubernetes.PodLogReader`.
- [x] Move RCA dependency graph K8s discovery behind `backend/internal/adapters/kubernetes` DTOs.
- [x] Move Prometheus SDK query/source-discovery behavior behind `backend/internal/adapters/prometheus.Collector`.
- [ ] Change ops incident workflow to consume `observability.EvidenceCollector`.
- [ ] Keep incident workflow graph and state names unchanged: `incident_analysis -> diagnosis_gate -> plan -> plan_gate -> plan_approval -> execute_plan -> verify_plan -> replan_decider -> final_report`.
- [ ] Verify `go test ./internal/workflows/ops ./internal/tools/ops ./internal/observability`.

### Phase 5: RAG adapter isolation

- [ ] Define `rag.VectorStore`, `rag.KeywordIndex`, and `rag.Indexer` interfaces in `backend/internal/rag`.
- [ ] Move Milvus SDK client/indexer/retriever code under `backend/internal/adapters/milvus`.
- [ ] Keep BM25 as default file-backed local index.
- [ ] Update `knowledge.NewKnowledgeAgent`, dialogue retrieval, strategy/ops case indexing, and `ragctl` to consume interfaces.
- [ ] Add tests proving `ragctl inspect/eval/rebuild-bm25` work without Milvus.
- [ ] Verify `go test ./internal/rag ./internal/knowledge ./cmd/ragctl`.

### Phase 6: Replace GoFrame runtime surface

- [ ] Create `backend/cmd/oncall/main.go` as thin entry point.
- [ ] Create `backend/internal/config` typed loader from env + YAML.
- [ ] Create `backend/internal/server` with standard `net/http`, CORS middleware, response middleware, and SSE helpers.
- [ ] Move `backend/internal/controller/chat` logic into `server.ChatHandler` and `service.ChatService` seams.
- [ ] Keep compatibility routes under `/api/v1`.
- [ ] Remove GoFrame imports from runtime code; keep `api/chat/v1` as DTOs until codegen is replaced.
- [ ] Run `rg -n "github.com/gogf/gf|ghttp|g\.Cfg|g\.Server" backend -g "*.go"` and ensure only legacy API/codegen files remain, then remove dependency when zero runtime imports remain.

### Phase 7: Documentation and migration finish

- [ ] Update `docs/learning/01-architecture-overview.md`, `02-bootstrap-and-request-flow.md`, `07-checkpoint-session-memory.md`, `08-knowledge-rag-tools.md`, and diagrams to match the new architecture.
- [ ] Add `docs/architecture/adr-0001-core-vs-adapters.md` recording the middleware decisions.
- [ ] Add `docs/architecture/adr-0002-no-core-external-sdk-imports.md` recording the import rule.
- [ ] Run docs path/line validation if available from prior audit, then `git diff --check`.
- [ ] Run backend full validation: `go test ./...`.

## Acceptance Criteria

- Backend starts in local dev without Redis, MySQL, Elasticsearch, Milvus, K8s, or Prometheus configured; missing adapters produce explicit degraded capability logs.
- MySQL code and dependencies are absent from `backend/go.mod` and source code, except historical docs if intentionally retained.
- Redis code exists only under `internal/adapters/redis`; core session code has in-memory/file implementations and tests.
- Elasticsearch/Milvus/K8s/Prometheus SDK imports exist only under `internal/adapters/*`.
- Tool registration has one public registry seam; workflow packages do not import concrete tool implementations directly.
- HTTP/SSE compatibility under `/api/v1` remains intact.
- `go test ./...` passes from `backend`.
- `rg` architecture guard commands show no forbidden imports outside adapter packages.

## Suggested Work Allocation

- `executor`: Phase 1 MySQL deletion and go.mod cleanup.
- `executor`: Phase 2 session/Redis adapter split.
- `executor`: Phase 3 tool registry consolidation.
- `executor`: Phase 4 observability adapter split.
- `executor`: Phase 5 RAG/Milvus adapter split.
- `test-engineer`: architecture guard tests, startup tests, SSE handler tests, RAG offline tests.
- `critic` or `verifier`: review dependency-direction violations and middleware decisions before merge.

## Risks and Mitigations

- **Risk:** Large import moves cause accidental behavior loss. **Mitigation:** introduce interfaces and tests first; move one seam at a time.
- **Risk:** Redis removal breaks resume/checkpoint behavior. **Mitigation:** make Redis optional only after file/in-memory checkpoint tests pass.
- **Risk:** Milvus optional mode weakens production RAG. **Mitigation:** keep Milvus adapter and add explicit degraded-mode response when vector retrieval is unavailable.
- **Risk:** GoFrame removal touches generated API/controller assumptions. **Mitigation:** migrate HTTP runtime first while leaving DTO files stable; remove codegen dependency last.
- **Risk:** Existing dirty worktree already contains moved/deleted files. **Mitigation:** preserve current uncommitted changes; execute phases against actual `git status` and verify after each phase.

## Current Validation Snapshot

- `go test ./...` from `backend` passed on 2026-08-20 against the current dirty worktree.
- `git status --short` showed existing uncommitted architecture changes before this plan was added, including moved tool packages and bootstrap layer files. Do not treat this plan as the source of those changes.
- 2026-08-20 update: Phase 1 MySQL deletion, Phase 2 Redis optional adapter split, and Phase 3 tools registry consolidation were verified with `go test ./...`, `go test ./internal/arch`, and `git diff --check` from the current worktree.
- 2026-08-20 update: Redis SDK imports are restricted to `backend/internal/adapters/redis` plus the architecture guard test; agent constructors no longer import `internal/permissions` or `internal/toolkit` directly.
- 2026-08-20 update: Elasticsearch SDK imports are restricted to `backend/internal/adapters/elasticsearch` plus the architecture guard test. `ESLogQueryTool` now depends on a `logSearcher` seam, and `PodLogShipper` bulk indexing calls `elasticsearch.Client.BulkIndexDocuments` instead of the SDK directly. Verified with `go test ./internal/arch ./internal/adapters/elasticsearch ./internal/adapters/kubernetes ./internal/bootstrap ./internal/tools/ops ./internal/workflow/ops -count=1`.
- 2026-08-20 update: `k8s_monitor` now depends on a `clusterMonitor` seam and delegates Kubernetes SDK resource listing/formatting to `internal/adapters/kubernetes.Monitor`. RCA dependency graph discovery now uses SDK-free Kubernetes DTOs; PodLogShipper log streaming now uses `PodLogReader`. Prometheus query/source-discovery now lives behind `internal/adapters/prometheus.Collector`. Verified with `go test ./internal/arch ./internal/adapters/elasticsearch ./internal/adapters/kubernetes ./internal/adapters/prometheus ./internal/bootstrap ./internal/tools ./internal/tools/ops ./internal/tools/rca ./internal/workflow/ops -count=1`.
- 2026-08-20 update: Full backend validation passed with repo-local cache: `go test ./...` from `backend`. Whitespace validation passed with `git diff --check` from repo root.
