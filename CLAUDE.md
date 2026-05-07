# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**OnCall** — a Go-based AI agent system for intelligent on-call alert handling. Built on GoFrame (HTTP framework) + Cloudwego Eino (AI orchestration). The backend exposes a chat API with streaming support, session memory, and multi-agent routing. The frontend is a Vanilla JS / Vite SPA.

Go module name: `go_agent`

## Build & Run

```bash
# Run the server (port 6872)
go run main.go

# Build binary (requires GoFrame CLI `gf`)
make build

# Code generation from API definitions
make ctrl      # Generate controllers from api/ definitions
make dao       # Generate DAO/DO/Entity from database schema
make service   # Generate service layer interfaces

# Start infrastructure (Milvus, Prometheus, etcd, MinIO)
cd manifest/docker && docker-compose up -d

# Frontend dev server (port 3000)
cd Front_page && ./start.sh
```

No test suite exists yet. No linter is configured.

## Testing

```bash
go test ./...
go test -race ./...
go test -cover ./...

# Run a specific test
go test -run TestName ./path/to/pkg/...
```

## Architecture

```
main.go  →  bootstrap.NewApplication()  →  GoFrame HTTP server (:6872)
              │
              ├── DialogueAgent   (internal/agent/dialogue/)   — primary chat path
              ├── KnowledgeAgent  (internal/agent/knowledge/)  — KB upload/retrieval
              ├── OpsAgent        (internal/agent/ops/)        — incident workflow
              └── OpsIntegration  (internal/agent/ops/)        — sequential tool runner
```

### HTTP Routes (`/api/v1/`)

All routes pass through CORS + response middleware (`utility/middleware/`).

| Method | Path | Handler |
|--------|------|---------|
| POST | `/api/v1/chat` | Single-turn dialogue |
| POST | `/api/v1/chat_stream` | Streaming chat (SSE) via DialogueAgent |
| POST | `/api/v1/ai_ops` | Ops diagnostic (OpsAgent) |
| POST | `/api/v1/upload` | Knowledge base file upload (KnowledgeAgent) |

### Agent Layer (`internal/agent/`)

Each agent is built as an Eino `adk.Agent` or `adk.ResumableAgent` (compose graph DAG).

**DialogueAgent** (`internal/agent/dialogue/`) — the primary chat agent. Runs intent analysis then routes to tools:
- `BashApprovalTool` — whitelisted shell commands (18-command allowlist)
- `K8sMonitorTool` — Kubernetes pod/resource monitoring
- `MetricsCollectorTool` — Prometheus metrics queries
- `WebSearchTool` — Serper / SearXNG web search
- `KnowledgeRetrieveTool` — Milvus semantic search
- `OpsCaseRetrieveTool` — ops case retrieval
- `IntentAnalysisTool` / `DetailSelectionTool` — internal intent routing

**KnowledgeAgent** (`internal/agent/knowledge/`) — three-node Eino graph: `file_loader → markdown_splitter → milvus_indexer`. Writes content to a temp `.md` file, indexes chunks, returns IDs, then deletes the temp file.

**OpsAgent** (`internal/agent/ops/`) — incident workflow agent with its own tools: `es_log_query`, `k8s_monitor`, `metrics_collector`. A `PodLogShipper` background goroutine ships pod logs from configured namespaces into Elasticsearch every 30 s (controlled by `log_sync.enabled` in config).

### Context / Session Layer (`internal/context/`)

Two-tier session storage:
- **L1**: In-memory `sync.Map` (`GlobalContext`) — hot sessions
- **L2**: Redis (`RedisStorage`) — persisted sessions with `oncall:` key prefix

A background ticker migrates inactive L1 sessions to L2 every 5 minutes. `CheckpointStore` stores Eino agent checkpoints in Redis with 24-hour TTL, enabling conversation resume via `adk.ResumableAgent`.

`SessionMemory` wraps `utility/mem` for token-budget-aware conversation history. Budget: 96k input / 8k output / 20k tools reserve.

### AI Layer (`internal/ai/`)

- `models/open_ai.go` — LLM client init (OpenAI-compatible; routes to proxy or Volcengine Ark)
- `embedder/` — Doubao embedding model (2048-dim) via Volcengine Ark API
- `retriever/` — Milvus retriever for `oncall_knowledge` collection (COSINE similarity)
- `indexer/` — Milvus indexer; auto-provisions the database and collection on first connect

### Key External Services

| Service | Role | Default Address |
|---------|------|-----------------|
| Redis | Session memory, checkpoints | `localhost:31029` |
| Milvus | Vector DB for RAG | `127.0.0.1:31953` |
| MySQL | Structured data (GORM) | `localhost:30306/orm_test` |
| Elasticsearch | Pod log storage | `localhost:30920` |
| Prometheus | Metrics queries | Configurable |
| Volcengine Ark | LLM inference + embeddings | Via OpenAI-compatible proxy |

### Configuration

Runtime config at `manifest/config/config.yaml` (read by GoFrame). Sensitive overrides can go in `.env` (loaded via `godotenv` before config.yaml). Key fields:

- `redis.addr` / `redis.db` / `redis.dialTimeout`
- `prometheus.url`
- `kubeconfig` — path to kubeconfig (used by K8s tools and OpsAgent)
- `log_sync.enabled` / `log_sync.namespaces` / `log_sync.interval`
- `file_dir` — base path for uploaded files

### Frontend

`Front_page/` — Vanilla JS SPA built with Vite. API base URL is hardcoded to `http://127.0.0.1:6872/api/v1` in `Front_page/src/services/api.ts`.

## Conventions

- Comments and commit messages are in **Chinese**
- GoFrame code generation: define API request/response structs in `api/`, run `make ctrl` to scaffold controller stubs — never edit generated controller files by hand
- Eino pipelines use `compose.NewGraph()` with explicit typed edges; each node is a named component
- Errors are always wrapped: `fmt.Errorf("context: %w", err)`
- The Milvus client (`utility/client/client.go`) is idempotent — it auto-creates the database and `oncall_knowledge` collection on startup; removing Milvus requires also disabling this init path
- The DialogueAgent degrades gracefully if Milvus retriever or embedder fails to init (logs a warning and continues without that capability)
