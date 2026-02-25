# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go-based AI agent system ("go_agent") for on-call alert handling. Provides intelligent chat with RAG (Retrieval-Augmented Generation), tool calling, and knowledge base indexing. The backend uses GoFrame + Eino (Cloudwego's AI orchestration framework), with a vanilla JS frontend.

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

# Frontend
cd Front_page && ./start.sh
```

No test suite exists yet. No linter is configured.

## Architecture

```
main.go → GoFrame HTTP server (port 6872)
  └─ /api/v1/chat         POST  single-turn chat
  └─ /api/v1/chat_stream  POST  streaming chat (SSE)
  └─ /api/v1/upload       POST  file upload to knowledge base
  └─ /api/v1/ai_ops       POST  AI operations
```

### Layer structure

- `api/chat/v1/` — API request/response struct definitions (GoFrame convention)
- `internal/controller/chat/` — HTTP handlers, auto-generated from api/ via `make ctrl`
- `internal/logic/` — Business logic (SSE streaming, chat orchestration)
- `internal/ai/agent/` — AI pipelines built with Eino graph orchestration:
  - `chat_pipeline/` — RAG chat: InputToRag → MilvusRetriever → MergeInputs → ChatTemplate → ReactAgent
  - `knowledge_index_pipeline/` — Document ingestion: FileLoader → MarkdownSplitter → MilvusIndexer
  - `plan_execute_replan/` — Plan-execute-replan agent pattern
- `internal/ai/tools/` — Agent tools (log queries via MCP, Prometheus alerts, MySQL CRUD, knowledge base search)
- `internal/ai/models/` — LLM client initialization (DeepSeek V3 via Volcengine Ark API, Doubao embedding)
- `utility/` — Shared infra: Redis-backed conversation memory (`mem/`), MySQL (`mysql/`), CORS/response middleware, logging (zap)

### Key external services

- **Milvus** (vector DB) — stores document embeddings for RAG retrieval
- **Redis** — conversation history with token budget management (96k input / 8k output)
- **MySQL** — structured data via GORM
- **Volcengine Ark API** — LLM inference (DeepSeek V3) and embeddings (Doubao)
- **Tencent Cloud CLS** — log queries via MCP protocol

### Configuration

Runtime config lives in `manifest/config/config.yaml`. Contains LLM endpoints/keys, Redis, MySQL, Milvus connection details, and file storage paths.

## Conventions

- Go module name: `go_agent`
- Comments and commit messages are in Chinese
- GoFrame code generation patterns: define API structs in `api/`, run `make ctrl` to scaffold controllers
- Eino pipelines use a graph-based DAG pattern — nodes are composed via `compose.NewGraph` with explicit edges
