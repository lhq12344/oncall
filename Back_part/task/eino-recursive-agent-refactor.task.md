# Eino Recursive Agent Refactor Tasks

Source plan: `Back_part/.claude/PRPs/plans/eino-recursive-agent-refactor.plan.md`

Goal: upgrade Gate -> Answer/Complex orchestration into a subgraph-tool pattern
with `knowledge_search_expert`, blackboard state, multi-role model routing, and
durable session/checkpoint behavior.

## Task 1: Multi-Role Model Config

- Update `internal/logic/ai/models/open_ai.go`.
- Add `GetChatModelForRole(ctx, role string) (*ChatModel, error)`.
- Support `gate`, `subgraph`, and `complex`; unknown role falls back to default.
- Use config keys `ds_<role>_model.model/api_key/base_url`.
- Use env fallback keys `DS_<ROLE>_MODEL_MODEL/API_KEY/BASE_URL`.
- Extract shared model construction into private `buildChatModel`.
- Validate: `go build ./internal/logic/ai/models/...`.

## Task 2: KnowledgeSpecialist Result And Subgraph

- Create/update `internal/logic/agent/dialogue/knowledge_specialist.go`.
- Add `KnowledgeSpecialistResult` with solved contexts and pending questions.
- Build `compose.Runnable[string, *KnowledgeSpecialistResult]`.
- Decompose user question into at most 5 atomic subquestions.
- Run parallel RAG with bounded timeout.
- Evaluate each subquestion as solved or pending.
- Connect graph `START -> decompose -> parallel_rag -> evaluate -> END`.

## Task 3: knowledge_search_expert Tool

- Add `NewKnowledgeSearchExpertTool(...)`.
- Tool name: `knowledge_search_expert`.
- Input JSON: `{"question":"..."}`.
- Output JSON: `{"solved_contexts":[...],"pending_questions":[...]}`.
- On failure, return pending JSON instead of aborting the ReAct loop.

## Task 4: Agent Config And Factory Updates

- Add `GateModel`, `SubgraphModel`, `ComplexModel` to dialogue `Config`.
- Add `resolveModel(preferred, fallback)`.
- Gate uses `GateModel`, replacing direct `knowledge_retrieve` with `knowledge_search_expert`.
- Answer uses default `ChatModel` and solved contexts.
- Complex uses `ComplexModel`, pending questions, and keeps skill middleware.
- Validate: `go build ./internal/logic/agent/dialogue/...`.

## Task 5: OrchState Blackboard And Routing

- Add `SolvedContexts` and `PendingQuestions` to `OrchState`.
- Parse `knowledge_search_expert` tool JSON in gate node.
- Write solved/pending data into `OrchState`.
- Route solved cases to Answer and pending cases to Complex.
- Inject solved context into Answer handoff messages.
- Inject pending questions into Complex handoff messages.

## Task 6: App Initialization

- Update `internal/logic/app/app.go`.
- Initialize gate, subgraph, and complex role models.
- Warn and fall back when optional role model init fails.
- Pass models and `EINO_EXT_SKILLS_DIR` into `dialogue.Config`.

## Task 7: session_messages Model

- Create/update `internal/model/session.go`.
- Add `SessionMessage` GORM model for session id, role, content, tool calls, tool call id, turn seq, and created time.
- Table name: `session_messages`.

## Task 8: SessionMemory Dual Persistence

- Update `internal/logic/session/session_memory.go`.
- Add optional `*gorm.DB`.
- After Redis persistence succeeds, asynchronously write prompt messages to MySQL.
- Nil DB is no-op; async DB failures are Warn logs only.

## Task 9: Checkpoint Recovery

- Update `internal/logic/session/checkpoint_store.go`.
- On Redis miss, attempt DB-backed recovery only when DB is configured.
- If full compose checkpoint serialization is unavailable, return not-found and let chat service rebuild from history.
- Write recovered data back to Redis when recovery succeeds.

## Task 10: Env Documentation

- Update `.env.example` if present; do not commit secrets from `.env`.
- Document optional role model keys for Gate, Subgraph, and Complex.

## Task 11: Verification Tests

- Cover decomposition, parallel RAG, evaluation, tool JSON, nil retriever, blackboard routing, and handoff context injection.
- Validation commands:

```bash
go build ./...
go test ./internal/logic/agent/dialogue/...
go test ./internal/logic/ai/models/...
go test ./internal/logic/session/...
go vet ./...
```
